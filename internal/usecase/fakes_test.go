package usecase

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
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
	f.snap.Revision, f.snap.ServerID = revision, serverID

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
	f.snap.Revision, f.snap.ServerID = revision, serverID

	return nil
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
	backups  []backupCall
	closed   int
}

var errAckLost = errors.New("connection lost after COMMIT")

func (f *fakeServer) Open(_ context.Context, s domain.ConnectionSettings, mode OpenMode) (RemoteSession, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.opens = append(f.opens, mode)

	f.settings = append(f.settings, s)
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
