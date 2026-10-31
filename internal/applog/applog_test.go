package applog

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	gossh "golang.org/x/crypto/ssh"
)

const (
	secretPassword = "PLAINPWD-xyz"
	secretUser     = "alice-user"
	secretDatabase = "notes-db"
	secretHost     = "db-host.example"
)

// openTest opens a log in a new directory and returns a reader of the log
// file and the logger's Error method.
func openTest(t *testing.T) (read func() string, logError func(msg string, args ...any)) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "state")

	l, closer, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	t.Cleanup(func() { _ = closer.Close() })

	read = func() string {
		b, err := os.ReadFile(filepath.Join(dir, "smartnote.log"))
		if err != nil {
			t.Fatalf("read log: %v", err)
		}

		return string(b)
	}

	return read, l.Error
}

func TestLogNoSecrets(t *testing.T) {
	read, logError := openTest(t)

	pgErr := pgConnectError(t)
	sshErr := sshAuthError(t)
	serverErr := fmt.Errorf("backup: %w", &pgconn.PgError{
		Severity: "FATAL", Code: "28P01",
		Message: fmt.Sprintf("password authentication failed for user %q on %s", secretUser, secretHost),
	})

	logError("connect failed", "code", "database", Err(pgErr))
	logError("connect failed", "code", "ssh", Redacted(sshErr))
	logError("backup failed", "code", "database", Redacted(serverErr))

	got := read()
	for _, secret := range []string{secretPassword, secretUser, secretDatabase, secretHost} {
		if strings.Contains(got, secret) {
			t.Errorf("log contains %q:\n%s", secret, got)
		}
	}

	for _, want := range []string{"*pgconn.ConnectError", "sqlstate=28P01", "code=ssh"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
}

func TestErrKeepsTextOfOtherErrors(t *testing.T) {
	read, logError := openTest(t)

	logError("save failed", Err(fmt.Errorf("save note: %w", errors.New("disk full"))))

	if got := read(); !strings.Contains(got, "save note: disk full") {
		t.Errorf("log lacks the error text:\n%s", got)
	}
}

func TestOpenErrorLevelAndMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")

	l, closer, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	l.Info("info message")
	l.Warn("warn message")
	l.Error("error message")

	if err := closer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	path := filepath.Join(dir, "smartnote.log")

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	got := string(b)
	if strings.Contains(got, "info message") || strings.Contains(got, "warn message") {
		t.Errorf("log has records below Error:\n%s", got)
	}

	if !strings.Contains(got, `level=ERROR msg="error message"`) {
		t.Errorf("log lacks the error record in text format:\n%s", got)
	}

	if runtime.GOOS == "windows" {
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("log mode = %o, want 600", perm)
	}
}

func TestOpenAppends(t *testing.T) {
	dir := t.TempDir()

	for _, msg := range []string{"first", "second"} {
		l, closer, err := Open(dir)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		l.Error(msg)

		if err := closer.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}

	b, err := os.ReadFile(filepath.Join(dir, "smartnote.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}

	if got := string(b); !strings.Contains(got, "first") || !strings.Contains(got, "second") {
		t.Errorf("log lost a record:\n%s", got)
	}
}

// pgConnectError connects to a closed local port with a connection string
// that names the password, user, database and host.
func pgConnectError(t *testing.T) error {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	cfg, err := pgx.ParseConfig(fmt.Sprintf("postgres://%s:%s@127.0.0.1:%d/%s?sslmode=disable&connect_timeout=5",
		secretUser, secretPassword, port, secretDatabase))
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	cfg.Host = secretHost
	cfg.LookupFunc = func(context.Context, string) ([]string, error) { return []string{"127.0.0.1"}, nil }

	conn, err := pgx.ConnectConfig(context.Background(), cfg)
	if err == nil {
		_ = conn.Close(context.Background())

		t.Fatal("connect to a closed port succeeded")
	}

	if !strings.Contains(err.Error(), secretUser) {
		t.Fatalf("pgx error lacks the user, the test would prove nothing: %v", err)
	}

	return fmt.Errorf("database: %w", err)
}

// sshAuthError runs an SSH handshake against an in-process server that
// rejects the password.
func sshAuthError(t *testing.T) error {
	t.Helper()

	_, hostPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("host key: %v", err)
	}

	hostKey, err := gossh.NewSignerFromKey(hostPriv)
	if err != nil {
		t.Fatalf("host signer: %v", err)
	}

	server := &gossh.ServerConfig{
		PasswordCallback: func(gossh.ConnMetadata, []byte) (*gossh.Permissions, error) {
			return nil, errors.New("denied")
		},
	}
	server.AddHostKey(hostKey)

	// net.Pipe would deadlock: both sides send their version line before reading.
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return // the client reports the failure
		}

		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_, _, _, _ = gossh.NewServerConn(conn, server) // the client reports the outcome
		_ = conn.Close()
	}()

	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(t.Context(), "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	t.Cleanup(func() { _ = conn.Close() })

	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("deadline: %v", err)
	}

	addr := net.JoinHostPort(secretHost, "22")

	client, _, _, err := gossh.NewClientConn(conn, addr, &gossh.ClientConfig{
		User:            secretUser,
		Auth:            []gossh.AuthMethod{gossh.Password(secretPassword)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	})
	if err == nil {
		_ = client.Close()

		t.Fatal("ssh handshake with a rejected password succeeded")
	}

	return fmt.Errorf("ssh: connect %s as %s: %w", addr, secretUser, err)
}
