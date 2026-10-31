package ssh

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// staticKeys is a KeyLoader with at most one key.
type staticKeys struct {
	key domain.KeyPair
	has bool
}

func (k staticKeys) LoadKey(context.Context) (domain.KeyPair, bool, error) { return k.key, k.has, nil }

// sshSettings points at addr with the test server's credentials.
func sshSettings(t *testing.T, addr, trusted string) domain.ConnectionSettings {
	t.Helper()

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}

	p, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}

	s := domain.ConnectionSettings{
		SSHHost: host, SSHPort: p, SSHUser: "u", SSHPassword: "pw", DBHost: "127.0.0.1", DBPort: 5432,
		DBName: "db", DBUser: "dbu", SyncPhrase: "sixteen chars ok", TrustedHostKey: trusted,
	}
	if trusted != "" {
		s.TrustedHostAddr = s.SSHAddr()
	}

	return s
}

func TestNewConnectorNil(t *testing.T) {
	if _, err := NewConnector(nil); err == nil {
		t.Error("NewConnector(nil) = nil error")
	}
}

// Compile-time checks that the adapters implement their ports.
var (
	_ usecase.RemoteConnector = (*Connector)(nil)
	_ usecase.KeyTool         = KeyTool{}
)

func TestDialSSHHostKeyUnknown(t *testing.T) {
	srv := newTestServer(t, nil)
	_, err := dialSSH(context.Background(), sshSettings(t, srv.addr, ""), nil)

	hk, ok := errors.AsType[*domain.HostKeyUnknownError](err)
	if !ok {
		t.Fatalf("dialSSH = %v, want *HostKeyUnknownError", err)
	}

	if hk.AuthorizedKey != srv.authorizedKey() {
		t.Errorf("AuthorizedKey = %q, want the server key", hk.AuthorizedKey)
	}
}

func TestDialSSHKeyTrustedForAnotherAddress(t *testing.T) {
	srv := newTestServer(t, nil)
	s := sshSettings(t, srv.addr, srv.authorizedKey())
	s.TrustedHostAddr = "old.example.org:22" // trusted for the server before the settings changed

	_, err := dialSSH(context.Background(), s, nil)

	hk, ok := errors.AsType[*domain.HostKeyUnknownError](err)
	if !ok {
		t.Fatalf("dialSSH = %v, want *HostKeyUnknownError: the key belongs to another address", err)
	}

	if hk.Addr != srv.addr {
		t.Errorf("Addr = %q, want %q", hk.Addr, srv.addr)
	}
}

func TestAuthOrder(t *testing.T) {
	srv := newTestServer(t, nil) // accepts only the password
	tool := KeyTool{}

	pair, err := tool.Generate()
	if err != nil {
		t.Fatal(err)
	}

	signer, err := gossh.ParsePrivateKey(pair.PrivatePEM)
	if err != nil {
		t.Fatal(err)
	}

	client, err := dialSSH(context.Background(), sshSettings(t, srv.addr, srv.authorizedKey()), signer)
	if err != nil {
		t.Fatalf("dialSSH: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Error(err)
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if !slices.Equal(srv.attempts, []string{"publickey", "password"}) {
		t.Errorf("auth attempts = %v, want [publickey password]", srv.attempts)
	}
}

func TestAuthByKey(t *testing.T) {
	pair, err := KeyTool{}.Generate()
	if err != nil {
		t.Fatal(err)
	}

	signer, err := gossh.ParsePrivateKey(pair.PrivatePEM)
	if err != nil {
		t.Fatal(err)
	}

	srv := newTestServer(t, func(s *testServer) { s.authorized = signer.PublicKey() })
	s := sshSettings(t, srv.addr, srv.authorizedKey())
	s.SSHPassword = ""

	client, err := dialSSH(context.Background(), s, signer)
	if err != nil {
		t.Fatalf("dialSSH with a key and no password: %v", err)
	}

	if err := client.Close(); err != nil {
		t.Error(err)
	}
}

// silentListener accepts TCP connections and never sends an SSH banner.
func silentListener(t *testing.T) string {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}

			t.Cleanup(func() { _ = c.Close() })
		}
	}()

	return ln.Addr().String()
}

func TestHandshakeTimeout(t *testing.T) {
	old := handshakeTimeout
	handshakeTimeout = 200 * time.Millisecond

	t.Cleanup(func() { handshakeTimeout = old })
	addr := silentListener(t)

	start := time.Now()

	if _, err := dialSSH(context.Background(), sshSettings(t, addr, ""), nil); err == nil {
		t.Fatal("dialSSH to a silent server: nil error")
	}

	if d := time.Since(start); d > time.Second {
		t.Errorf("dialSSH took %v, want about %v", d, handshakeTimeout)
	}

	handshakeTimeout = 10 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start = time.Now()

	if _, err := dialSSH(ctx, sshSettings(t, addr, ""), nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("dialSSH with an expiring context = %v, want DeadlineExceeded", err)
	}

	if d := time.Since(start); d > time.Second {
		t.Errorf("canceled dialSSH took %v", d)
	}
}

func TestOpenCanceledWhileChannelOpenHangs(t *testing.T) {
	srv := newTestServer(t, func(s *testServer) { s.hangChannels = true })

	c, err := NewConnector(staticKeys{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()

	_, err = c.Open(ctx, sshSettings(t, srv.addr, srv.authorizedKey()), usecase.OpenCheck)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open = %v, want DeadlineExceeded", err)
	}

	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Open took %v", d)
	}

	select {
	case <-srv.closed:
	case <-time.After(2 * time.Second):
		t.Error("the server connection stayed open")
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()

	if !slices.Equal(srv.dialed, []string{"127.0.0.1:5432"}) {
		t.Errorf("channels to %v, want [127.0.0.1:5432]", srv.dialed)
	}
}

func TestOpenCanceledWhileDatabaseHangs(t *testing.T) {
	srv := newTestServer(t, nil)
	db := silentListener(t) // the tunnel opens, PostgreSQL never answers

	c, err := NewConnector(staticKeys{})
	if err != nil {
		t.Fatal(err)
	}

	s := sshSettings(t, srv.addr, srv.authorizedKey())

	_, port, err := net.SplitHostPort(db)
	if err != nil {
		t.Fatal(err)
	}

	if s.DBPort, err = strconv.Atoi(port); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()

	if _, err := c.Open(ctx, s, usecase.OpenCheck); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Open = %v, want DeadlineExceeded", err)
	}

	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Open took %v: an SSH channel has no deadlines, the client must be closed", d)
	}
}

func TestOpenRefusesInvalidSettings(t *testing.T) {
	c, err := NewConnector(staticKeys{})
	if err != nil {
		t.Fatal(err)
	}

	s := sshSettings(t, "127.0.0.1:22", "")

	s.DBHost = "10.0.0.5"
	if _, err := c.Open(context.Background(), s, usecase.OpenCheck); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("Open = %v, want a validation error before connecting", err)
	}
}

// failingKeys is a KeyLoader whose local storage fails.
type failingKeys struct{ err error }

func (k failingKeys) LoadKey(context.Context) (domain.KeyPair, bool, error) {
	return domain.KeyPair{}, false, k.err
}

// TestOpenLocalKeyErrorIsLocal: a key that cannot be read from the local
// storage says nothing about the server, so it must reach usecase as a
// LocalError, which does not disconnect.
func TestOpenLocalKeyErrorIsLocal(t *testing.T) {
	errLocked := errors.New("storage is locked")

	tests := []struct {
		name   string
		keys   KeyLoader
		target error
	}{
		{name: "load fails", keys: failingKeys{err: errLocked}, target: errLocked},
		{name: "unreadable key", keys: staticKeys{key: domain.KeyPair{PrivatePEM: []byte("not a key")}, has: true}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewConnector(tc.keys)
			if err != nil {
				t.Fatal(err)
			}

			_, err = c.Open(context.Background(), sshSettings(t, "127.0.0.1:22", ""), usecase.OpenCheck)
			if err == nil {
				t.Fatal("Open = nil error, want the key error")
			}

			if _, ok := errors.AsType[usecase.LocalError](err); !ok {
				t.Errorf("Open = %v, want a usecase.LocalError", err)
			}

			if tc.target != nil && !errors.Is(err, tc.target) {
				t.Errorf("Open = %v, want it to wrap %v", err, tc.target)
			}
		})
	}
}

func TestDialSSHTransportErrors(t *testing.T) {
	srv := newTestServer(t, nil)

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	closedAddr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	wrongPassword := sshSettings(t, srv.addr, srv.authorizedKey())
	wrongPassword.SSHPassword = "wrong"

	tests := []struct {
		name string
		s    domain.ConnectionSettings
	}{
		{name: "closed port", s: sshSettings(t, closedAddr, "")},
		{name: "wrong password", s: wrongPassword},
		{name: "unknown host key", s: sshSettings(t, srv.addr, "")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := dialSSH(context.Background(), tc.s, nil)
			if err == nil {
				t.Fatal("dialSSH succeeded")
			}

			te, ok := errors.AsType[*TransportError](err)
			if !ok {
				t.Fatalf("dialSSH = %v (%T), want *TransportError", err, err)
			}

			if te.Error() != te.Err.Error() {
				t.Errorf("Error() = %q, want the wrapped text %q", te.Error(), te.Err.Error())
			}
		})
	}
}
