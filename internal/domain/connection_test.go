package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
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
		{name: "only trusted host key", settings: ConnectionSettings{TrustedHostKey: "ssh-ed25519 AAAA", TrustedHostAddr: "h:22"}, want: true},
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
		{name: "sync phrase of 8 nfd letters is 16 units but 8 after nfc", modify: func(s *ConnectionSettings) { s.SyncPhrase = strings.Repeat("\u0435\u0308", 8) }, wantField: "sync_phrase"},
		{name: "sync phrase of 16 nfd letters", modify: func(s *ConnectionSettings) { s.SyncPhrase = strings.Repeat("\u0435\u0308", 16) }},
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
		DBName: "n", DBUser: "d", DBPassword: "q", SyncPhrase: "s", TrustedHostKey: "k", TrustedHostAddr: "h:1",
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
		"db_name", "db_user", "db_password", "sync_phrase", "trusted_host_key", "trusted_host_addr"}
	if len(got) != len(want) {
		t.Errorf("got %d keys %v, want %d", len(got), got, len(want))
	}

	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("key %q missing in %s", k, data)
		}
	}
}

// secretSettings has a distinct marker in every field that must not reach a log.
func secretSettings() ConnectionSettings {
	return ConnectionSettings{
		SSHHost: "host-marker", SSHPort: 22, SSHUser: "ssh-user-marker", SSHPassword: "ssh-password-marker",
		DBHost: "127.0.0.1", DBPort: 5432, DBName: "db-name-marker", DBUser: "db-user-marker",
		DBPassword: "db-password-marker", SyncPhrase: "sync-phrase-marker", TrustedHostKey: "host-key-marker",
	}
}

var settingsMarkers = []string{"host-marker", "ssh-user-marker", "ssh-password-marker", "db-name-marker",
	"db-user-marker", "db-password-marker", "sync-phrase-marker", "host-key-marker"}

func TestConnectionSettings_Redacted(t *testing.T) {
	s := secretSettings()

	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("connect", "settings", s, "ptr", &s)

	outputs := map[string]string{
		"%v": fmt.Sprintf("%v", s), //nolint:gocritic // the test checks the fmt path, not String

		"%+v":  fmt.Sprintf("%+v", s),
		"%#v":  fmt.Sprintf("%#v", s),
		"ptr":  fmt.Sprintf("%+v", &s),
		"slog": logged.String(),
	}
	for verb, out := range outputs {
		for _, m := range settingsMarkers {
			if strings.Contains(out, m) {
				t.Errorf("%s output %q contains %q", verb, out, m)
			}
		}
	}

	if got := fmt.Sprint(ConnectionSettings{}); !strings.Contains(got, "empty") { //nolint:gocritic // the fmt path
		t.Errorf("empty settings print as %q, want a mention of empty", got)
	}
}

func TestConnectionSettings_SSHAddr(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{host: "example.org", port: 22, want: "example.org:22"},
		{host: "::1", port: 2222, want: "[::1]:2222"},
	}
	for _, tt := range tests {
		if got := (ConnectionSettings{SSHHost: tt.host, SSHPort: tt.port}).SSHAddr(); got != tt.want {
			t.Errorf("SSHAddr(%s, %d) = %q, want %q", tt.host, tt.port, got, tt.want)
		}
	}
}

func TestConnectionSettings_TrustedKey(t *testing.T) {
	s := validSettings()

	s.TrustedHostKey, s.TrustedHostAddr = "ssh-ed25519 AAAA", s.SSHAddr()
	if got := s.TrustedKey(); got != "ssh-ed25519 AAAA" {
		t.Errorf("TrustedKey for the bound address = %q", got)
	}

	moved := s

	moved.SSHPort = 2222
	if got := moved.TrustedKey(); got != "" {
		t.Errorf("TrustedKey after the port changed = %q, want none", got)
	}

	unbound := s

	unbound.TrustedHostAddr = ""
	if got := unbound.TrustedKey(); got != "" {
		t.Errorf("TrustedKey without an address = %q, want none", got)
	}
}

func TestConnectionSettings_WithoutStaleTrust(t *testing.T) {
	s := validSettings()

	s.TrustedHostKey, s.TrustedHostAddr = "ssh-ed25519 AAAA", s.SSHAddr()
	if got := s.WithoutStaleTrust(); got != s {
		t.Error("a key bound to the current address was dropped")
	}

	moved := s
	moved.SSHHost = "other.example.org"

	got := moved.WithoutStaleTrust()
	if got.TrustedHostKey != "" || got.TrustedHostAddr != "" {
		t.Errorf("key bound to another address kept: %q for %q", got.TrustedHostKey, got.TrustedHostAddr)
	}

	if got.SSHHost != "other.example.org" {
		t.Error("WithoutStaleTrust changed other fields")
	}
}
