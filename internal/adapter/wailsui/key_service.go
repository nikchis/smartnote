package wailsui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// maxKeyFileSize bounds the file read as a private key; an RSA 16384 key in
// PEM is about 13 KiB.
const maxKeyFileSize = 64 << 10

// Files are the native dialogs and the clipboard the key service uses. An
// empty path means the user closed the dialog.
type Files interface {
	OpenFile(title string) (string, error)
	SaveFile(title, filename string) (string, error)
	SetClipboard(text string) error
}

// KeyService generates, imports and exports the client SSH key. A protected
// key whose passphrase is missing or wrong stays in memory, so that the retry
// with a passphrase does not open the file dialog again.
type KeyService struct {
	env   Env
	keys  *usecase.Keys
	files Files

	mu      sync.Mutex
	pending []byte // the chosen key file awaiting its passphrase
}

// NewKeyService returns a KeyService over k that opens dialogs through f.
func NewKeyService(env Env, k *usecase.Keys, f Files) *KeyService {
	return &KeyService{env: env, keys: k, files: f}
}

// HasKey reports whether a key is stored.
func (s *KeyService) HasKey() (bool, error) {
	ok, err := s.keys.HasKey(s.env.Ctx)
	if err != nil {
		return false, s.env.fail("has ssh key", err)
	}

	return ok, nil
}

// Generate creates a new Ed25519 key in place of the stored one.
func (s *KeyService) Generate() error {
	return s.env.fail("generate ssh key", s.keys.Generate(s.env.Ctx))
}

// ImportFromFile imports a private key from a file the user chooses, or from
// the file kept by the last call that returned key_passphrase_required. It
// returns false without an error when the user closes the dialog.
func (s *KeyService) ImportFromFile(passphrase string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data := s.pending
	s.pending = nil

	if data == nil {
		path, err := s.files.OpenFile("Import SSH private key")
		if err != nil || path == "" {
			return false, s.env.fail("import ssh key: open dialog", err)
		}

		if data, err = readKeyFile(path); err != nil {
			return false, s.env.fail("import ssh key", err)
		}
	}

	err := s.keys.Import(s.env.Ctx, data, passphrase)
	if errors.Is(err, domain.ErrKeyPassphraseRequired) {
		s.pending = data
		return false, s.env.fail("import ssh key", err)
	}

	clear(data)

	if err != nil {
		return false, s.env.fail("import ssh key", err)
	}

	return true, nil
}

// CancelImport forgets the key file awaiting its passphrase.
func (s *KeyService) CancelImport() {
	s.mu.Lock()
	defer s.mu.Unlock()

	clear(s.pending)
	s.pending = nil
}

// ExportPublicToFile copies the public key in authorized_keys format to the
// clipboard and saves it to a file the user chooses. It returns false without
// an error when the user closes the dialog.
func (s *KeyService) ExportPublicToFile() (bool, error) {
	pub, err := s.keys.PublicKey(s.env.Ctx)
	if err != nil {
		return false, s.env.fail("export public key", err)
	}

	if err := s.files.SetClipboard(pub); err != nil {
		return false, s.env.fail("export public key: clipboard", err)
	}

	path, err := s.files.SaveFile("Export SSH public key", "smartnote.pub")
	if err != nil || path == "" {
		return false, s.env.fail("export public key: save dialog", err)
	}

	//nolint:gosec // a public key, readable like ~/.ssh/*.pub; the path is the user's choice
	if err := os.WriteFile(path, []byte(pub+"\n"), 0o644); err != nil {
		return false, s.env.fail("export public key", fmt.Errorf("write public key: %w", err))
	}

	return true, nil
}

// readKeyFile reads at most maxKeyFileSize bytes of the file at path.
func readKeyFile(path string) ([]byte, error) {
	f, err := os.Open(path) // the path is the user's choice in the open dialog
	if err != nil {
		return nil, fmt.Errorf("open key file: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only: closing cannot lose data

	data, err := io.ReadAll(io.LimitReader(f, maxKeyFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read key file: %w", err)
	}

	if len(data) > maxKeyFileSize {
		clear(data)
		return nil, &domain.ValidationError{Field: "key_file", Reason: "is too large for a private key"}
	}

	return data, nil
}

// WailsFiles opens the dialogs and the clipboard of the running Wails application.
type WailsFiles struct{}

// OpenFile asks for a file to open.
func (WailsFiles) OpenFile(title string) (string, error) {
	path, err := application.Get().Dialog.OpenFile().SetTitle(title).PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("open file dialog: %w", err)
	}

	return path, nil
}

// SaveFile asks for a file to save, suggesting filename.
func (WailsFiles) SaveFile(title, filename string) (string, error) {
	path, err := application.Get().Dialog.SaveFile().SetMessage(title).SetFilename(filename).PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("save file dialog: %w", err)
	}

	return path, nil
}

// SetClipboard puts text on the clipboard.
func (WailsFiles) SetClipboard(text string) error {
	if !application.Get().Clipboard.SetText(text) {
		return errors.New("clipboard is not available")
	}

	return nil
}
