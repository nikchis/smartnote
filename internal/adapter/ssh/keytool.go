package ssh

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
)

// keyComment is the comment of keys this app writes.
const keyComment = "smartnote"

// KeyTool creates and parses client SSH keys. It implements usecase.KeyTool.
type KeyTool struct{}

// Generate returns a new Ed25519 key in OpenSSH format.
func (KeyTool) Generate() (domain.KeyPair, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return domain.KeyPair{}, fmt.Errorf("generate key: %w", err)
	}
	return pairOf(priv)
}

// Import parses an OpenSSH or PEM private key and returns it re-encoded
// without a passphrase, since the local storage already encrypts it. A
// protected key with no or a wrong passphrase returns
// domain.ErrKeyPassphraseRequired.
func (KeyTool) Import(data []byte, passphrase string) (domain.KeyPair, error) {
	key, err := gossh.ParseRawPrivateKey(data)
	if _, protected := errors.AsType[*gossh.PassphraseMissingError](err); protected {
		if passphrase == "" {
			return domain.KeyPair{}, fmt.Errorf("import key: %w", domain.ErrKeyPassphraseRequired)
		}
		key, err = gossh.ParseRawPrivateKeyWithPassphrase(data, []byte(passphrase))
		if errors.Is(err, x509.IncorrectPasswordError) {
			return domain.KeyPair{}, fmt.Errorf("import key: wrong passphrase: %w", domain.ErrKeyPassphraseRequired)
		}
	}
	if err != nil {
		return domain.KeyPair{}, fmt.Errorf("import key: %w", err)
	}
	if p, ok := key.(*ed25519.PrivateKey); ok {
		key = *p // MarshalPrivateKey takes the value
	}
	return pairOf(key)
}

func pairOf(key crypto.PrivateKey) (domain.KeyPair, error) {
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		return domain.KeyPair{}, fmt.Errorf("key: %w", err)
	}
	block, err := gossh.MarshalPrivateKey(key, keyComment)
	if err != nil {
		return domain.KeyPair{}, fmt.Errorf("encode key: %w", err)
	}
	return domain.KeyPair{
		PrivatePEM:       pem.EncodeToMemory(block),
		PublicAuthorized: authorizedLine(signer.PublicKey()) + " " + keyComment,
	}, nil
}
