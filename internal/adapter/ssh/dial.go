package ssh

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/nikchis/smartnote/internal/domain"
)

// contextDialer opens TCP connections; *gossh.Client opens them through the tunnel.
type contextDialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// isolatedDSN sets every parameter pgconn would otherwise take from a PG*
// environment variable or from files in the home directory (~/.pgpass,
// ~/.postgresql), so none of them can change or break the configuration.
// pgConfig then fills in the settings. A service (PGSERVICE) is the one
// exception: pgconn reads its file whenever the key is present, even empty.
// Its values are overridden like the rest, but a PGSERVICE naming a missing
// service fails parsing, as it would for psql.
const isolatedDSN = "host=localhost port=5432 dbname='' user='' password='' passfile='' " +
	"application_name='' connect_timeout=0 sslmode=disable sslkey='' sslcert='' sslrootcert='' " +
	"sslpassword='' sslsni='' sslnegotiation='' target_session_attrs=any " +
	"options='' min_protocol_version='' max_protocol_version='' channel_binding='' require_auth=''"

// pgConfig returns a pgx configuration that reaches only s.DBHost:s.DBPort
// through d. Nothing comes from the environment (PGHOST, PGSSLMODE and so
// on): no fallback hosts, TLS, runtime parameters or session checks. Names
// are not resolved locally, since localhost means the SSH server.
func pgConfig(s domain.ConnectionSettings, d contextDialer) (*pgx.ConnConfig, error) {
	cfg, err := pgx.ParseConfig(isolatedDSN)
	if err != nil {
		return nil, fmt.Errorf("database config: %w", err)
	}
	if s.DBPort < 1 || s.DBPort > 65535 {
		return nil, &domain.ValidationError{Field: "db_port", Reason: "must be between 1 and 65535"}
	}
	cfg.Host = s.DBHost
	cfg.Port = uint16(s.DBPort)
	cfg.Database = s.DBName
	cfg.User = s.DBUser
	cfg.Password = s.DBPassword
	cfg.TLSConfig = nil
	cfg.Fallbacks = nil
	cfg.RuntimeParams = map[string]string{}
	cfg.ValidateConnect = nil
	cfg.ConnectTimeout = 0 // the operation's context bounds the connection
	cfg.LookupFunc = func(_ context.Context, host string) ([]string, error) { return []string{host}, nil }

	allowed := net.JoinHostPort(s.DBHost, strconv.Itoa(s.DBPort))
	cfg.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" || addr != allowed {
			return nil, fmt.Errorf("database dial to %s %s refused: only %s is allowed", network, addr, allowed)
		}
		return d.DialContext(ctx, "tcp", addr)
	}
	return cfg, nil
}
