package usecase

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

var errStore = errors.New("store failure")

// fakeNoteStore keeps notes in a map and versions in a slice. fail, when set,
// is returned by every write; onWrite, when set, runs at the end of every
// write, outside the lock.
type fakeNoteStore struct {
	mu       sync.Mutex
	notes    map[uuid.UUID]domain.Note
	versions []domain.NoteVersion
	writes   int
	fail     error
	onWrite  func()
}

func newFakeNoteStore(notes ...domain.Note) *fakeNoteStore {
	s := &fakeNoteStore{notes: map[uuid.UUID]domain.Note{}}
	for _, n := range notes {
		s.notes[n.ID] = n
	}

	return s
}

func (s *fakeNoteStore) ListNotes(context.Context) ([]domain.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.Note, 0, len(s.notes))
	for _, n := range s.notes {
		out = append(out, n)
	}

	return out, nil
}

func (s *fakeNoteStore) GetNote(_ context.Context, id uuid.UUID) (domain.Note, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	n, ok := s.notes[id]
	if !ok {
		return domain.Note{}, fmt.Errorf("note %s: not found", id)
	}

	return n, nil
}

func (s *fakeNoteStore) write() error {
	s.writes++
	return s.fail
}

// hook runs onWrite after a write released the lock.
func (s *fakeNoteStore) hook() {
	s.mu.Lock()
	h := s.onWrite
	s.mu.Unlock()

	if h != nil {
		h()
	}
}

func (s *fakeNoteStore) InsertNote(_ context.Context, n domain.Note) error {
	defer s.hook() // runs after the deferred Unlock

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.write(); err != nil {
		return err
	}

	s.notes[n.ID] = n

	return nil
}

func (s *fakeNoteStore) UpdateNoteWithVersion(_ context.Context, n domain.Note, prev domain.NoteVersion) error {
	defer s.hook() // runs after the deferred Unlock

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.write(); err != nil {
		return err
	}

	s.notes[n.ID] = n
	s.versions = append(s.versions, prev)

	return nil
}

func (s *fakeNoteStore) UpdateNoteName(_ context.Context, id uuid.UUID, name string, updated time.Time) error {
	defer s.hook() // runs after the deferred Unlock

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.write(); err != nil {
		return err
	}

	n := s.notes[id]
	n.Name, n.Updated = name, updated
	s.notes[id] = n

	return nil
}

func (s *fakeNoteStore) DeleteNote(_ context.Context, id uuid.UUID) error {
	defer s.hook() // runs after the deferred Unlock

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.write(); err != nil {
		return err
	}

	delete(s.notes, id)
	s.versions = slices.DeleteFunc(s.versions, func(v domain.NoteVersion) bool { return v.NoteID == id })

	return nil
}

func (s *fakeNoteStore) ListVersions(_ context.Context, noteID uuid.UUID) ([]domain.NoteVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []domain.NoteVersion

	for _, v := range s.versions {
		if v.NoteID == noteID {
			out = append(out, v)
		}
	}

	return out, nil
}

// fakeVaultStore records the passwords it receives.
type fakeVaultStore struct {
	exists          bool
	created         []string
	unlocked        []string
	changedPassword []string
}

func (s *fakeVaultStore) Exists(context.Context) (bool, error) { return s.exists, nil }

func (s *fakeVaultStore) Create(_ context.Context, password string) error {
	s.created = append(s.created, password)
	return nil
}

func (s *fakeVaultStore) Unlock(_ context.Context, password string) error {
	s.unlocked = append(s.unlocked, password)
	return nil
}

func (s *fakeVaultStore) ChangePassword(_ context.Context, newPassword string) error {
	s.changedPassword = append(s.changedPassword, newPassword)
	return nil
}

// fixedClock returns t on every call.
func fixedClock(t time.Time) Clock { return func() time.Time { return t } }

// seqIDs returns UUIDs 00000000-0000-0000-0000-000000000001, …02 and so on.
func seqIDs() IDGenerator {
	var (
		mu sync.Mutex
		n  byte
	)

	return func() uuid.UUID {
		mu.Lock()
		defer mu.Unlock()

		n++

		var id uuid.UUID

		id[15] = n

		return id
	}
}

// testID returns a UUID ending in b, outside the range seqIDs reaches in a test.
func testID(b byte) uuid.UUID {
	var id uuid.UUID

	id[0], id[15] = 0xff, b

	return id
}

var (
	t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	t1 = t0.Add(time.Hour)
)

// fakeSettings returns fixed connection settings.
type fakeSettings struct {
	mu    sync.Mutex
	s     domain.ConnectionSettings
	saved []domain.ConnectionSettings
}

func (f *fakeSettings) LoadSettings(context.Context) (domain.ConnectionSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.s, nil
}

func (f *fakeSettings) SaveSettings(_ context.Context, s domain.ConnectionSettings) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.s = s
	f.saved = append(f.saved, s)

	return nil
}

// syncStateCall is one SetSyncState call.
type syncStateCall struct {
	ServerID uuid.UUID
	Revision int64
}

// fakeSyncState is the local side of synchronization. When notes is set,
// snapshots read it and ApplyRestore writes the plan into it, as one local
// database would. onSnapshot runs inside LocalSnapshot and onApply inside
// ApplyRestore, as a concurrent edit would; snapErr fails LocalSnapshot and
// setCtxErr records the context error SetSyncState saw.
type fakeSyncState struct {
	mu         sync.Mutex
	snap       LocalSnapshot
	notes      *fakeNoteStore
	setCalls   []syncStateCall
	restores   []RestorePlan
	onSnapshot func()
	onApply    func()
	snapErr    error
	setCtxErr  []error
}

func (f *fakeSyncState) LocalSnapshot(ctx context.Context) (LocalSnapshot, error) {
	f.mu.Lock()
	hook, notes, snapErr := f.onSnapshot, f.notes, f.snapErr
	snap := f.snap
	snap.Notes = slices.Clone(snap.Notes)
	snap.Synced = maps.Clone(snap.Synced)
	f.mu.Unlock()

	if hook != nil {
		hook()
	}

	if snapErr != nil {
		return LocalSnapshot{}, snapErr
	}

	if notes != nil {
		list, err := notes.ListNotes(ctx)
		if err != nil {
			return LocalSnapshot{}, err
		}

		snap.Notes = list
	}

	return snap, nil
}

func (f *fakeSyncState) ApplyRestore(_ context.Context, p RestorePlan, revision int64, serverID uuid.UUID) error {
	f.mu.Lock()
	hook, notes := f.onApply, f.notes
	f.mu.Unlock()

	if hook != nil {
		hook()
	}

	if notes != nil {
		notes.mu.Lock()
		for _, n := range p.Insert {
			notes.notes[n.ID] = n
		}

		for _, c := range p.Text {
			notes.notes[c.Note.ID] = c.Note
			notes.versions = append(notes.versions, c.Previous)
		}

		for _, n := range p.Rename {
			notes.notes[n.ID] = n
		}

		for _, id := range p.Delete {
			delete(notes.notes, id)
		}
		notes.mu.Unlock()
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.restores = append(f.restores, p)
	f.synced(serverID, revision)

	return nil
}

func (f *fakeSyncState) SetSyncState(ctx context.Context, serverID uuid.UUID, revision int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.setCtxErr = append(f.setCtxErr, ctx.Err())
	if err := ctx.Err(); err != nil {
		return err
	}

	f.setCalls = append(f.setCalls, syncStateCall{serverID, revision})
	f.synced(serverID, revision)

	return nil
}

// synced stores the pair as the current one and in the history, as the
// local database does; f.mu is held.
func (f *fakeSyncState) synced(serverID uuid.UUID, revision int64) {
	f.snap.Revision, f.snap.ServerID = revision, serverID
	if f.snap.Synced == nil {
		f.snap.Synced = map[uuid.UUID]int64{}
	}

	f.snap.Synced[serverID] = revision
}

func (f *fakeSyncState) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.setCalls) + len(f.restores)
}

// backupCall is one RemoteSession.Backup call.
type backupCall struct {
	Notes            []domain.Note
	ExpectedServerID uuid.UUID
	ExpectedRevision int64
}

// fakeServer is the remote side shared by all sessions. The hooks change how
// the next calls behave.
type fakeServer struct {
	mu       sync.Mutex
	notes    []domain.Note
	revision int64
	serverID uuid.UUID
	noSchema bool // meta is missing: OpenCreate creates it, OpenCheck reports Empty

	openErr     error
	snapshotErr error
	// snapshotGate, when set, blocks Snapshot until it is closed; snapshotEntered is closed on entry.
	snapshotGate    chan struct{}
	snapshotEntered chan struct{}
	backupErr       error  // returned by Backup before any change
	backupAckLost   bool   // Backup applies the change, then returns an error
	backupBlocks    bool   // Backup waits for the context to end
	afterBackup     func() // runs after a successful Backup, before it returns
	closeErr        error  // returned by Close

	opens    []OpenMode
	settings []domain.ConnectionSettings
	targets  []RemoteTarget
	backups  []backupCall
	closed   int
}

var errAckLost = errors.New("connection lost after COMMIT")

func (f *fakeServer) Open(_ context.Context, t RemoteTarget, mode OpenMode) (RemoteSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.opens = append(f.opens, mode)

	f.targets = append(f.targets, t)
	f.settings = append(f.settings, t.Settings)

	if f.openErr != nil {
		return nil, f.openErr
	}

	sess := &fakeSession{srv: f}
	if f.noSchema {
		switch mode {
		case OpenCreate:
			f.noSchema = false
			sess.created = true
		case OpenCheck:
			sess.empty = true
		}
	}

	return sess, nil
}

type fakeSession struct {
	srv            *fakeServer
	created, empty bool
}

func (s *fakeSession) Created() bool { return s.created }
func (s *fakeSession) Empty() bool   { return s.empty }

func (s *fakeSession) Snapshot(ctx context.Context) (RemoteSnapshot, error) {
	s.srv.mu.Lock()
	gate, entered := s.srv.snapshotGate, s.srv.snapshotEntered
	s.srv.mu.Unlock()

	if entered != nil {
		close(entered)
	}

	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return RemoteSnapshot{}, ctx.Err()
		}
	}

	s.srv.mu.Lock()
	defer s.srv.mu.Unlock()

	if s.srv.snapshotErr != nil {
		return RemoteSnapshot{}, s.srv.snapshotErr
	}

	return RemoteSnapshot{Notes: slices.Clone(s.srv.notes), Revision: s.srv.revision, ServerID: s.srv.serverID}, nil
}

func (s *fakeSession) Backup(ctx context.Context, notes []domain.Note, expectedServerID uuid.UUID, expectedRevision int64) (int64, error) {
	s.srv.mu.Lock()
	s.srv.backups = append(s.srv.backups, backupCall{slices.Clone(notes), expectedServerID, expectedRevision})
	blocks := s.srv.backupBlocks
	s.srv.mu.Unlock()

	if blocks {
		<-ctx.Done()
		return 0, ctx.Err()
	}

	s.srv.mu.Lock()
	defer s.srv.mu.Unlock()

	if s.srv.backupErr != nil {
		return 0, s.srv.backupErr
	}

	if expectedServerID != s.srv.serverID || expectedRevision != s.srv.revision {
		return 0, domain.ErrRemoteChanged
	}

	s.srv.notes = slices.Clone(notes)

	s.srv.revision++
	if s.srv.backupAckLost {
		return 0, errAckLost
	}

	if h := s.srv.afterBackup; h != nil {
		h()
	}

	return s.srv.revision, nil
}

func (s *fakeSession) Close() error {
	s.srv.mu.Lock()
	defer s.srv.mu.Unlock()

	s.srv.closed++

	return s.srv.closeErr
}

// fakeKeyStore keeps one key pair.
type fakeKeyStore struct {
	key   domain.KeyPair
	has   bool
	saves int
}

func (f *fakeKeyStore) LoadKey(context.Context) (domain.KeyPair, bool, error) {
	return f.key, f.has, nil
}

func (f *fakeKeyStore) SaveKey(_ context.Context, k domain.KeyPair) error {
	f.key, f.has = k, true
	f.saves++

	return nil
}

// fakeKeyTool returns key or err from both methods and records Import input.
type fakeKeyTool struct {
	key        domain.KeyPair
	err        error
	imported   []byte
	passphrase string
}

func (f *fakeKeyTool) Generate() (domain.KeyPair, error) { return f.key, f.err }

func (f *fakeKeyTool) Import(data []byte, passphrase string) (domain.KeyPair, error) {
	f.imported, f.passphrase = data, passphrase
	return f.key, f.err
}

// nopIndex is a NoteIndex that ignores every call.
type nopIndex struct{}

func (nopIndex) NoteSaved(domain.Note) {}
func (nopIndex) NoteDeleted(uuid.UUID) {}

// recordingIndex records the NoteIndex calls and counts those made while the
// edit lock of changes was free.
type recordingIndex struct {
	changes *ChangeTracker

	mu       sync.Mutex
	saved    []domain.Note
	deleted  []uuid.UUID
	unlocked int
}

// checkLock counts a call made outside the edit lock.
func (r *recordingIndex) checkLock() {
	if r.changes.edit.TryLock() {
		r.changes.edit.Unlock()
		r.unlocked++
	}
}

func (r *recordingIndex) NoteSaved(n domain.Note) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.checkLock()
	r.saved = append(r.saved, n)
}

func (r *recordingIndex) NoteDeleted(id uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.checkLock()
	r.deleted = append(r.deleted, id)
}

// broadcast wakes every waiter on each notify. The zero value is ready.
type broadcast struct {
	mu sync.Mutex
	ch chan struct{}
}

// wait returns a channel that is closed by the next notify.
func (b *broadcast) wait() <-chan struct{} {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ch == nil {
		b.ch = make(chan struct{})
	}

	return b.ch
}

func (b *broadcast) notify() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ch != nil {
		close(b.ch)
		b.ch = nil
	}
}

// hangGuard bounds every wait of a test that would otherwise hang; it never
// paces a passing test.
const hangGuard = time.Second

// waitUntil returns once cond holds, rechecking it after every notify of b.
func waitUntil(t *testing.T, b *broadcast, what string, cond func() bool) {
	t.Helper()

	deadline := time.After(hangGuard)

	for {
		woken := b.wait() // before cond, so no notify between them is lost

		if cond() {
			return
		}

		select {
		case <-woken:
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// engineCall is one index command a fakeEngine received.
type engineCall struct {
	Kind     string // reset, upsert or remove
	Notes    []IndexedNote
	Removals []IndexedRemoval
}

// fakeEngine applies the index commands to a map in the order they come, with
// no Rev check of its own, so the order is all that keeps it right. hang makes
// every command wait for its context; gate, when set, holds the next Upsert
// until it is closed, after closing entered.
type fakeEngine struct {
	mu       sync.Mutex
	index    map[uuid.UUID]IndexedNote
	calls    []engineCall
	starts   int
	hang     bool
	gate     chan struct{}
	entered  chan struct{}
	hits     []SearchHit
	queries  []string
	startErr error

	events  chan SearchEvent
	changed broadcast // notified after every command
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{index: map[uuid.UUID]IndexedNote{}, events: make(chan SearchEvent)}
}

func (f *fakeEngine) Start(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.starts++

	return f.startErr
}

// hold waits as hang and gate tell the command kind to.
func (f *fakeEngine) hold(ctx context.Context, kind string) error {
	f.mu.Lock()
	hang := f.hang

	var gate, entered chan struct{}
	if kind == "upsert" {
		gate, entered = f.gate, f.entered
		f.gate, f.entered = nil, nil
	}
	f.mu.Unlock()

	if gate == nil && !hang {
		return nil
	}

	if entered != nil {
		close(entered)
	}

	select {
	case <-gate: // a nil gate with hang set waits for ctx only
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeEngine) record(c engineCall, apply func()) {
	f.mu.Lock()
	f.calls = append(f.calls, c)

	apply()
	f.mu.Unlock()

	f.changed.notify()
}

func (f *fakeEngine) Reset(ctx context.Context) error {
	if err := f.hold(ctx, "reset"); err != nil {
		return err
	}

	f.record(engineCall{Kind: "reset"}, func() { clear(f.index) })

	return nil
}

func (f *fakeEngine) Upsert(ctx context.Context, notes []IndexedNote) error {
	if err := f.hold(ctx, "upsert"); err != nil {
		return err
	}

	f.record(engineCall{Kind: "upsert", Notes: slices.Clone(notes)}, func() {
		for _, n := range notes {
			f.index[n.ID] = n
		}
	})

	return nil
}

func (f *fakeEngine) Remove(ctx context.Context, ids []IndexedRemoval) error {
	if err := f.hold(ctx, "remove"); err != nil {
		return err
	}

	f.record(engineCall{Kind: "remove", Removals: slices.Clone(ids)}, func() {
		for _, r := range ids {
			delete(f.index, r.ID)
		}
	})

	return nil
}

func (f *fakeEngine) Search(_ context.Context, query string, _ int) ([]SearchHit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.queries = append(f.queries, query)

	return slices.Clone(f.hits), nil
}

func (f *fakeEngine) Events() <-chan SearchEvent { return f.events }

func (f *fakeEngine) Close() error { return nil }

// snapshot returns a copy of the index and of the calls so far.
func (f *fakeEngine) snapshot() (map[uuid.UUID]IndexedNote, []engineCall) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return maps.Clone(f.index), slices.Clone(f.calls)
}

// embeddingPut is one PutEmbedding call.
type embeddingPut struct {
	ID   uuid.UUID
	Data []byte
}

// fakeEmbeddingStore keeps embedding records in a map and records every
// PutEmbedding call.
type fakeEmbeddingStore struct {
	mu         sync.Mutex
	records    map[uuid.UUID][]byte
	unreadable []uuid.UUID
	listErr    error
	lists      int
	puts       []embeddingPut
	deleted    [][]uuid.UUID

	changed broadcast // notified after every call
}

func (f *fakeEmbeddingStore) ListEmbeddings(context.Context) (map[uuid.UUID][]byte, []uuid.UUID, error) {
	defer f.changed.notify()

	f.mu.Lock()
	defer f.mu.Unlock()

	f.lists++
	if f.listErr != nil {
		return nil, nil, f.listErr
	}

	return maps.Clone(f.records), slices.Clone(f.unreadable), nil
}

func (f *fakeEmbeddingStore) PutEmbedding(_ context.Context, id uuid.UUID, data []byte) error {
	defer f.changed.notify()

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.records == nil {
		f.records = map[uuid.UUID][]byte{}
	}

	f.records[id] = data
	f.puts = append(f.puts, embeddingPut{id, data})

	return nil
}

func (f *fakeEmbeddingStore) DeleteEmbeddings(_ context.Context, ids []uuid.UUID) error {
	defer f.changed.notify()

	f.mu.Lock()
	defer f.mu.Unlock()

	for _, id := range ids {
		delete(f.records, id)
	}

	f.unreadable = slices.DeleteFunc(f.unreadable, func(id uuid.UUID) bool { return slices.Contains(ids, id) })
	f.deleted = append(f.deleted, slices.Clone(ids))

	return nil
}

func (f *fakeEmbeddingStore) putCalls() []embeddingPut {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.puts)
}

// saveConnectionCall is one ConnectionStore.SaveConnection call.
type saveConnectionCall struct {
	Settings domain.ConnectionSettings
	Change   TokenChange
	Token    CloudToken
}

// fakeConnectionStore saves the settings into settings and applies the token
// change to tokens, as one transaction of local.db would.
type fakeConnectionStore struct {
	settings *fakeSettings
	tokens   *fakeTokenStore

	mu    sync.Mutex
	calls []saveConnectionCall
}

func (f *fakeConnectionStore) SaveConnection(ctx context.Context, s domain.ConnectionSettings, change TokenChange, t CloudToken) error {
	f.mu.Lock()
	f.calls = append(f.calls, saveConnectionCall{Settings: s, Change: change, Token: t})
	f.mu.Unlock()

	f.tokens.mu.Lock()
	switch change {
	case TokenKeep:
	case TokenSet:
		f.tokens.tok, f.tokens.has = t, true
	case TokenDelete:
		f.tokens.tok, f.tokens.has = CloudToken{}, false
	}
	f.tokens.mu.Unlock()

	return f.settings.SaveSettings(ctx, s)
}

func (f *fakeConnectionStore) saveCalls() []saveConnectionCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.calls)
}

// loginResult is what a fakeLogin's Wait or Exchange returns.
type loginResult struct {
	tok CloudToken
	err error
}

// fakeLogin is one sign-in session. Wait and Exchange signal entered, then
// block until the test sends a result, the context ends or the login is closed.
type fakeLogin struct {
	url     string
	result  chan loginResult
	entered chan struct{}
	closed  chan struct{}
	close   sync.Once

	mu    sync.Mutex
	waits int
	codes []string
}

func newFakeLogin(url string) *fakeLogin {
	return &fakeLogin{
		url: url, result: make(chan loginResult), entered: make(chan struct{}, 8), closed: make(chan struct{}),
	}
}

func (l *fakeLogin) URL() string { return l.url }

func (l *fakeLogin) Wait(ctx context.Context) (CloudToken, error) {
	l.mu.Lock()
	l.waits++
	l.mu.Unlock()

	return l.finish(ctx)
}

func (l *fakeLogin) Exchange(ctx context.Context, code string) (CloudToken, error) {
	l.mu.Lock()
	l.codes = append(l.codes, code)
	l.mu.Unlock()

	return l.finish(ctx)
}

func (l *fakeLogin) finish(ctx context.Context) (CloudToken, error) {
	l.entered <- struct{}{}

	select {
	case r := <-l.result:
		return r.tok, r.err
	case <-ctx.Done():
		return CloudToken{}, ctx.Err()
	case <-l.closed:
		return CloudToken{}, errors.New("login closed")
	}
}

func (l *fakeLogin) Close() error {
	l.close.Do(func() { close(l.closed) })

	return nil
}

func (l *fakeLogin) isClosed() bool {
	select {
	case <-l.closed:
		return true
	default:
		return false
	}
}

func (l *fakeLogin) waitCalls() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.waits
}

func (l *fakeLogin) submitted() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.codes)
}

// beginCall is one CloudAuthorizer.Begin* call.
type beginCall struct {
	clientID string
	manual   bool
	// prevClosed reports whether every earlier login was closed at the call.
	prevClosed bool
}

// fakeAuthorizer hands out fakeLogins and sends each one on begun, so the
// test can finish it. beginErr, when set, is returned instead.
type fakeAuthorizer struct {
	begun chan *fakeLogin

	mu       sync.Mutex
	beginErr error
	logins   []*fakeLogin
	calls    []beginCall
}

func newFakeAuthorizer() *fakeAuthorizer {
	return &fakeAuthorizer{begun: make(chan *fakeLogin, 8)}
}

func (a *fakeAuthorizer) BeginLoopback(_ context.Context, clientID string) (CloudLogin, error) {
	return a.begin(clientID, false)
}

func (a *fakeAuthorizer) BeginManual(_ context.Context, clientID string) (CloudLogin, error) {
	return a.begin(clientID, true)
}

func (a *fakeAuthorizer) begin(clientID string, manual bool) (CloudLogin, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	prevClosed := true

	for _, l := range a.logins {
		if !l.isClosed() {
			prevClosed = false
		}
	}

	a.calls = append(a.calls, beginCall{clientID: clientID, manual: manual, prevClosed: prevClosed})
	if a.beginErr != nil {
		return nil, a.beginErr
	}

	l := newFakeLogin(fmt.Sprintf("https://oauth.example/authorize?n=%d", len(a.logins)))
	a.logins = append(a.logins, l)

	a.begun <- l

	return l, nil
}

func (a *fakeAuthorizer) beginCalls() []beginCall {
	a.mu.Lock()
	defer a.mu.Unlock()

	return slices.Clone(a.calls)
}

// recv returns the next value from ch or fails the test after a timeout.
func recv[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)

		var zero T

		return zero
	}
}

// send delivers v on ch or fails the test after a timeout.
func send[T any](t *testing.T, ch chan<- T, v T, what string) {
	t.Helper()

	select {
	case ch <- v:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out sending %s", what)
	}
}
