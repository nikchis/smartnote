package ssh

import (
	"crypto/dsa" //nolint:staticcheck // a DSA key is needed to test that it is refused
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
)

func TestKeyToolGenerateImport(t *testing.T) {
	tool := KeyTool{}

	pair, err := tool.Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	if !strings.HasPrefix(pair.PublicAuthorized, "ssh-ed25519 ") || strings.HasSuffix(pair.PublicAuthorized, "\n") {
		t.Errorf("PublicAuthorized = %q", pair.PublicAuthorized)
	}

	again, err := tool.Import(pair.PrivatePEM, "")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}

	if again.PublicAuthorized != pair.PublicAuthorized {
		t.Errorf("imported public key %q, want %q", again.PublicAuthorized, pair.PublicAuthorized)
	}

	other, err := tool.Generate()
	if err != nil {
		t.Fatal(err)
	}

	if other.PublicAuthorized == pair.PublicAuthorized {
		t.Error("two keys are the same")
	}
}

func TestKeyToolImportProtected(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	block, err := gossh.MarshalPrivateKeyWithPassphrase(priv, "", []byte("key passphrase"))
	if err != nil {
		t.Fatal(err)
	}

	protected := pem.EncodeToMemory(block)
	tool := KeyTool{}

	if _, err := tool.Import(protected, ""); !errors.Is(err, domain.ErrKeyPassphraseRequired) {
		t.Errorf("Import without a passphrase = %v, want ErrKeyPassphraseRequired", err)
	}

	if _, err := tool.Import(protected, "wrong passphrase"); !errors.Is(err, domain.ErrKeyPassphraseRequired) {
		t.Errorf("Import with a wrong passphrase = %v, want ErrKeyPassphraseRequired", err)
	}

	pair, err := tool.Import(protected, "key passphrase")
	if err != nil {
		t.Fatalf("Import with the passphrase: %v", err)
	}

	if _, err := tool.Import(pair.PrivatePEM, ""); err != nil {
		t.Errorf("the stored key still needs a passphrase: %v", err)
	}

	pub, err := gossh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}

	if want := authorizedLine(pub) + " " + keyComment; pair.PublicAuthorized != want {
		t.Errorf("PublicAuthorized = %q, want %q", pair.PublicAuthorized, want)
	}
}

func TestKeyToolImportGarbage(t *testing.T) {
	if _, err := (KeyTool{}).Import([]byte("not a key"), ""); err == nil || errors.Is(err, domain.ErrKeyPassphraseRequired) {
		t.Errorf("Import of garbage = %v, want a parse error", err)
	}
}

// rsaPEM returns a new RSA key of bits in PKCS #1 PEM.
func rsaPEM(t *testing.T, bits int) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
}

// dsaPEM returns a new DSA key in the OpenSSL PEM format ssh-keygen wrote.
func dsaPEM(t *testing.T) []byte {
	t.Helper()

	var key dsa.PrivateKey
	if err := dsa.GenerateParameters(&key.Parameters, rand.Reader, dsa.L1024N160); err != nil {
		t.Fatal(err)
	}

	if err := dsa.GenerateKey(&key, rand.Reader); err != nil {
		t.Fatal(err)
	}

	der, err := asn1.Marshal(struct {
		Version       int
		P, Q, G, Y, X *big.Int
	}{0, key.P, key.Q, key.G, key.Y, key.X})
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "DSA PRIVATE KEY", Bytes: der})
}

// TestKeyToolImportRejectsWeakKeys: RSA keys shorter than 2048 bits and DSA
// keys are refused as a field error of the key file; RSA 2048 is accepted.
func TestKeyToolImportRejectsWeakKeys(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		weak bool
	}{
		{name: "RSA 1024", data: rsaPEM(t, 1024), weak: true},
		{name: "DSA", data: dsaPEM(t), weak: true},
		{name: "RSA 2048", data: rsaPEM(t, 2048)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (KeyTool{}).Import(tc.data, "")
			if !tc.weak {
				if err != nil {
					t.Fatalf("Import = %v, want nil", err)
				}

				return
			}

			ve, ok := errors.AsType[*domain.ValidationError](err)
			if !ok || ve.Field != "key_file" {
				t.Errorf("Import = %v, want a validation error of field key_file", err)
			}
		})
	}
}
