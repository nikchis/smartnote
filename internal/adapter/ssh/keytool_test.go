package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
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
