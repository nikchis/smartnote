package domain

import (
	"fmt"
	"log/slog"
	"net"
	"strconv"
)

// Values used when settings leave a field empty; the settings form shows
// them as placeholders.
const (
	DefaultDBHost  = "127.0.0.1"
	DefaultSSHPort = 22
	DefaultDBPort  = 5432
)

// StorageKind is the type of remote storage settings point to.
type StorageKind string

// Storage kinds. An empty StorageKind in settings saved by older versions is
// normalized by ConnectionSettings.Kind.
const (
	KindNone        StorageKind = "none"
	KindSSHPostgres StorageKind = "ssh_postgres"
	KindYandexDisk  StorageKind = "yandex_disk"
)

// maxYandexClientIDLen is the longest accepted Yandex OAuth client ID.
const maxYandexClientIDLen = 64

// ConnectionSettings describe the remote storage: for ssh_postgres the SSH
// server and the PostgreSQL database reachable from it, for yandex_disk the
// OAuth client ID. Fields of other kinds are kept so that switching back needs
// no retyping; SyncPhrase is shared.
type ConnectionSettings struct {
	StorageKind     StorageKind `json:"kind"`
	YandexClientID  string      `json:"yandex_client_id"`
	SSHHost         string      `json:"ssh_host"`
	SSHPort         int         `json:"ssh_port"`
	SSHUser         string      `json:"ssh_user"`
	SSHPassword     string      `json:"ssh_password"`
	DBHost          string      `json:"db_host"`
	DBPort          int         `json:"db_port"`
	DBName          string      `json:"db_name"`
	DBUser          string      `json:"db_user"`
	DBPassword      string      `json:"db_password"`
	SyncPhrase      string      `json:"sync_phrase"`
	TrustedHostKey  string      `json:"trusted_host_key"` // authorized_keys format; empty until trusted
	TrustedHostAddr string      `json:"trusted_host_addr"`
}

// Kind returns the storage kind. Empty StorageKind of settings saved before
// kinds existed becomes ssh_postgres if any SSH/DB field or the sync phrase is
// set, and none otherwise; a trusted host key alone does not count.
func (s ConnectionSettings) Kind() StorageKind {
	if s.StorageKind != "" {
		return s.StorageKind
	}

	if s.sshFieldsEmpty() {
		return KindNone
	}

	return KindSSHPostgres
}

// sshFieldsEmpty reports whether no SSH/DB field and no sync phrase is set.
// Kind, client ID and the trusted key with its address are not considered.
func (s ConnectionSettings) sshFieldsEmpty() bool {
	s.StorageKind, s.YandexClientID = "", ""
	s.TrustedHostKey, s.TrustedHostAddr = "", ""

	return s == ConnectionSettings{}
}

// IsEmpty reports whether the app works locally only: the kind is none, or
// it is ssh_postgres and no SSH/DB field or sync phrase is set. yandex_disk
// and unknown kinds are never empty.
func (s ConnectionSettings) IsEmpty() bool {
	switch s.Kind() {
	case KindNone:
		return true
	case KindSSHPostgres:
		return s.sshFieldsEmpty()
	default:
		return false
	}
}

// Validate checks the settings by kind. none needs nothing; ssh_postgres
// checks required fields, ports, the loopback database host and the sync
// phrase length (nil if all SSH/DB fields are empty); yandex_disk checks the
// sync phrase length and the client ID format. An unknown kind is invalid.
func (s ConnectionSettings) Validate() error {
	switch s.Kind() {
	case KindNone:
		return nil
	case KindSSHPostgres:
		return s.validateSSHPostgres()
	case KindYandexDisk:
		return s.validateYandexDisk()
	default:
		return &ValidationError{Field: "kind", Reason: "unknown storage kind"}
	}
}

func (s ConnectionSettings) validateYandexDisk() error {
	if err := validSyncPhrase(s.SyncPhrase); err != nil {
		return err
	}

	if !validYandexClientID(s.YandexClientID) {
		return &ValidationError{Field: "yandex_client_id", Reason: "must be 1 to 64 letters, digits, '_' or '-'"}
	}

	return nil
}

// validYandexClientID reports whether id is empty or 1 to 64 characters of
// [A-Za-z0-9_-]. An empty ID means "use the built-in one".
func validYandexClientID(id string) bool {
	if len(id) > maxYandexClientIDLen {
		return false
	}

	for i := 0; i < len(id); i++ {
		c := id[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}

	return true
}

func validSyncPhrase(phrase string) error {
	if secretLen(phrase) < MinSyncPhraseLen {
		return &ValidationError{Field: "sync_phrase", Reason: fmt.Sprintf("must be at least %d characters", MinSyncPhraseLen)}
	}

	return nil
}

func (s ConnectionSettings) validateSSHPostgres() error {
	if s.sshFieldsEmpty() {
		return nil
	}

	if err := required("ssh_host", s.SSHHost); err != nil {
		return err
	}

	if err := required("ssh_user", s.SSHUser); err != nil {
		return err
	}

	if err := validPort("ssh_port", s.SSHPort); err != nil {
		return err
	}

	if s.DBHost != "" && !IsLoopbackHost(s.DBHost) {
		return &ValidationError{Field: "db_host", Reason: "must be localhost, 127.0.0.1 or ::1"}
	}

	if err := validPort("db_port", s.DBPort); err != nil {
		return err
	}

	if err := required("db_name", s.DBName); err != nil {
		return err
	}

	if err := required("db_user", s.DBUser); err != nil {
		return err
	}

	return validSyncPhrase(s.SyncPhrase)
}

// String hides every field: together they form the connection string and hold
// passwords and the sync phrase, none of which may reach the log. It also
// serves %v and %+v.
func (s ConnectionSettings) String() string {
	if s.IsEmpty() {
		return "ConnectionSettings{empty}"
	}

	return "ConnectionSettings{redacted}"
}

// GoString keeps %#v as redacted as String.
func (s ConnectionSettings) GoString() string { return s.String() }

// LogValue keeps slog output as redacted as String.
func (s ConnectionSettings) LogValue() slog.Value { return slog.StringValue(s.String()) }

// WithDefaults returns s with an empty DBHost, SSHPort and DBPort set to
// their defaults, only for a non-empty ssh_postgres s. An invalid port is
// kept for Validate.
func (s ConnectionSettings) WithDefaults() ConnectionSettings {
	if s.Kind() != KindSSHPostgres || s.IsEmpty() {
		return s
	}

	if s.DBHost == "" {
		s.DBHost = DefaultDBHost
	}

	if s.SSHPort == 0 {
		s.SSHPort = DefaultSSHPort
	}

	if s.DBPort == 0 {
		s.DBPort = DefaultDBPort
	}

	return s
}

// IsLoopbackHost reports whether h is one of the allowed database hosts:
// "localhost", "127.0.0.1" or "::1".
func IsLoopbackHost(h string) bool {
	switch h {
	case "localhost", "127.0.0.1", "::1":
		return true
	}

	return false
}

func required(field, v string) error {
	if v == "" {
		return &ValidationError{Field: field, Reason: reasonRequired}
	}

	return nil
}

func validPort(field string, port int) error {
	if port < 1 || port > 65535 {
		return &ValidationError{Field: field, Reason: "must be between 1 and 65535"}
	}

	return nil
}

// SSHAddr is the SSH server address host:port, the address a trusted key is bound to.
func (s ConnectionSettings) SSHAddr() string {
	return net.JoinHostPort(s.SSHHost, strconv.Itoa(s.SSHPort))
}

// TrustedKey returns the trusted server key if it was trusted for the current
// SSH address, and "" otherwise: a key trusted for another server is not one
// to compare with, and the user is asked again.
func (s ConnectionSettings) TrustedKey() string {
	if s.TrustedHostAddr != s.SSHAddr() {
		return ""
	}

	return s.TrustedHostKey
}

// WithoutStaleTrust returns s without a trusted key bound to another address.
func (s ConnectionSettings) WithoutStaleTrust() ConnectionSettings {
	if s.TrustedKey() == "" {
		s.TrustedHostKey, s.TrustedHostAddr = "", ""
	}

	return s
}
