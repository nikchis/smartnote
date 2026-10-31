package wailsui

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/adapter/ssh"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

type memKeys struct {
	pair domain.KeyPair
	has  bool
}

func (m *memKeys) LoadKey(context.Context) (domain.KeyPair, bool, error) { return m.pair, m.has, nil }

func (m *memKeys) SaveKey(_ context.Context, k domain.KeyPair) error {
	m.pair, m.has = k, true
	return nil
}

// fakeFiles answers the file dialogs with fixed paths and counts them.
type fakeFiles struct {
	open, save   string
	opens, saves int
	clipboard    string
}

func (f *fakeFiles) OpenFile(string) (string, error) {
	f.opens++
	return f.open, nil
}

func (f *fakeFiles) SaveFile(string, string) (string, error) {
	f.saves++
	return f.save, nil
}

func (f *fakeFiles) SetClipboard(text string) error {
	f.clipboard = text
	return nil
}

func newTestKeyService(t *testing.T, files *fakeFiles) (*KeyService, *memKeys) {
	t.Helper()

	store := &memKeys{}

	keys, err := usecase.NewKeys(store, ssh.KeyTool{})
	if err != nil {
		t.Fatal(err)
	}

	env := Env{Ctx: t.Context(), Log: slog.New(slog.DiscardHandler)}

	return NewKeyService(env, keys, files), store
}

// writeProtectedKey writes an Ed25519 key protected with passphrase.
func writeProtectedKey(t *testing.T, passphrase string) string {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	block, err := gossh.MarshalPrivateKeyWithPassphrase(priv, "", []byte(passphrase))
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestImportProtectedKeyAsksOnce(t *testing.T) {
	files := &fakeFiles{open: writeProtectedKey(t, "key secret")}
	svc, store := newTestKeyService(t, files)

	ok, err := svc.ImportFromFile("")
	if code := codeOf(err); ok || code != codeKeyPassphraseRequired {
		t.Fatalf("ImportFromFile without passphrase = %v, %v; want false, %s", ok, err, codeKeyPassphraseRequired)
	}

	ok, err = svc.ImportFromFile("wrong")
	if code := codeOf(err); ok || code != codeKeyPassphraseRequired {
		t.Fatalf("ImportFromFile with a wrong passphrase = %v, %v; want false, %s", ok, err, codeKeyPassphraseRequired)
	}

	ok, err = svc.ImportFromFile("key secret")
	if err != nil || !ok {
		t.Fatalf("ImportFromFile with the passphrase = %v, %v; want true, nil", ok, err)
	}

	if files.opens != 1 {
		t.Errorf("open dialogs = %d, want 1: retries reuse the chosen file", files.opens)
	}

	if !store.has {
		t.Error("the key was not stored")
	}
}

func TestImportCancelForgetsFile(t *testing.T) {
	files := &fakeFiles{open: writeProtectedKey(t, "key secret")}
	svc, _ := newTestKeyService(t, files)

	if _, err := svc.ImportFromFile(""); codeOf(err) != codeKeyPassphraseRequired {
		t.Fatalf("ImportFromFile = %v, want %s", err, codeKeyPassphraseRequired)
	}

	svc.CancelImport()

	files.open = "" // the user closes the dialog this time

	ok, err := svc.ImportFromFile("key secret")
	if err != nil || ok {
		t.Fatalf("ImportFromFile after CancelImport = %v, %v; want false, nil", ok, err)
	}

	if files.opens != 2 {
		t.Errorf("open dialogs = %d, want 2: CancelImport forgets the file", files.opens)
	}
}

func TestImportGarbageForgetsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "garbage")
	if err := os.WriteFile(path, []byte("not a key"), 0o600); err != nil {
		t.Fatal(err)
	}

	files := &fakeFiles{open: path}
	svc, store := newTestKeyService(t, files)

	if _, err := svc.ImportFromFile(""); codeOf(err) != codeInternal {
		t.Fatalf("ImportFromFile = %v, want %s", err, codeInternal)
	}

	if _, err := svc.ImportFromFile(""); err == nil {
		t.Fatal("second ImportFromFile succeeded")
	}

	if files.opens != 2 || store.has {
		t.Errorf("opens = %d, stored = %v; want 2, false", files.opens, store.has)
	}
}

func TestImportTooLargeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(path, []byte(strings.Repeat("A", maxKeyFileSize+1)), 0o600); err != nil {
		t.Fatal(err)
	}

	svc, _ := newTestKeyService(t, &fakeFiles{open: path})

	_, err := svc.ImportFromFile("")

	var ui *UIError
	if !errors.As(err, &ui) || ui.Code != codeValidation || ui.Field != "key_file" {
		t.Fatalf("ImportFromFile = %v, want validation of key_file", err)
	}
}

func TestExportPublicKey(t *testing.T) {
	dir := t.TempDir()
	files := &fakeFiles{save: filepath.Join(dir, "smartnote.pub")}
	svc, _ := newTestKeyService(t, files)

	if ok, err := svc.ExportPublicToFile(); codeOf(err) != codeKeyMissing || ok {
		t.Fatalf("ExportPublicToFile without a key = %v, %v; want false, %s", ok, err, codeKeyMissing)
	}

	if err := svc.Generate(); err != nil {
		t.Fatal(err)
	}

	ok, err := svc.ExportPublicToFile()
	if err != nil || !ok {
		t.Fatalf("ExportPublicToFile = %v, %v; want true, nil", ok, err)
	}

	b, err := os.ReadFile(files.save)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(string(b), "ssh-ed25519 ") || !strings.HasSuffix(string(b), "\n") {
		t.Errorf("file = %q, want one authorized_keys line", b)
	}

	if files.clipboard != strings.TrimSpace(string(b)) {
		t.Errorf("clipboard = %q, want the public key", files.clipboard)
	}
}

func TestExportCanceledKeepsClipboard(t *testing.T) {
	files := &fakeFiles{}
	svc, _ := newTestKeyService(t, files)

	if err := svc.Generate(); err != nil {
		t.Fatal(err)
	}

	ok, err := svc.ExportPublicToFile()
	if err != nil || ok {
		t.Fatalf("ExportPublicToFile with a closed dialog = %v, %v; want false, nil", ok, err)
	}

	if !strings.HasPrefix(files.clipboard, "ssh-ed25519 ") {
		t.Errorf("clipboard = %q, want the public key even without a file", files.clipboard)
	}
}

func codeOf(err error) string {
	var ui *UIError
	if errors.As(err, &ui) {
		return ui.Code
	}

	return ""
}
