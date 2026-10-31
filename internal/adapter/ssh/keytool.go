package ssh

import (
	"crypto"
	"crypto/dsa" //nolint:staticcheck // only to recognize and refuse DSA keys
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
)

// keyComment is the comment of keys this app writes.
const keyComment = "smartnote"

// minRSABits is the smallest RSA key accepted on import (NIST SP 800-131A).
const minRSABits = 2048

// fieldKeyFile names the imported key file in validation errors.
const fieldKeyFile = "key_file"

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

	if err := checkStrength(key); err != nil {
		return domain.KeyPair{}, err
	}

	if p, ok := key.(*ed25519.PrivateKey); ok {
		key = *p // MarshalPrivateKey takes the value
	}

	return pairOf(key)
}

// checkStrength refuses keys too weak to protect the server: DSA and RSA
// shorter than minRSABits. Ed25519 and ECDSA keys are accepted.
func checkStrength(key crypto.PrivateKey) error {
	switch k := key.(type) {
	case *dsa.PrivateKey:
		return &domain.ValidationError{Field: fieldKeyFile, Reason: "is a DSA key, which is no longer secure; use Ed25519 or RSA of at least 2048 bits"}
	case *rsa.PrivateKey:
		if bits := k.N.BitLen(); bits < minRSABits {
			return &domain.ValidationError{Field: fieldKeyFile, Reason: fmt.Sprintf("is an RSA key of %d bits; at least %d are needed", bits, minRSABits)}
		}
	}

	return nil
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
