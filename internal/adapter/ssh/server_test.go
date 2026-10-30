package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

// testServer is an SSH server in the test process. It accepts the password
// "pw" for user "u" and, when authorized is set, that public key.
type testServer struct {
	addr       string
	hostSigner gossh.Signer
	authorized gossh.PublicKey
	// hangChannels makes direct-tcpip requests wait forever, as a stuck server would.
	hangChannels bool

	mu       sync.Mutex
	attempts []string      // auth methods in the order the client tried them
	dialed   []string      // direct-tcpip targets
	closed   chan struct{} // closed when a client connection ends
}

func newTestServer(t *testing.T, configure func(*testServer)) *testServer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	srv := &testServer{hostSigner: signer, closed: make(chan struct{}, 8)}
	if configure != nil {
		configure(srv)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() }) // ends the accept loop
	srv.addr = ln.Addr().String()

	cfg := &gossh.ServerConfig{
		PasswordCallback: func(c gossh.ConnMetadata, pw []byte) (*gossh.Permissions, error) {
			srv.record("password")
			if c.User() == "u" && string(pw) == "pw" {
				return nil, nil
			}
			return nil, errDenied
		},
		PublicKeyCallback: func(_ gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
			srv.record("publickey")
			if srv.authorized != nil && string(key.Marshal()) == string(srv.authorized.Marshal()) {
				return nil, nil
			}
			return nil, errDenied
		},
	}
	cfg.AddHostKey(signer)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(c, cfg)
		}
	}()
	return srv
}

var errDenied = errors.New("denied")

func (s *testServer) record(method string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attempts = append(s.attempts, method)
}

func (s *testServer) serve(c net.Conn, cfg *gossh.ServerConfig) {
	defer func() { s.closed <- struct{}{} }()
	conn, chans, reqs, err := gossh.NewServerConn(c, cfg)
	if err != nil {
		return
	}
	go gossh.DiscardRequests(reqs)
	for ch := range chans {
		if ch.ChannelType() != "direct-tcpip" {
			_ = ch.Reject(gossh.UnknownChannelType, "only direct-tcpip")
			continue
		}
		var target struct {
			Host       string
			Port       uint32
			OriginHost string
			OriginPort uint32
		}
		if err := gossh.Unmarshal(ch.ExtraData(), &target); err == nil {
			s.mu.Lock()
			s.dialed = append(s.dialed, net.JoinHostPort(target.Host, itoa(target.Port)))
			s.mu.Unlock()
		}
		if s.hangChannels {
			continue // never answer: the client waits for the channel
		}
		go s.forward(ch, target.Host, target.Port)
	}
	_ = conn.Wait()
}

func (s *testServer) forward(nc gossh.NewChannel, host string, port uint32) {
	up, err := net.Dial("tcp", net.JoinHostPort(host, itoa(port)))
	if err != nil {
		_ = nc.Reject(gossh.ConnectionFailed, err.Error())
		return
	}
	ch, reqs, err := nc.Accept()
	if err != nil {
		_ = up.Close()
		return
	}
	go gossh.DiscardRequests(reqs)
	go func() { _, _ = io.Copy(ch, up); _ = ch.CloseWrite() }()
	_, _ = io.Copy(up, ch)
	_ = up.Close()
}

// authorizedKey returns the server host key in authorized_keys format.
func (s *testServer) authorizedKey() string {
	return authorizedLine(s.hostSigner.PublicKey())
}

func itoa(p uint32) string { return strconv.FormatUint(uint64(p), 10) }
