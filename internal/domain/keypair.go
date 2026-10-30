package domain

import "log/slog"

// KeyPair is the client SSH key.
type KeyPair struct {
	PrivatePEM       []byte // OpenSSH private key, not passphrase-protected
	PublicAuthorized string // public key in authorized_keys format
}

// String hides both keys, which may not reach the log. It also serves %v and %+v.
func (k KeyPair) String() string { return "KeyPair{redacted}" }

// GoString keeps %#v as redacted as String.
func (k KeyPair) GoString() string { return k.String() }

// LogValue keeps slog output as redacted as String.
func (k KeyPair) LogValue() slog.Value { return slog.StringValue(k.String()) }
