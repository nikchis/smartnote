package domain

import "fmt"

// DefaultDBHost is the database host used when settings leave it empty.
const DefaultDBHost = "127.0.0.1"

// ConnectionSettings describe the SSH server and the PostgreSQL database
// reachable from it. Either all required fields are set, or none.
type ConnectionSettings struct {
	SSHHost        string `json:"ssh_host"`
	SSHPort        int    `json:"ssh_port"`
	SSHUser        string `json:"ssh_user"`
	SSHPassword    string `json:"ssh_password"`
	DBHost         string `json:"db_host"`
	DBPort         int    `json:"db_port"`
	DBName         string `json:"db_name"`
	DBUser         string `json:"db_user"`
	DBPassword     string `json:"db_password"`
	SyncPhrase     string `json:"sync_phrase"`
	TrustedHostKey string `json:"trusted_host_key"` // authorized_keys format; empty until trusted
}

// IsEmpty reports whether no field except TrustedHostKey is set: the app then
// works locally only.
func (s ConnectionSettings) IsEmpty() bool {
	s.TrustedHostKey = ""
	return s == ConnectionSettings{}
}

// Validate returns nil for empty settings; otherwise it checks required fields,
// ports, the loopback database host and the sync phrase length.
func (s ConnectionSettings) Validate() error {
	if s.IsEmpty() {
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
	if textLen(s.SyncPhrase) < MinSyncPhraseLen {
		return &ValidationError{Field: "sync_phrase", Reason: fmt.Sprintf("must be at least %d characters", MinSyncPhraseLen)}
	}
	return nil
}

// WithDefaults returns s with an empty DBHost set to DefaultDBHost, unless s is empty.
func (s ConnectionSettings) WithDefaults() ConnectionSettings {
	if !s.IsEmpty() && s.DBHost == "" {
		s.DBHost = DefaultDBHost
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
		return &ValidationError{Field: field, Reason: "is required"}
	}
	return nil
}

func validPort(field string, port int) error {
	if port < 1 || port > 65535 {
		return &ValidationError{Field: field, Reason: "must be between 1 and 65535"}
	}
	return nil
}
