//go:build integration

package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// adminConfig connects to the test server as its superuser.
var adminConfig *pgx.ConnConfig

func TestMain(m *testing.M) {
	ctx := context.Background()

	c, err := tcpostgres.Run(ctx, "postgres:17-alpine",
		tcpostgres.WithUsername("admin"), tcpostgres.WithPassword("admin"), tcpostgres.WithDatabase("postgres"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		log.Fatalf("start postgres: %v", err)
	}

	dsn, err := c.ConnectionString(ctx, "sslmode=disable")
	if err == nil {
		adminConfig, err = pgx.ParseConfig(dsn)
	}

	code := 1

	if err != nil {
		log.Printf("postgres config: %v", err)
	} else {
		code = m.Run()
	}

	if err := c.Terminate(ctx); err != nil {
		log.Printf("stop postgres: %v", err)
	}

	os.Exit(code)
}

func randomName(prefix string) string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}

	return prefix + hex.EncodeToString(b)
}

// newDatabase creates an empty database and returns the superuser config for it.
func newDatabase(t *testing.T) *pgx.ConnConfig {
	t.Helper()

	name := randomName("t_")
	adminExec(t, adminConfig, "CREATE DATABASE "+name)
	cfg := adminConfig.Copy()
	cfg.Database = name

	return cfg
}

func connect(t *testing.T, cfg *pgx.ConnConfig) *pgx.Conn {
	t.Helper()

	conn, err := pgx.ConnectConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	return conn
}

func adminExec(t *testing.T, cfg *pgx.ConnConfig, sql string, args ...any) {
	t.Helper()

	conn := connect(t, cfg)
	defer func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()

	if _, err := conn.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func adminQueryRow(t *testing.T, cfg *pgx.ConnConfig, dest []any, sql string, args ...any) {
	t.Helper()

	conn := connect(t, cfg)
	defer func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()

	if err := conn.QueryRow(context.Background(), sql, args...).Scan(dest...); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
}

func open(t *testing.T, cfg *pgx.ConnConfig, mode usecase.OpenMode, phrase string) (*Session, error) {
	t.Helper()

	s, err := NewSession(context.Background(), connect(t, cfg), Options{Mode: mode, Phrase: phrase, KDF: testKDF})
	if err == nil {
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		})
	}

	return s, err
}

func closeAtEnd(t *testing.T, s *Session) {
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
}

func mustOpen(t *testing.T, cfg *pgx.ConnConfig, mode usecase.OpenMode) *Session {
	t.Helper()

	s, err := open(t, cfg, mode, testPhrase)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	return s
}

func testNote(name, info string) domain.Note {
	return domain.Note{ID: uuid.New(), Name: name, Info: info, Updated: time.UnixMilli(time.Now().UnixMilli()).UTC()}
}

func revision(t *testing.T, cfg *pgx.ConnConfig) int64 {
	t.Helper()

	var r int64
	adminQueryRow(t, cfg, []any{&r}, "SELECT revision FROM smartnote.sync_state WHERE id = 1")

	return r
}

func noteCount(t *testing.T, cfg *pgx.ConnConfig) int {
	t.Helper()

	var n int
	adminQueryRow(t, cfg, []any{&n}, "SELECT count(*) FROM smartnote.notes")

	return n
}

func schemaExists(t *testing.T, cfg *pgx.ConnConfig) bool {
	t.Helper()

	var ok bool
	adminQueryRow(t, cfg, []any{&ok}, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'smartnote')")

	return ok
}

func TestCreateSchemaOnce(t *testing.T) {
	cfg := newDatabase(t)

	var wg sync.WaitGroup

	sessions := make([]*Session, 2)
	errs := make([]error, 2)

	for i := range sessions {
		wg.Go(func() {
			s, err := NewSession(context.Background(), connect(t, cfg), Options{Mode: usecase.OpenCreate, Phrase: testPhrase, KDF: testKDF})
			sessions[i], errs[i] = s, err
		})
	}

	wg.Wait()

	created := 0
	ids := map[uuid.UUID]bool{}

	for i, s := range sessions {
		if errs[i] != nil {
			t.Fatalf("session %d: %v", i, errs[i])
		}

		closeAtEnd(t, s)

		if s.Created() {
			created++
		}

		snap, err := s.Snapshot(context.Background())
		if err != nil {
			t.Fatalf("Snapshot: %v", err)
		}

		ids[snap.ServerID] = true
	}

	if created != 1 || len(ids) != 1 {
		t.Errorf("created %d times, %d server ids; want 1 and 1", created, len(ids))
	}
}

func TestWrongPhrase(t *testing.T) {
	cfg := newDatabase(t)
	mustOpen(t, cfg, usecase.OpenCreate)

	for _, mode := range []usecase.OpenMode{usecase.OpenCreate, usecase.OpenCheck} {
		if _, err := open(t, cfg, mode, "another phrase 16"); !errors.Is(err, domain.ErrWrongSyncPhrase) {
			t.Errorf("mode %d: NewSession = %v, want ErrWrongSyncPhrase", mode, err)
		}
	}
}

func TestCheckModeEmpty(t *testing.T) {
	cfg := newDatabase(t)

	s := mustOpen(t, cfg, usecase.OpenCheck)
	if !s.Empty() || s.Created() {
		t.Errorf("Empty = %v, Created = %v; want true, false", s.Empty(), s.Created())
	}

	if schemaExists(t, cfg) {
		t.Error("OpenCheck created the schema")
	}

	if _, err := s.Snapshot(context.Background()); err == nil {
		t.Error("Snapshot of an empty server: nil error")
	}

	adminExec(t, cfg, "CREATE SCHEMA smartnote")
	adminExec(t, cfg, ddl)

	s2 := mustOpen(t, cfg, usecase.OpenCheck)
	if !s2.Empty() {
		t.Error("tables without a meta row: Empty = false")
	}
}

func TestSchemaCreatedOnCleanDB(t *testing.T) {
	cfg := newDatabase(t)

	s := mustOpen(t, cfg, usecase.OpenCreate)
	if !s.Created() {
		t.Error("Created = false on a clean database")
	}

	if r := revision(t, cfg); r != 0 {
		t.Errorf("revision = %d, want 0", r)
	}

	var tables int
	adminQueryRow(t, cfg, []any{&tables}, "SELECT count(*) FROM pg_tables WHERE schemaname = 'smartnote'")

	if tables != 3 {
		t.Errorf("tables = %d, want 3", tables)
	}

	again := mustOpen(t, cfg, usecase.OpenCreate)
	if again.Created() {
		t.Error("second OpenCreate: Created = true")
	}
}

// appRole creates a login role without CREATE on the database and returns
// its config for cfg's database.
func appRole(t *testing.T, cfg *pgx.ConnConfig) (app *pgx.ConnConfig, role string) {
	t.Helper()

	role = randomName("app_")
	adminExec(t, cfg, fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD 'pw'", role))
	adminExec(t, cfg, fmt.Sprintf("REVOKE CREATE ON DATABASE %s FROM PUBLIC", cfg.Database))
	app = cfg.Copy()
	app.User, app.Password = role, "pw"

	return app, role
}

func TestPrecreatedSchemaWithoutDBCreate(t *testing.T) {
	cfg := newDatabase(t)
	app, role := appRole(t, cfg)
	adminExec(t, cfg, "CREATE SCHEMA smartnote AUTHORIZATION "+role)

	s := mustOpen(t, app, usecase.OpenCreate)
	if !s.Created() {
		t.Error("Created = false")
	}

	if _, err := s.Backup(context.Background(), []domain.Note{testNote("a", "b")}, mustServerID(t, s), 0); err != nil {
		t.Errorf("Backup as the schema owner: %v", err)
	}

	if again := mustOpen(t, app, usecase.OpenCreate); again.Created() {
		t.Error("second OpenCreate: Created = true")
	}
}

func TestNoSchemaNoRight(t *testing.T) {
	cfg := newDatabase(t)
	app, _ := appRole(t, cfg)

	_, err := open(t, app, usecase.OpenCreate, testPhrase)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("NewSession = %v, want permission denied", err)
	}

	if schemaExists(t, cfg) {
		t.Error("a schema was created")
	}
}

func mustServerID(t *testing.T, s *Session) uuid.UUID {
	t.Helper()

	snap, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	return snap.ServerID
}

func TestBackupSnapshotRoundTrip(t *testing.T) {
	cfg := newDatabase(t)
	ctx := context.Background()
	s := mustOpen(t, cfg, usecase.OpenCreate)
	serverID := mustServerID(t, s)
	notes := []domain.Note{testNote("Привет 😀", "line1\nline2"), testNote("b", strings.Repeat("x", domain.MaxInfoLen))}

	rev, err := s.Backup(ctx, notes, serverID, 0)
	if err != nil || rev != 1 {
		t.Fatalf("Backup = %d, %v; want 1, nil", rev, err)
	}

	other := mustOpen(t, cfg, usecase.OpenCheck)

	snap, err := other.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if snap.Revision != 1 || snap.ServerID != serverID || len(snap.Notes) != 2 {
		t.Fatalf("snapshot = rev %d, server %v, %d notes", snap.Revision, snap.ServerID, len(snap.Notes))
	}

	byID := map[uuid.UUID]domain.Note{}
	for _, n := range snap.Notes {
		byID[n.ID] = n
	}

	for _, n := range notes {
		if got := byID[n.ID]; got != n {
			t.Errorf("note %v = %+v, want %+v", n.ID, got, n)
		}
	}

	rev, err = s.Backup(ctx, notes[:1], serverID, 1)
	if err != nil || rev != 2 || noteCount(t, cfg) != 1 {
		t.Errorf("Backup of one note = %d, %v, %d notes; want 2, nil, 1", rev, err, noteCount(t, cfg))
	}

	rev, err = s.Backup(ctx, nil, serverID, 2)
	if err != nil || rev != 3 || noteCount(t, cfg) != 0 {
		t.Errorf("Backup of no notes = %d, %v, %d notes; want 3, nil, 0", rev, err, noteCount(t, cfg))
	}
}

func TestBackupServerReplacedSameRevision(t *testing.T) {
	cfg := newDatabase(t)
	ctx := context.Background()
	s := mustOpen(t, cfg, usecase.OpenCreate)

	serverA := mustServerID(t, s)
	if _, err := s.Backup(ctx, nil, serverA, 0); err != nil {
		t.Fatal(err)
	}

	adminExec(t, cfg, "UPDATE smartnote.meta SET server_id = $1", uuid.NewString())

	for range 2 {
		adminExec(t, cfg, "INSERT INTO smartnote.notes VALUES ($1, now(), '\\x00', '\\x00')", uuid.NewString())
	}

	if _, err := s.Backup(ctx, []domain.Note{testNote("a", "b")}, serverA, 1); !errors.Is(err, domain.ErrRemoteChanged) {
		t.Fatalf("Backup = %v, want ErrRemoteChanged", err)
	}

	if n, r := noteCount(t, cfg), revision(t, cfg); n != 2 || r != 1 {
		t.Errorf("server = %d notes at revision %d, want 2 at 1", n, r)
	}
}

// ackDropper passes traffic through until armed; then it sends a message
// with COMMIT, drops everything the server answers, waits for the server to
// commit and closes the connection. pgx reads in the background, so the
// answer is dropped in Read rather than by closing early.
type ackDropper struct {
	net.Conn
	armed   *atomic.Bool
	dropped atomic.Bool
}

func (c *ackDropper) Write(b []byte) (int, error) {
	commit := c.armed.Load() && bytes.Contains(bytes.ToLower(b), []byte("commit"))
	if commit {
		c.dropped.Store(true)
	}

	n, err := c.Conn.Write(b)

	if commit {
		time.Sleep(200 * time.Millisecond)

		_ = c.Close() // the test wants the connection gone; its error does not matter
	}

	return n, err
}

func (c *ackDropper) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if c.dropped.Load() {
		return 0, net.ErrClosed
	}

	return n, err
}

func TestBackupCommitAckLost(t *testing.T) {
	cfg := newDatabase(t)
	ctx := context.Background()

	mustOpen(t, cfg, usecase.OpenCreate)

	armed := &atomic.Bool{}
	dropCfg := cfg.Copy()
	dropCfg.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		c, err := (&net.Dialer{}).DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		return &ackDropper{Conn: c, armed: armed}, nil
	}

	s, err := NewSession(ctx, connect(t, dropCfg), Options{Mode: usecase.OpenCheck, Phrase: testPhrase, KDF: testKDF})
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = s.Close() }() // the connection is broken on purpose

	serverID := mustServerID(t, s)

	armed.Store(true)

	if _, err := s.Backup(ctx, []domain.Note{testNote("a", "b")}, serverID, 0); err == nil {
		t.Fatal("Backup with a lost COMMIT answer: nil error")
	}

	if r := revision(t, cfg); r != 1 {
		t.Errorf("revision seen by another connection = %d; the outcome is unknown to the client, the server committed", r)
	}
}

func TestConcurrentBackupDisjoint(t *testing.T) {
	cfg := newDatabase(t)
	ctx := context.Background()
	first := mustOpen(t, cfg, usecase.OpenCreate)
	second := mustOpen(t, cfg, usecase.OpenCreate)
	serverID := mustServerID(t, first)
	sets := [][]domain.Note{{testNote("a", "a")}, {testNote("b", "b")}}

	errs := make([]error, 2)

	var wg sync.WaitGroup
	for i, s := range []*Session{first, second} {
		wg.Go(func() { _, errs[i] = s.Backup(ctx, sets[i], serverID, 0) })
	}

	wg.Wait()

	winner := -1

	for i, err := range errs {
		switch {
		case err == nil:
			winner = i
		case !errors.Is(err, domain.ErrRemoteChanged):
			t.Fatalf("Backup %d = %v", i, err)
		}
	}

	if winner < 0 || (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("errors = %v, want exactly one success", errs)
	}

	snap, err := mustOpen(t, cfg, usecase.OpenCheck).Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if snap.Revision != 1 || len(snap.Notes) != 1 || snap.Notes[0].ID != sets[winner][0].ID {
		t.Errorf("server = rev %d, %+v; want the winner's set at 1", snap.Revision, snap.Notes)
	}
}

func TestSnapshotConsistent(t *testing.T) {
	cfg := newDatabase(t)
	ctx := context.Background()
	reader := mustOpen(t, cfg, usecase.OpenCreate)
	writer := mustOpen(t, cfg, usecase.OpenCreate)
	serverID := mustServerID(t, reader)

	old := testNote("old", "old")
	if _, err := writer.Backup(ctx, []domain.Note{old}, serverID, 0); err != nil {
		t.Fatal(err)
	}

	var hookErr error

	reader.snapshotHook = func() {
		_, hookErr = writer.Backup(ctx, []domain.Note{testNote("new", "new")}, serverID, 1)
	}

	snap, err := reader.Snapshot(ctx)
	if err != nil || hookErr != nil {
		t.Fatalf("Snapshot = %v, Backup in between = %v", err, hookErr)
	}

	if snap.Revision != 1 || len(snap.Notes) != 1 || snap.Notes[0] != old {
		t.Errorf("snapshot = rev %d, %+v; want the old state", snap.Revision, snap.Notes)
	}
}

func TestBackupCanceledRollsBack(t *testing.T) {
	cfg := newDatabase(t)
	s := mustOpen(t, cfg, usecase.OpenCreate)

	serverID := mustServerID(t, s)
	if _, err := s.Backup(context.Background(), []domain.Note{testNote("keep", "x")}, serverID, 0); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.backupHook = cancel
	if _, err := s.Backup(ctx, []domain.Note{testNote("a", "a"), testNote("b", "b")}, serverID, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("Backup = %v, want context.Canceled", err)
	}

	if n, r := noteCount(t, cfg), revision(t, cfg); n != 1 || r != 1 {
		t.Errorf("server = %d notes at revision %d, want 1 at 1", n, r)
	}
}

func TestTamperedRow(t *testing.T) {
	cfg := newDatabase(t)
	ctx := context.Background()
	s := mustOpen(t, cfg, usecase.OpenCreate)

	a, b := testNote("a", "secret a"), testNote("b", "secret b")
	if _, err := s.Backup(ctx, []domain.Note{a, b}, mustServerID(t, s), 0); err != nil {
		t.Fatal(err)
	}

	adminExec(t, cfg, `UPDATE smartnote.notes n SET info = o.info FROM smartnote.notes o
		WHERE n.note_id <> o.note_id`)

	if _, err := s.Snapshot(ctx); !errors.Is(err, errNoteDoesNotOpen) {
		t.Errorf("Snapshot = %v, want errNoteDoesNotOpen", err)
	}
}

func TestServerKDFOutOfRange(t *testing.T) {
	cfg := newDatabase(t)
	mustOpen(t, cfg, usecase.OpenCreate)
	adminExec(t, cfg, `UPDATE smartnote.meta SET kdf_params = '{"t":3,"m":4194304,"p":4}'`)

	for _, mode := range []usecase.OpenMode{usecase.OpenCreate, usecase.OpenCheck} {
		start := time.Now()

		if _, err := open(t, cfg, mode, testPhrase); !errors.Is(err, domain.ErrUnsupportedFormat) {
			t.Errorf("mode %d: NewSession = %v, want ErrUnsupportedFormat", mode, err)
		}

		if d := time.Since(start); d > 5*time.Second {
			t.Errorf("mode %d took %v: Argon2id ran with forged parameters", mode, d)
		}
	}
}

func TestServerFormatNewer(t *testing.T) {
	cfg := newDatabase(t)
	mustOpen(t, cfg, usecase.OpenCreate)
	adminExec(t, cfg, "UPDATE smartnote.meta SET format_version = 2")

	if _, err := open(t, cfg, usecase.OpenCreate, testPhrase); !errors.Is(err, domain.ErrUnsupportedFormat) {
		t.Errorf("NewSession = %v, want ErrUnsupportedFormat", err)
	}
}

func TestNoPlaintextInTables(t *testing.T) {
	cfg := newDatabase(t)
	ctx := context.Background()

	s := mustOpen(t, cfg, usecase.OpenCreate)
	if _, err := s.Backup(ctx, []domain.Note{testNote("PLAINNAME-xyz", "PLAININFO-xyz")}, mustServerID(t, s), 0); err != nil {
		t.Fatal(err)
	}

	var dump string
	adminQueryRow(t, cfg, []any{&dump}, `SELECT string_agg(encode(name, 'escape') || encode(info, 'escape'), '')
		FROM smartnote.notes`)

	var meta string
	adminQueryRow(t, cfg, []any{&meta}, `SELECT encode(key_check, 'escape') FROM smartnote.meta`)

	for _, secret := range []string{"PLAINNAME-xyz", "PLAININFO-xyz", testPhrase, keyCheckPlain} {
		if strings.Contains(dump+meta, secret) {
			t.Errorf("tables contain %q", secret)
		}
	}
}

func TestCloseCallsAbortOnce(t *testing.T) {
	cfg := newDatabase(t)

	var aborts atomic.Int32

	s, err := NewSession(context.Background(), connect(t, cfg), Options{Mode: usecase.OpenCreate, Phrase: testPhrase, KDF: testKDF,
		Abort: func() { aborts.Add(1) }})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	if aborts.Load() != 1 {
		t.Errorf("Abort called %d times, want 1", aborts.Load())
	}

	var failedAborts atomic.Int32

	_, err = NewSession(context.Background(), connect(t, cfg), Options{Mode: usecase.OpenCheck, Phrase: "another phrase 16", KDF: testKDF,
		Abort: func() { failedAborts.Add(1) }})
	if !errors.Is(err, domain.ErrWrongSyncPhrase) || failedAborts.Load() != 1 {
		t.Errorf("failed NewSession = %v, aborts %d; want ErrWrongSyncPhrase and 1", err, failedAborts.Load())
	}
}

func TestContextAbortsTransport(t *testing.T) {
	cfg := newDatabase(t)

	var aborted atomic.Bool

	ctx, cancel := context.WithCancel(context.Background())

	s, err := NewSession(context.Background(), connect(t, cfg), Options{Mode: usecase.OpenCreate, Phrase: testPhrase, KDF: testKDF,
		Abort: func() { aborted.Store(true) }})
	if err != nil {
		t.Fatal(err)
	}

	closeAtEnd(t, s)

	s.snapshotHook = func() {
		cancel()
		time.Sleep(50 * time.Millisecond) // AfterFunc runs Abort in its own goroutine
	}
	if _, err := s.Snapshot(ctx); err == nil {
		t.Error("Snapshot with a canceled context: nil error")
	}

	if !aborted.Load() {
		t.Error("canceling the context did not call Abort")
	}
}
