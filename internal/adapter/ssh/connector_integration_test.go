//go:build integration

package ssh

import (
	"context"
	"errors"
	"log"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/nikchis/smartnote/internal/adapter/sqlite"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
	"github.com/nikchis/smartnote/internal/usecase"
)

const (
	dbPassword = "dbpass"
	dbUser     = "smartnote"
)

var (
	clientKey domain.KeyPair // authorized on every test sshd
	sshHost   string
	sshPort   int
)

// startSSHD builds and starts the test SSH server.
func startSSHD(ctx context.Context) (*testcontainers.DockerContainer, error) {
	key := clientKey.PublicAuthorized

	return testcontainers.Run(ctx, "",
		testcontainers.WithDockerfile(testcontainers.FromDockerfile{
			Context:   "testdata/sshd",
			BuildArgs: map[string]*string{"AUTHORIZED_KEY": &key},
			// As docker build --network=host: the default bridge may not reach the package mirror.
			BuildOptionsModifier: func(o *client.ImageBuildOptions) { o.NetworkMode = "host" },
		}),
		testcontainers.WithExposedPorts("22/tcp"),
		testcontainers.WithWaitStrategy(wait.ForLog("Server listening on").WithStartupTimeout(time.Minute)))
}

func hostPort(ctx context.Context, c *testcontainers.DockerContainer) (host string, port int, err error) {
	if host, err = c.Host(ctx); err != nil {
		return "", 0, err
	}

	p, err := c.MappedPort(ctx, "22/tcp")
	if err != nil {
		return "", 0, err
	}

	return host, int(p.Num()), nil
}

func TestMain(m *testing.M) {
	ctx := context.Background()

	var err error
	if clientKey, err = (KeyTool{}).Generate(); err != nil {
		log.Fatal(err)
	}

	sshd, err := startSSHD(ctx)
	if err != nil {
		log.Fatalf("start sshd: %v", err)
	}

	code := 1

	defer func() {
		if err := sshd.Terminate(ctx); err != nil {
			log.Printf("stop sshd: %v", err)
		}

		os.Exit(code)
	}()
	// PostgreSQL shares the SSH server's network namespace and listens on its loopback only.
	pg, err := testcontainers.Run(ctx, "postgres:17-alpine",
		testcontainers.WithEnv(map[string]string{"POSTGRES_USER": dbUser, "POSTGRES_PASSWORD": dbPassword, "POSTGRES_DB": dbUser}),
		testcontainers.WithCmd("postgres", "-c", "listen_addresses=127.0.0.1"),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.NetworkMode = container.NetworkMode("container:" + sshd.GetContainerID())
		}),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(time.Minute)))
	if err != nil {
		log.Printf("start postgres: %v", err)
		return
	}
	defer func() {
		if err := pg.Terminate(ctx); err != nil {
			log.Printf("stop postgres: %v", err)
		}
	}()

	if sshHost, sshPort, err = hostPort(ctx, sshd); err != nil {
		log.Printf("sshd address: %v", err)
		return
	}

	code = m.Run()
}

// newDatabase creates a database through the tunnel and returns its name.
func newDatabase(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	s := settingsFor("postgres", "phrase is unused here", "testpass")
	s.TrustedHostKey = trustedKey(t)

	client, err := dialSSH(ctx, s, nil)
	if err != nil {
		t.Fatalf("ssh: %v", err)
	}

	defer func() {
		if err := closeClient(client); err != nil {
			t.Error(err)
		}
	}()

	cfg, err := pgConfig(s, client)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("database: %v", err)
	}
	defer func() {
		if err := conn.Close(ctx); err != nil {
			t.Error(err)
		}
	}()

	name := "t_" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}

	return name
}

// trustedKey returns the server host key by asking it once.
func trustedKey(t *testing.T) string {
	t.Helper()

	_, err := dialSSH(context.Background(), settingsFor("postgres", "", "testpass"), nil)

	hk, ok := errors.AsType[*domain.HostKeyUnknownError](err)
	if !ok {
		t.Fatalf("first contact = %v, want *HostKeyUnknownError", err)
	}

	return hk.AuthorizedKey
}

func settingsFor(db, phrase, sshPassword string) domain.ConnectionSettings {
	return domain.ConnectionSettings{
		SSHHost: sshHost, SSHPort: sshPort, SSHUser: "tester", SSHPassword: sshPassword,
		DBHost: "127.0.0.1", DBPort: 5432, DBName: db, DBUser: dbUser, DBPassword: dbPassword, SyncPhrase: phrase,
	}
}

// machine is one device: its own local storage and the scenarios over it.
type machine struct {
	store *sqlite.Store
	sync  *usecase.Sync
	notes *usecase.Notes
	conn  *usecase.Connection
}

var testKDF = sealbox.KDFParams{Time: 1, MemoryKiB: 19456, Threads: 1}

func newMachine(t *testing.T, s domain.ConnectionSettings, key *domain.KeyPair) *machine {
	t.Helper()

	ctx := context.Background()

	store, err := sqlite.OpenWithKDF(t.TempDir(), testKDF)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	if err := store.Create(ctx, "local password"); err != nil {
		t.Fatal(err)
	}

	if key != nil {
		if err := store.SaveKey(ctx, *key); err != nil {
			t.Fatal(err)
		}
	}

	connector, err := NewConnector(store)
	if err != nil {
		t.Fatal(err)
	}

	gate, changes := &usecase.Gate{}, &usecase.ChangeTracker{}

	sync, err := usecase.NewSync(usecase.SyncDeps{Local: store, Settings: store, Remote: connector, Changes: changes,
		IDs: uuid.New, Gate: gate, Warn: func(err error) { t.Logf("warning: %v", err) }})
	if err != nil {
		t.Fatal(err)
	}

	notes, err := usecase.NewNotes(store, func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }, uuid.New, changes)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := usecase.NewConnection(store, connector, gate, sync.ResetConnection)
	if err != nil {
		t.Fatal(err)
	}

	if err := conn.Save(ctx, s); err != nil {
		t.Fatal(err)
	}

	return &machine{store: store, sync: sync, notes: notes, conn: conn}
}

// connect runs the connect check, trusting the server key on first contact
// as the user would.
func (m *machine) connect(t *testing.T) usecase.ConnectOutcome {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	out, err := m.sync.Connect(ctx)
	if hk, ok := errors.AsType[*domain.HostKeyUnknownError](err); ok {
		if err := m.conn.TrustHostKey(ctx, hk.AuthorizedKey); err != nil {
			t.Fatal(err)
		}

		out, err = m.sync.Connect(ctx)
	}

	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	return out
}

func TestTunnelEndToEnd(t *testing.T) {
	ctx := context.Background()
	db := newDatabase(t)
	a := newMachine(t, settingsFor(db, "sixteen chars ok", ""), &clientKey)  // key only
	b := newMachine(t, settingsFor(db, "sixteen chars ok", "testpass"), nil) // password only

	note, err := a.notes.Create(ctx, "Привет", "from machine A")
	if err != nil {
		t.Fatal(err)
	}

	if out := a.connect(t); out.Status != usecase.StatusCreated {
		t.Fatalf("A: Connect = %q, want created", out.Status)
	}

	out := b.connect(t)
	if out.Status != usecase.StatusRemoteNewer || out.Restore != (usecase.Counts{Added: 1}) {
		t.Fatalf("B: Connect = %+v, want remote_newer adding 1", out)
	}

	if err := b.sync.CommitRestore(ctx); err != nil {
		t.Fatalf("B: CommitRestore: %v", err)
	}

	got, err := b.store.GetNote(ctx, note.ID)
	if err != nil || got.Name != note.Name || got.Info != note.Info {
		t.Fatalf("B: note = %+v, %v; want %+v", got, err, note)
	}

	if _, err := b.notes.Save(ctx, note.ID, note.Name, "edited on B"); err != nil {
		t.Fatal(err)
	}

	if _, err := b.sync.PrepareBackup(ctx); err != nil {
		t.Fatalf("B: PrepareBackup: %v", err)
	}

	if err := b.sync.CommitBackup(ctx); err != nil {
		t.Fatalf("B: CommitBackup: %v", err)
	}

	if out := a.connect(t); out.Status != usecase.StatusRemoteNewer {
		t.Fatalf("A: Connect after B's Backup = %q, want remote_newer", out.Status)
	}

	if err := a.sync.CommitRestore(ctx); err != nil {
		t.Fatal(err)
	}

	if got, err := a.store.GetNote(ctx, note.ID); err != nil || got.Info != "edited on B" {
		t.Errorf("A: note = %+v, %v; want B's text", got, err)
	}

	versions, err := a.notes.Versions(ctx, note.ID)
	if err != nil || len(versions) != 1 || versions[0].Info != "from machine A" {
		t.Errorf("A: versions = %+v, %v; want the replaced text", versions, err)
	}
}

func TestPhraseNFCvsNFD(t *testing.T) {
	ctx := context.Background()
	db := newDatabase(t)
	a := newMachine(t, settingsFor(db, "passé phrase sixteen", "testpass"), nil)

	b := newMachine(t, settingsFor(db, "passé phrase sixteen", "testpass"), nil)
	if _, err := a.notes.Create(ctx, "n", "i"); err != nil {
		t.Fatal(err)
	}

	if out := a.connect(t); out.Status != usecase.StatusCreated {
		t.Fatalf("A: Connect = %q", out.Status)
	}

	if out := b.connect(t); out.Status != usecase.StatusRemoteNewer {
		t.Errorf("B with the NFD phrase: Connect = %q, want remote_newer", out.Status)
	}
}

func TestWrongPhraseOverTunnel(t *testing.T) {
	ctx := context.Background()
	db := newDatabase(t)
	a := newMachine(t, settingsFor(db, "sixteen chars ok", "testpass"), nil)
	a.connect(t)
	b := newMachine(t, settingsFor(db, "another phrase 16", "testpass"), nil)
	s := settingsFor(db, "another phrase 16", "testpass")

	s.TrustedHostKey = trustedKey(t)
	if err := b.conn.Save(ctx, s); err != nil {
		t.Fatal(err)
	}

	if _, err := b.sync.Connect(ctx); !errors.Is(err, domain.ErrWrongSyncPhrase) {
		t.Errorf("Connect = %v, want ErrWrongSyncPhrase", err)
	}
}

func TestHostKeyChangedAfterTrust(t *testing.T) {
	ctx := context.Background()
	trusted := trustedKey(t)

	other, err := startSSHD(ctx)
	if err != nil {
		t.Fatalf("start a second sshd: %v", err)
	}

	t.Cleanup(func() {
		if err := other.Terminate(context.Background()); err != nil {
			t.Error(err)
		}
	})

	host, port, err := hostPort(ctx, other)
	if err != nil {
		t.Fatal(err)
	}

	c, err := NewConnector(staticKeys{})
	if err != nil {
		t.Fatal(err)
	}

	s := settingsFor("postgres", "sixteen chars ok", "testpass")
	s.SSHHost, s.SSHPort, s.TrustedHostKey = host, port, trusted

	_, err = c.Open(ctx, s, usecase.OpenCheck)
	if _, ok := errors.AsType[*domain.HostKeyMismatchError](err); !ok {
		t.Fatalf("Open = %v, want *HostKeyMismatchError", err)
	}
}
