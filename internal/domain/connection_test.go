package domain

import (
	"encoding/json"
	"strings"
	"testing"
)

// validSettings returns complete settings that pass validation.
func validSettings() ConnectionSettings {
	return ConnectionSettings{
		SSHHost:    "example.org",
		SSHPort:    22,
		SSHUser:    "user",
		DBHost:     "127.0.0.1",
		DBPort:     5432,
		DBName:     "smartnote",
		DBUser:     "smartnote",
		SyncPhrase: strings.Repeat("p", MinSyncPhraseLen),
	}
}

func TestConnectionSettings_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		settings ConnectionSettings
		want     bool
	}{
		{name: "zero value", settings: ConnectionSettings{}, want: true},
		{name: "only trusted host key", settings: ConnectionSettings{TrustedHostKey: "ssh-ed25519 AAAA"}, want: true},
		{name: "only ssh password", settings: ConnectionSettings{SSHPassword: "x"}, want: false},
		{name: "only db port", settings: ConnectionSettings{DBPort: 5432}, want: false},
		{name: "complete", settings: validSettings(), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.settings.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConnectionSettings_Validate(t *testing.T) {
	tests := []struct {
		name      string
		modify    func(*ConnectionSettings)
		wantField string
	}{
		{name: "empty settings", modify: func(s *ConnectionSettings) { *s = ConnectionSettings{} }},
		{name: "only ssh host", modify: func(s *ConnectionSettings) { *s = ConnectionSettings{SSHHost: "example.org"} }, wantField: "ssh_user"},
		{name: "complete", modify: func(*ConnectionSettings) {}},
		{name: "missing ssh host", modify: func(s *ConnectionSettings) { s.SSHHost = "" }, wantField: "ssh_host"},
		{name: "ssh port 0", modify: func(s *ConnectionSettings) { s.SSHPort = 0 }, wantField: "ssh_port"},
		{name: "ssh port 70000", modify: func(s *ConnectionSettings) { s.SSHPort = 70000 }, wantField: "ssh_port"},
		{name: "ssh port -1", modify: func(s *ConnectionSettings) { s.SSHPort = -1 }, wantField: "ssh_port"},
		{name: "ssh port 65535", modify: func(s *ConnectionSettings) { s.SSHPort = 65535 }},
		{name: "db port 0", modify: func(s *ConnectionSettings) { s.DBPort = 0 }, wantField: "db_port"},
		{name: "db port 65536", modify: func(s *ConnectionSettings) { s.DBPort = 65536 }, wantField: "db_port"},
		{name: "db port 1", modify: func(s *ConnectionSettings) { s.DBPort = 1 }},
		{name: "missing db name", modify: func(s *ConnectionSettings) { s.DBName = "" }, wantField: "db_name"},
		{name: "missing db user", modify: func(s *ConnectionSettings) { s.DBUser = "" }, wantField: "db_user"},
		{name: "missing sync phrase", modify: func(s *ConnectionSettings) { s.SyncPhrase = "" }, wantField: "sync_phrase"},
		{name: "db host not loopback", modify: func(s *ConnectionSettings) { s.DBHost = "10.0.0.5" }, wantField: "db_host"},
		{name: "db host other loopback address", modify: func(s *ConnectionSettings) { s.DBHost = "127.0.0.2" }, wantField: "db_host"},
		{name: "db host localhost", modify: func(s *ConnectionSettings) { s.DBHost = "localhost" }},
		{name: "db host ipv4 loopback", modify: func(s *ConnectionSettings) { s.DBHost = "127.0.0.1" }},
		{name: "db host ipv6 loopback", modify: func(s *ConnectionSettings) { s.DBHost = "::1" }},
		{name: "db host empty uses default", modify: func(s *ConnectionSettings) { s.DBHost = "" }},
		{name: "sync phrase of 15 units", modify: func(s *ConnectionSettings) { s.SyncPhrase = strings.Repeat("p", MinSyncPhraseLen-1) }, wantField: "sync_phrase"},
		{name: "sync phrase of 16 units", modify: func(s *ConnectionSettings) { s.SyncPhrase = strings.Repeat("p", MinSyncPhraseLen) }},
		{name: "passwords are optional", modify: func(s *ConnectionSettings) { s.SSHPassword, s.DBPassword = "", "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSettings()
			tt.modify(&s)
			assertValidationField(t, s.Validate(), tt.wantField)
		})
	}
}

func TestConnectionSettings_WithDefaults(t *testing.T) {
	tests := []struct {
		name       string
		settings   ConnectionSettings
		wantDBHost string
	}{
		{name: "empty settings stay empty", settings: ConnectionSettings{}, wantDBHost: ""},
		{name: "missing db host gets default", settings: ConnectionSettings{SSHHost: "h"}, wantDBHost: DefaultDBHost},
		{name: "db host is kept", settings: ConnectionSettings{SSHHost: "h", DBHost: "::1"}, wantDBHost: "::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.settings.WithDefaults().DBHost; got != tt.wantDBHost {
				t.Errorf("WithDefaults().DBHost = %q, want %q", got, tt.wantDBHost)
			}
		})
	}
}

func TestIsLoopbackHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{host: "localhost", want: true},
		{host: "127.0.0.1", want: true},
		{host: "::1", want: true},
		{host: "", want: false},
		{host: "127.0.0.2", want: false},
		{host: "::ffff:127.0.0.1", want: false},
		{host: "[::1]", want: false},
		{host: "LOCALHOST", want: false},
		{host: "example.org", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := IsLoopbackHost(tt.host); got != tt.want {
				t.Errorf("IsLoopbackHost(%q) = %v, want %v", tt.host, got, tt.want)
			}
		})
	}
}

func TestConnectionSettings_JSONNames(t *testing.T) {
	s := ConnectionSettings{
		SSHHost: "h", SSHPort: 1, SSHUser: "u", SSHPassword: "p", DBHost: "::1", DBPort: 2,
		DBName: "n", DBUser: "d", DBPassword: "q", SyncPhrase: "s", TrustedHostKey: "k",
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"ssh_host", "ssh_port", "ssh_user", "ssh_password", "db_host", "db_port",
		"db_name", "db_user", "db_password", "sync_phrase", "trusted_host_key"}
	if len(got) != len(want) {
		t.Errorf("got %d keys %v, want %d", len(got), got, len(want))
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("key %q missing in %s", k, data)
		}
	}
}
