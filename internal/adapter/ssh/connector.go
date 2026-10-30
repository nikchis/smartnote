package ssh

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/adapter/postgres"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
	"github.com/nikchis/smartnote/internal/usecase"
)

// handshakeTimeout bounds TCP and the SSH handshake, as in v1 (spec §6.1).
var handshakeTimeout = 5 * time.Second

// KeyLoader reads the client SSH key; sqlite.Store implements it.
type KeyLoader interface {
	LoadKey(ctx context.Context) (domain.KeyPair, bool, error)
}

// Connector opens remote sessions: SSH, then PostgreSQL through the tunnel,
// then the storage check. It implements usecase.RemoteConnector.
type Connector struct {
	keys KeyLoader
}

// NewConnector returns a Connector that authenticates with the key from keys
// when there is one.
func NewConnector(keys KeyLoader) (*Connector, error) {
	if keys == nil {
		return nil, errors.New("ssh: NewConnector: nil key loader")
	}

	return &Connector{keys: keys}, nil
}

// Open connects with s and returns a checked session; the session closes the
// SSH client. Ending ctx at any point closes everything opened so far.
func (c *Connector) Open(ctx context.Context, s domain.ConnectionSettings, mode usecase.OpenMode) (usecase.RemoteSession, error) {
	s = s.WithDefaults()
	if err := s.Validate(); err != nil {
		return nil, err
	}

	if s.IsEmpty() {
		return nil, domain.ErrNotConnected
	}

	signer, err := c.signer(ctx)
	if err != nil {
		return nil, err
	}

	client, err := dialSSH(ctx, s, signer)
	if err != nil {
		return nil, err
	}

	sess, err := openDatabase(ctx, s, client, mode)
	if err != nil {
		return nil, withContext(ctx, err)
	}

	return sess, nil
}

func (c *Connector) signer(ctx context.Context) (gossh.Signer, error) {
	pair, ok, err := c.keys.LoadKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("load ssh key: %w", err)
	}

	if !ok {
		return nil, nil
	}

	signer, err := gossh.ParsePrivateKey(pair.PrivatePEM)
	if err != nil {
		return nil, fmt.Errorf("ssh key: %w", err)
	}

	return signer, nil
}

// openDatabase connects to PostgreSQL through client and checks the storage.
// Until it returns, ending ctx closes client, which also frees a channel open
// that the server never answers.
func openDatabase(ctx context.Context, s domain.ConnectionSettings, client *gossh.Client, mode usecase.OpenMode) (usecase.RemoteSession, error) {
	stop := context.AfterFunc(ctx, func() { _ = client.Close() }) // the operation is over; see withContext
	defer stop()

	abort := func() { _ = client.Close() } // a second Close only reports that it is closed

	cfg, err := pgConfig(s, client)
	if err != nil {
		return nil, errors.Join(err, closeClient(client))
	}

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("database: %w", err), closeClient(client))
	}

	sess, err := postgres.NewSession(ctx, conn, postgres.Options{
		Mode: mode, Phrase: s.SyncPhrase, KDF: sealbox.DefaultKDF(), Abort: abort,
	})
	if err != nil {
		return nil, err // NewSession closed conn and called abort
	}

	return sess, nil
}

// dialSSH opens TCP and runs the SSH handshake within handshakeTimeout and
// ctx. It authenticates with signer when given, then with the password.
func dialSSH(ctx context.Context, s domain.ConnectionSettings, signer gossh.Signer) (*gossh.Client, error) {
	addr := net.JoinHostPort(s.SSHHost, strconv.Itoa(s.SSHPort))

	var auth []gossh.AuthMethod
	if signer != nil {
		auth = append(auth, gossh.PublicKeys(signer))
	}

	if s.SSHPassword != "" || signer == nil {
		auth = append(auth, gossh.Password(s.SSHPassword))
	}

	config := &gossh.ClientConfig{
		User:              s.SSHUser,
		Auth:              auth,
		HostKeyCallback:   hostKeyCallback(s.TrustedHostKey),
		HostKeyAlgorithms: pinnedAlgorithms(s.TrustedHostKey),
	}

	hctx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	raw, err := (&net.Dialer{}).DialContext(hctx, "tcp", addr)
	if err != nil {
		return nil, withContext(ctx, fmt.Errorf("ssh: connect %s: %w", addr, err))
	}
	// The handshake has no context: a deadline bounds it and ending ctx closes the socket.
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() }) // reported through ctx below
	if err := raw.SetDeadline(time.Now().Add(handshakeTimeout)); err != nil {
		stop()
		return nil, errors.Join(err, raw.Close())
	}

	conn, chans, reqs, err := gossh.NewClientConn(raw, addr, config)
	stopped := stop()

	if err != nil {
		if cerr := raw.Close(); cerr != nil && !errors.Is(cerr, net.ErrClosed) {
			err = errors.Join(err, cerr)
		}

		return nil, withContext(ctx, err)
	}

	if !stopped {
		return nil, errors.Join(ctx.Err(), conn.Close())
	}

	if err := raw.SetDeadline(time.Time{}); err != nil {
		return nil, errors.Join(err, conn.Close())
	}

	return gossh.NewClient(conn, chans, reqs), nil
}

// withContext adds ctx's error to err when ctx has ended, so that callers see
// a timeout or a cancellation rather than the closed connection it caused.
func withContext(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil && !errors.Is(err, cerr) {
		return errors.Join(cerr, err)
	}

	return err
}

func closeClient(client *gossh.Client) error {
	if err := client.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return fmt.Errorf("close ssh client: %w", err)
	}

	return nil
}
