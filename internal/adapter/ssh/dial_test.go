package ssh

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
)

// fakeDialer records the addresses it is asked for and fails every dial.
type fakeDialer struct{ addrs []string }

var errFakeDial = errors.New("fake dial")

func (d *fakeDialer) DialContext(_ context.Context, _, addr string) (net.Conn, error) {
	d.addrs = append(d.addrs, addr)
	return nil, errFakeDial
}

func dbSettings(host string) domain.ConnectionSettings {
	return domain.ConnectionSettings{DBHost: host, DBPort: 5432, DBName: "db", DBUser: "u", DBPassword: "p"}
}

func TestPgConfigIgnoresEnv(t *testing.T) {
	t.Setenv("PGHOST", "env-primary.invalid,env-backup.invalid")
	t.Setenv("PGSSLMODE", "verify-full")
	t.Setenv("PGSSLROOTCERT", "/nonexistent/root.crt")
	t.Setenv("PGAPPNAME", "from-env")
	t.Setenv("PGTARGETSESSIONATTRS", "read-write")
	t.Setenv("PGCONNECT_TIMEOUT", "1")
	t.Setenv("PGPORT", "1,2,3")
	t.Setenv("PGPASSFILE", "/nonexistent/pgpass")
	serviceFile := filepath.Join(t.TempDir(), "pg_service.conf")
	if err := os.WriteFile(serviceFile, []byte("[evil]\nhost=evil.invalid\nport=1\nsslmode=require\ndbname=evil\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PGSERVICEFILE", serviceFile)
	t.Setenv("PGSERVICE", "evil")
	t.Setenv("PGMINPROTOCOLVERSION", "bogus")
	t.Setenv("PGOPTIONS", "-c search_path=evil")
	d := &fakeDialer{}

	cfg, err := pgConfig(dbSettings("127.0.0.1"), d)
	if err != nil {
		t.Fatalf("pgConfig: %v", err)
	}
	if len(cfg.Fallbacks) != 0 || cfg.TLSConfig != nil || cfg.ValidateConnect != nil || cfg.ConnectTimeout != 0 {
		t.Errorf("config keeps environment settings: fallbacks %d, tls %v, validate %v, timeout %v",
			len(cfg.Fallbacks), cfg.TLSConfig != nil, cfg.ValidateConnect != nil, cfg.ConnectTimeout)
	}
	if len(cfg.RuntimeParams) != 0 {
		t.Errorf("RuntimeParams = %v, want none", cfg.RuntimeParams)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 5432 || cfg.Database != "db" || cfg.User != "u" || cfg.Password != "p" {
		t.Errorf("config = %s@%s:%d/%s", cfg.User, cfg.Host, cfg.Port, cfg.Database)
	}
	for _, addr := range []string{"env-backup.invalid:5432", "evil.invalid:1", "127.0.0.1:1"} {
		if _, err := cfg.DialFunc(context.Background(), "tcp", addr); err == nil || errors.Is(err, errFakeDial) {
			t.Errorf("DialFunc to %s = %v, want a refusal before dialing", addr, err)
		}
	}
	if len(d.addrs) != 0 {
		t.Errorf("the dialer was called with %v", d.addrs)
	}
}

func TestPgConfigNoLocalLookup(t *testing.T) {
	tests := []struct {
		host string
		addr string
	}{
		{host: "localhost", addr: "localhost:5432"},
		{host: "127.0.0.1", addr: "127.0.0.1:5432"},
		{host: "::1", addr: "[::1]:5432"},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			d := &fakeDialer{}
			cfg, err := pgConfig(dbSettings(tt.host), d)
			if err != nil {
				t.Fatal(err)
			}
			hosts, err := cfg.LookupFunc(context.Background(), tt.host)
			if err != nil || !slices.Equal(hosts, []string{tt.host}) {
				t.Errorf("LookupFunc = %v, %v; want [%s]", hosts, err, tt.host)
			}
			if _, err := cfg.DialFunc(context.Background(), "tcp", tt.addr); !errors.Is(err, errFakeDial) {
				t.Errorf("DialFunc = %v, want the dialer's error", err)
			}
			if !slices.Equal(d.addrs, []string{tt.addr}) {
				t.Errorf("dialer got %v, want [%s]", d.addrs, tt.addr)
			}
		})
	}
}
