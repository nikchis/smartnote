// Package ssh connects to the server over SSH, tunnels PostgreSQL through
// the connection and manages the client key (spec §6.1).
package ssh

import (
	"bytes"
	"fmt"
	"net"
	"strings"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
)

// hostKeyCallback accepts only the trusted key in authorized_keys format.
// With none trusted it returns *domain.HostKeyUnknownError carrying the
// presented key, so the user can decide to trust it; another key returns
// *domain.HostKeyMismatchError.
func hostKeyCallback(trustedAuthorized string) gossh.HostKeyCallback {
	return func(_ string, _ net.Addr, key gossh.PublicKey) error {
		got := gossh.FingerprintSHA256(key)
		if trustedAuthorized == "" {
			return &domain.HostKeyUnknownError{Fingerprint: got, AuthorizedKey: authorizedLine(key)}
		}
		want, err := parseAuthorized(trustedAuthorized)
		if err != nil {
			return err
		}
		if !bytes.Equal(want.Marshal(), key.Marshal()) {
			return &domain.HostKeyMismatchError{Expected: gossh.FingerprintSHA256(want), Got: got}
		}
		return nil
	}
}

// pinnedAlgorithms returns the host key algorithms of the trusted key's type,
// so that a server offering another key type first is not reported as a
// changed key; nil when nothing is trusted.
func pinnedAlgorithms(trustedAuthorized string) []string {
	if trustedAuthorized == "" {
		return nil
	}
	key, err := parseAuthorized(trustedAuthorized)
	if err != nil {
		return nil
	}
	if key.Type() == gossh.KeyAlgoRSA {
		return []string{gossh.KeyAlgoRSASHA512, gossh.KeyAlgoRSASHA256, gossh.KeyAlgoRSA}
	}
	return []string{key.Type()}
}

func parseAuthorized(line string) (gossh.PublicKey, error) {
	key, _, _, _, err := gossh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return nil, fmt.Errorf("trusted host key: %w", err)
	}
	return key, nil
}

// authorizedLine is key in authorized_keys format without the newline.
func authorizedLine(key gossh.PublicKey) string {
	return strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key)))
}
