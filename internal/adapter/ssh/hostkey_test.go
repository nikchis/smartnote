package ssh

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"slices"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
)

func newEd25519Key(t *testing.T) gossh.PublicKey {
	t.Helper()

	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	k, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	return k
}

func TestHostKeyUnknown(t *testing.T) {
	key := newEd25519Key(t)
	err := hostKeyCallback("")("example.org:22", nil, key)

	hk, ok := errors.AsType[*domain.HostKeyUnknownError](err)
	if !ok {
		t.Fatalf("callback = %v, want *HostKeyUnknownError", err)
	}

	if hk.Addr != "example.org:22" {
		t.Errorf("Addr = %q, want the address that presented the key", hk.Addr)
	}

	if hk.Fingerprint != gossh.FingerprintSHA256(key) {
		t.Errorf("Fingerprint = %q, want %q", hk.Fingerprint, gossh.FingerprintSHA256(key))
	}

	parsed, err := parseAuthorized(hk.AuthorizedKey)
	if err != nil || !bytes.Equal(parsed.Marshal(), key.Marshal()) {
		t.Errorf("AuthorizedKey %q does not parse back to the key: %v", hk.AuthorizedKey, err)
	}
}

func TestHostKeyMismatch(t *testing.T) {
	trusted, presented := newEd25519Key(t), newEd25519Key(t)
	err := hostKeyCallback(authorizedLine(trusted))("example.org:22", nil, presented)

	hk, ok := errors.AsType[*domain.HostKeyMismatchError](err)
	if !ok {
		t.Fatalf("callback = %v, want *HostKeyMismatchError", err)
	}

	if hk.Expected != gossh.FingerprintSHA256(trusted) || hk.Got != gossh.FingerprintSHA256(presented) {
		t.Errorf("mismatch = %+v", hk)
	}

	if err := hostKeyCallback(authorizedLine(trusted))("example.org:22", nil, trusted); err != nil {
		t.Errorf("trusted key: %v", err)
	}
}

func TestHostKeyBrokenTrusted(t *testing.T) {
	if err := hostKeyCallback("not a key")("example.org:22", nil, newEd25519Key(t)); err == nil {
		t.Error("a trusted key that does not parse was accepted")
	}
}

func TestPinnedAlgorithms(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	rsaPub, err := gossh.NewPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		trusted string
		want    []string
	}{
		{name: "not trusted", trusted: "", want: nil},
		{name: "broken", trusted: "garbage", want: nil},
		{name: "ed25519", trusted: authorizedLine(newEd25519Key(t)), want: []string{"ssh-ed25519"}},
		{name: "rsa", trusted: authorizedLine(rsaPub), want: []string{"rsa-sha2-512", "rsa-sha2-256", "ssh-rsa"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pinnedAlgorithms(tt.trusted); !slices.Equal(got, tt.want) {
				t.Errorf("pinnedAlgorithms = %v, want %v", got, tt.want)
			}
		})
	}
}
