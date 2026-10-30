package usecase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

var (
	serverA = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	serverB = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
)

func completeSettings() domain.ConnectionSettings {
	return domain.ConnectionSettings{
		SSHHost: "example.org", SSHPort: 22, SSHUser: "u", DBHost: "127.0.0.1", DBPort: 5432,
		DBName: "smartnote", DBUser: "smartnote", SyncPhrase: strings.Repeat("p", domain.MinSyncPhraseLen),
	}
}

// syncFixture is a Sync over fakes, with every published State recorded.
type syncFixture struct {
	sync    *Sync
	local   *fakeSyncState
	srv     *fakeServer
	changes *ChangeTracker
	gate    *Gate

	mu     sync.Mutex
	states []State
}

func newSyncFixture(t *testing.T, local LocalSnapshot, srv *fakeServer) *syncFixture {
	t.Helper()
	f := &syncFixture{
		local:   &fakeSyncState{snap: local},
		srv:     srv,
		changes: &ChangeTracker{},
		gate:    &Gate{},
	}
	s, err := NewSync(SyncDeps{
		Local:    f.local,
		Settings: &fakeSettings{s: completeSettings()},
		Remote:   srv,
		Changes:  f.changes,
		IDs:      seqIDs(),
		Gate:     f.gate,
		OnState: func(st State) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.states = append(f.states, st)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.sync = s
	return f
}

func (f *syncFixture) lastState() State {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.states) == 0 {
		return State{}
	}
	return f.states[len(f.states)-1]
}

func TestNewSyncNil(t *testing.T) {
	full := SyncDeps{Local: &fakeSyncState{}, Settings: &fakeSettings{}, Remote: &fakeServer{},
		Changes: &ChangeTracker{}, IDs: seqIDs(), Gate: &Gate{}}
	tests := []struct {
		name   string
		modify func(*SyncDeps)
	}{
		{name: "local", modify: func(d *SyncDeps) { d.Local = nil }},
		{name: "settings", modify: func(d *SyncDeps) { d.Settings = nil }},
		{name: "remote", modify: func(d *SyncDeps) { d.Remote = nil }},
		{name: "changes", modify: func(d *SyncDeps) { d.Changes = nil }},
		{name: "ids", modify: func(d *SyncDeps) { d.IDs = nil }},
		{name: "gate", modify: func(d *SyncDeps) { d.Gate = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := full
			tt.modify(&d)
			if _, err := NewSync(d); err == nil {
				t.Error("want an error for a nil dependency")
			}
		})
	}
	if _, err := NewSync(full); err != nil {
		t.Errorf("NewSync without OnState: %v", err)
	}
}

func TestConnectOffline(t *testing.T) {
	srv := &fakeServer{}
	f := newSyncFixture(t, LocalSnapshot{}, srv)
	empty := &fakeSettings{}
	f.sync.d.Settings = empty

	out, err := f.sync.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if out.Status != StatusOffline {
		t.Errorf("Status = %q, want offline", out.Status)
	}
	if len(srv.opens) != 0 {
		t.Errorf("Open called %d times", len(srv.opens))
	}
	if f.sync.State().Connected {
		t.Error("Connected = true while offline")
	}
}

func TestConnectCreatedBacksUp(t *testing.T) {
	srv := &fakeServer{noSchema: true, serverID: serverA}
	local := LocalSnapshot{Notes: []domain.Note{note(1, "a", "x")}}
	f := newSyncFixture(t, local, srv)

	out, err := f.sync.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if out.Status != StatusCreated {
		t.Errorf("Status = %q, want created", out.Status)
	}
	if len(srv.opens) != 1 || srv.opens[0] != OpenCreate {
		t.Errorf("opens = %v, want [OpenCreate]", srv.opens)
	}
	if len(srv.backups) != 1 {
		t.Fatalf("backups = %d, want 1", len(srv.backups))
	}
	if b := srv.backups[0]; b.ExpectedRevision != 0 || b.ExpectedServerID != serverA || len(b.Notes) != 1 {
		t.Errorf("Backup(%+v), want expected revision 0 from the snapshot, server A, 1 note", b)
	}
	if got := f.local.setCalls; len(got) != 1 || got[0] != (syncStateCall{serverA, 1}) {
		t.Errorf("SetSyncState calls = %v, want [{A 1}]", got)
	}
	if st := f.sync.State(); !st.Connected || st.Dirty || st.RestoreAvailable {
		t.Errorf("State = %+v, want connected, clean", st)
	}
	if srv.closed != 1 {
		t.Errorf("sessions closed = %d, want 1", srv.closed)
	}
}

func TestConnectEmptyBothInSync(t *testing.T) {
	srv := &fakeServer{serverID: serverA}
	f := newSyncFixture(t, LocalSnapshot{}, srv)

	out, err := f.sync.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if out.Status != StatusInSync {
		t.Errorf("Status = %q, want in_sync", out.Status)
	}
	if st := f.sync.State(); st != (State{Connected: true}) {
		t.Errorf("State = %+v, want {Connected: true}", st)
	}
	if len(srv.backups) != 0 || len(f.local.restores) != 0 {
		t.Error("an empty pair caused a Backup or a Restore")
	}
	if _, err := f.sync.PrepareBackup(context.Background()); err != nil {
		t.Fatalf("PrepareBackup: %v", err)
	}
}

func TestConnectRemoteNewer(t *testing.T) {
	srv := &fakeServer{serverID: serverA, revision: 5, notes: []domain.Note{note(1, "a", "server")}}
	local := LocalSnapshot{Notes: []domain.Note{note(1, "a", "local"), note(2, "b", "x"), note(3, "c", "y")}, Revision: 4, ServerID: serverA}
	f := newSyncFixture(t, local, srv)

	out, err := f.sync.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if out.Status != StatusRemoteNewer {
		t.Errorf("Status = %q, want remote_newer", out.Status)
	}
	if out.Restore != (Counts{Updated: 1, Deleted: 2}) {
		t.Errorf("Restore = %+v, want {Updated: 1, Deleted: 2}", out.Restore)
	}
	if st := f.sync.State(); !st.Connected || !st.Dirty || !st.RestoreAvailable {
		t.Errorf("State = %+v, want connected, dirty, restore available", st)
	}

	if err := f.sync.CommitRestore(context.Background()); err != nil {
		t.Fatalf("CommitRestore with the plan from Connect: %v", err)
	}
	if len(f.local.restores) != 1 || len(f.local.restores[0].Delete) != 2 {
		t.Errorf("restores = %+v, want one plan deleting 2 notes", f.local.restores)
	}
	if len(srv.opens) != 1 {
		t.Errorf("opens = %d, want 1: Restore from Connect reuses its snapshot", len(srv.opens))
	}
	if st := f.sync.State(); st.Dirty || st.RestoreAvailable {
		t.Errorf("State after Restore = %+v, want clean", st)
	}
	if f.local.snap.Revision != 5 {
		t.Errorf("local revision = %d, want 5", f.local.snap.Revision)
	}
}

func TestConnectNotNewerDiverged(t *testing.T) {
	srv := &fakeServer{serverID: serverA, revision: 4, notes: []domain.Note{note(1, "a", "server")}}
	local := LocalSnapshot{Notes: []domain.Note{note(1, "a", "local")}, Revision: 4, ServerID: serverA}
	f := newSyncFixture(t, local, srv)

	out, err := f.sync.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if out.Status != StatusDiverged {
		t.Errorf("Status = %q, want diverged", out.Status)
	}
	if st := f.sync.State(); !st.Dirty || !st.RestoreAvailable || !st.Connected {
		t.Errorf("State = %+v, want connected, dirty, restore available", st)
	}
	if err := f.sync.CommitRestore(context.Background()); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("CommitRestore after diverged = %v, want ErrNoPendingOperation", err)
	}
	if f.local.writes() != 0 {
		t.Errorf("local writes = %d, want 0", f.local.writes())
	}
}

func TestConnectServerIDChanged(t *testing.T) {
	srv := &fakeServer{serverID: serverB, revision: 1, notes: []domain.Note{note(1, "a", "server")}}
	local := LocalSnapshot{Notes: []domain.Note{note(1, "a", "local")}, Revision: 7, ServerID: serverA}
	f := newSyncFixture(t, local, srv)

	out, err := f.sync.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if got := f.local.setCalls; len(got) != 1 || got[0] != (syncStateCall{serverB, 0}) {
		t.Errorf("SetSyncState calls = %v, want [{B 0}]", got)
	}
	if out.Status != StatusRemoteNewer {
		t.Errorf("Status = %q, want remote_newer", out.Status)
	}
}

func TestConnectInSyncAlignsRevision(t *testing.T) {
	notes := []domain.Note{note(1, "a", "x")}
	srv := &fakeServer{serverID: serverA, revision: 5, notes: notes}
	f := newSyncFixture(t, LocalSnapshot{Notes: notes, Revision: 3, ServerID: serverA}, srv)

	out, err := f.sync.Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if out.Status != StatusInSync {
		t.Errorf("Status = %q, want in_sync", out.Status)
	}
	if got := f.local.setCalls; len(got) != 1 || got[0] != (syncStateCall{serverA, 5}) {
		t.Errorf("SetSyncState calls = %v, want [{A 5}]", got)
	}
}

func TestConnectInSyncClearsDirty(t *testing.T) {
	notes := []domain.Note{note(1, "a", "x")}
	srv := &fakeServer{serverID: serverA, revision: 2, notes: notes}
	f := newSyncFixture(t, LocalSnapshot{Notes: notes, Revision: 2, ServerID: serverA}, srv)
	f.changes.MarkChanged()

	if _, err := f.sync.Connect(context.Background()); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if f.sync.State().Dirty {
		t.Error("Dirty = true although local data equals the server's")
	}
	if len(f.local.setCalls) != 0 {
		t.Errorf("SetSyncState called %v with an equal revision", f.local.setCalls)
	}
}

// diverged returns a fixture where the local note differs from the server's.
func diverged(t *testing.T) *syncFixture {
	t.Helper()
	srv := &fakeServer{serverID: serverA, revision: 3, notes: []domain.Note{note(1, "a", "server"), note(2, "b", "gone")}}
	return newSyncFixture(t, LocalSnapshot{Notes: []domain.Note{note(1, "a", "local")}, Revision: 3, ServerID: serverA}, srv)
}

func TestBackup(t *testing.T) {
	f := diverged(t)
	ctx := context.Background()
	f.changes.MarkChanged()

	counts, err := f.sync.PrepareBackup(ctx)
	if err != nil {
		t.Fatalf("PrepareBackup: %v", err)
	}
	if counts != (Counts{Updated: 1, Deleted: 1}) {
		t.Errorf("Counts = %+v, want {Updated: 1, Deleted: 1}", counts)
	}
	if err := f.sync.CommitBackup(ctx); err != nil {
		t.Fatalf("CommitBackup: %v", err)
	}
	if got := f.local.setCalls; len(got) != 1 || got[0] != (syncStateCall{serverA, 4}) {
		t.Errorf("SetSyncState calls = %v, want [{A 4}]", got)
	}
	if st := f.sync.State(); st.Dirty || st.RestoreAvailable || !st.Connected {
		t.Errorf("State = %+v, want connected and clean", st)
	}
	if err := f.sync.CommitBackup(ctx); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("second CommitBackup = %v, want ErrNoPendingOperation", err)
	}
	if want := 2; f.srv.closed != want {
		t.Errorf("sessions closed = %d, want %d", f.srv.closed, want)
	}
}

func TestCommitBackupRemoteChanged(t *testing.T) {
	f := diverged(t)
	ctx := context.Background()
	if _, err := f.sync.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sync.PrepareBackup(ctx); err != nil {
		t.Fatal(err)
	}
	f.srv.mu.Lock()
	f.srv.revision++ // another device backed up in between
	f.srv.mu.Unlock()

	if err := f.sync.CommitBackup(ctx); !errors.Is(err, domain.ErrRemoteChanged) {
		t.Fatalf("CommitBackup = %v, want ErrRemoteChanged", err)
	}
	if f.local.writes() != 0 {
		t.Errorf("local writes = %d, want 0", f.local.writes())
	}
	if st := f.sync.State(); !st.Dirty || !st.Connected {
		t.Errorf("State = %+v, want dirty and still connected", st)
	}
}

func TestCommitBackupPassesServerID(t *testing.T) {
	f := diverged(t)
	ctx := context.Background()
	if _, err := f.sync.PrepareBackup(ctx); err != nil {
		t.Fatal(err)
	}
	f.srv.mu.Lock()
	f.srv.serverID = serverB // the server was replaced, the revision happens to match
	f.srv.mu.Unlock()

	err := f.sync.CommitBackup(ctx)
	if got := f.srv.backups[len(f.srv.backups)-1].ExpectedServerID; got != serverA {
		t.Errorf("Backup expected server %v, want the planned %v", got, serverA)
	}
	if !errors.Is(err, domain.ErrRemoteChanged) {
		t.Fatalf("CommitBackup = %v, want ErrRemoteChanged", err)
	}
	if f.local.writes() != 0 {
		t.Errorf("local writes = %d, want 0", f.local.writes())
	}
}

func TestCommitBackupCanceled(t *testing.T) {
	f := diverged(t)
	f.changes.MarkChanged()
	if _, err := f.sync.PrepareBackup(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.srv.backupBlocks = true
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(10*time.Millisecond, cancel)

	if err := f.sync.CommitBackup(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("CommitBackup = %v, want context.Canceled", err)
	}
	if len(f.local.setCalls) != 0 {
		t.Errorf("SetSyncState called: %v", f.local.setCalls)
	}
	if !f.sync.State().Dirty {
		t.Error("Dirty = false after a canceled Backup")
	}
}

func TestCommitBackupAckLost(t *testing.T) {
	f := diverged(t)
	ctx := context.Background()
	f.changes.MarkChanged()
	if _, err := f.sync.PrepareBackup(ctx); err != nil {
		t.Fatal(err)
	}
	f.srv.backupAckLost = true

	if err := f.sync.CommitBackup(ctx); !errors.Is(err, errAckLost) {
		t.Fatalf("CommitBackup = %v, want the lost acknowledgement", err)
	}
	if f.local.snap.Revision != 3 || len(f.local.setCalls) != 0 {
		t.Errorf("local revision = %d, set calls %v; want 3 and none", f.local.snap.Revision, f.local.setCalls)
	}
	if !f.sync.State().Dirty {
		t.Error("Dirty = false after an unknown outcome")
	}

	f.srv.backupAckLost = false
	counts, err := f.sync.PrepareBackup(ctx)
	if err != nil {
		t.Fatalf("PrepareBackup: %v", err)
	}
	if counts != (Counts{}) {
		t.Errorf("Counts = %+v, want zero: the Backup went through", counts)
	}
	out, err := f.sync.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if out.Status != StatusInSync || f.local.snap.Revision != 4 {
		t.Errorf("Connect = %q, local revision %d; want in_sync and 4", out.Status, f.local.snap.Revision)
	}
}

func TestCommitBackupKeepsDirtyOnConcurrentEdit(t *testing.T) {
	f := diverged(t)
	ctx := context.Background()
	f.changes.MarkChanged()
	if _, err := f.sync.PrepareBackup(ctx); err != nil {
		t.Fatal(err)
	}
	f.changes.MarkChanged()

	if err := f.sync.CommitBackup(ctx); err != nil {
		t.Fatalf("CommitBackup: %v", err)
	}
	if !f.sync.State().Dirty {
		t.Error("Dirty = false although a note changed after the snapshot")
	}
}

func TestBackupEditDuringLocalSnapshotKeepsDirty(t *testing.T) {
	f := diverged(t)
	ctx := context.Background()
	f.local.onSnapshot = f.changes.MarkChanged // the edit races the snapshot and may be missing from it

	if _, err := f.sync.PrepareBackup(ctx); err != nil {
		t.Fatal(err)
	}
	f.local.onSnapshot = nil
	if err := f.sync.CommitBackup(ctx); err != nil {
		t.Fatalf("CommitBackup: %v", err)
	}
	if !f.sync.State().Dirty {
		t.Error("Dirty = false: the generation was read after the snapshot")
	}
}

func TestRestore(t *testing.T) {
	f := diverged(t)
	ctx := context.Background()

	counts, err := f.sync.PrepareRestore(ctx)
	if err != nil {
		t.Fatalf("PrepareRestore: %v", err)
	}
	if counts != (Counts{Added: 1, Updated: 1}) {
		t.Errorf("Counts = %+v, want {Added: 1, Updated: 1}", counts)
	}
	if f.srv.opens[0] != OpenCheck {
		t.Errorf("PrepareRestore opened with %v, want OpenCheck", f.srv.opens[0])
	}
	if err := f.sync.CommitRestore(ctx); err != nil {
		t.Fatalf("CommitRestore: %v", err)
	}
	if len(f.local.restores) != 1 {
		t.Fatalf("restores = %d, want 1", len(f.local.restores))
	}
	if p := f.local.restores[0]; p.Revision != 3 || p.ServerID != serverA || len(p.Text) != 1 || len(p.Insert) != 1 {
		t.Errorf("plan = %+v", p)
	}
}

func TestPrepareRestoreMissingSchema(t *testing.T) {
	srv := &fakeServer{noSchema: true, serverID: serverA}
	f := newSyncFixture(t, LocalSnapshot{Notes: []domain.Note{note(1, "a", "x")}}, srv)

	if _, err := f.sync.PrepareRestore(context.Background()); !errors.Is(err, domain.ErrRemoteChanged) {
		t.Fatalf("PrepareRestore = %v, want ErrRemoteChanged", err)
	}
	if !srv.noSchema {
		t.Error("PrepareRestore created the schema")
	}
	if err := f.sync.CommitRestore(context.Background()); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("CommitRestore = %v, want ErrNoPendingOperation", err)
	}
}

func TestCommitRestoreLocalChanged(t *testing.T) {
	f := diverged(t)
	ctx := context.Background()
	if _, err := f.sync.PrepareRestore(ctx); err != nil {
		t.Fatal(err)
	}
	f.changes.MarkChanged()

	if err := f.sync.CommitRestore(ctx); !errors.Is(err, domain.ErrLocalChanged) {
		t.Fatalf("CommitRestore = %v, want ErrLocalChanged", err)
	}
	if len(f.local.restores) != 0 {
		t.Error("ApplyRestore called")
	}
	if !f.sync.State().Dirty {
		t.Error("Dirty = false after ErrLocalChanged")
	}
}

func TestRestoreAbortsOnSnapshotError(t *testing.T) {
	f := diverged(t)
	f.srv.snapshotErr = errors.New("decrypt note: sealbox: cannot open sealed data")

	if _, err := f.sync.PrepareRestore(context.Background()); err == nil {
		t.Fatal("PrepareRestore = nil error")
	}
	if err := f.sync.CommitRestore(context.Background()); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("CommitRestore = %v, want ErrNoPendingOperation", err)
	}
	if f.local.writes() != 0 {
		t.Errorf("local writes = %d, want 0", f.local.writes())
	}
	if f.sync.State().Connected {
		t.Error("Connected = true after a failed snapshot")
	}
}

func TestWrongPhraseKeepsConnectedFalseNoWrites(t *testing.T) {
	srv := &fakeServer{openErr: domain.ErrWrongSyncPhrase}
	f := newSyncFixture(t, LocalSnapshot{Notes: []domain.Note{note(1, "a", "x")}}, srv)
	ctx := context.Background()

	if _, err := f.sync.Connect(ctx); !errors.Is(err, domain.ErrWrongSyncPhrase) {
		t.Fatalf("Connect = %v, want ErrWrongSyncPhrase", err)
	}
	if _, err := f.sync.PrepareBackup(ctx); !errors.Is(err, domain.ErrWrongSyncPhrase) {
		t.Fatalf("PrepareBackup = %v, want ErrWrongSyncPhrase", err)
	}
	if f.local.writes() != 0 || len(srv.backups) != 0 {
		t.Errorf("writes: local %d, server %d; want none", f.local.writes(), len(srv.backups))
	}
	if f.sync.State().Connected {
		t.Error("Connected = true")
	}
}

func TestConnectedState(t *testing.T) {
	tests := []struct {
		name          string
		err           error
		wantConnected bool
	}{
		{name: "network error", err: errors.New("ssh: handshake failed"), wantConnected: false},
		{name: "timeout", err: context.DeadlineExceeded, wantConnected: false},
		{name: "wrong phrase", err: domain.ErrWrongSyncPhrase, wantConnected: true},
		{name: "validation", err: &domain.ValidationError{Field: "ssh_port", Reason: "x"}, wantConnected: true},
		{name: "remote changed", err: domain.ErrRemoteChanged, wantConnected: true},
		{name: "host key unknown", err: &domain.HostKeyUnknownError{Fingerprint: "SHA256:x"}, wantConnected: true},
		{name: "host key mismatch", err: &domain.HostKeyMismatchError{Expected: "a", Got: "b"}, wantConnected: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notes := []domain.Note{note(1, "a", "x")}
			srv := &fakeServer{serverID: serverA, notes: notes}
			f := newSyncFixture(t, LocalSnapshot{Notes: notes, ServerID: serverA}, srv)
			if _, err := f.sync.Connect(context.Background()); err != nil {
				t.Fatal(err)
			}
			srv.openErr = tt.err
			if _, err := f.sync.PrepareBackup(context.Background()); !errors.Is(err, tt.err) {
				t.Fatalf("PrepareBackup = %v, want %v", err, tt.err)
			}
			if got := f.sync.State().Connected; got != tt.wantConnected {
				t.Errorf("Connected = %v, want %v", got, tt.wantConnected)
			}
		})
	}
}

func TestPrepareWithEmptySettings(t *testing.T) {
	srv := &fakeServer{}
	f := newSyncFixture(t, LocalSnapshot{}, srv)
	f.sync.d.Settings = &fakeSettings{}

	if _, err := f.sync.PrepareBackup(context.Background()); !errors.Is(err, domain.ErrNotConnected) {
		t.Errorf("PrepareBackup = %v, want ErrNotConnected", err)
	}
	if _, err := f.sync.PrepareRestore(context.Background()); !errors.Is(err, domain.ErrNotConnected) {
		t.Errorf("PrepareRestore = %v, want ErrNotConnected", err)
	}
	if len(srv.opens) != 0 {
		t.Errorf("Open called %d times", len(srv.opens))
	}
}

func TestBusy(t *testing.T) {
	f := diverged(t)
	f.srv.snapshotGate = make(chan struct{})
	f.srv.snapshotEntered = make(chan struct{})

	done := make(chan error, 1)
	go func() {
		_, err := f.sync.Connect(context.Background())
		done <- err
	}()
	<-f.srv.snapshotEntered

	if st := f.sync.State(); !st.Busy {
		t.Errorf("State during Connect = %+v, want Busy", st)
	}
	if _, err := f.sync.PrepareBackup(context.Background()); !errors.Is(err, domain.ErrBusy) {
		t.Errorf("PrepareBackup during Connect = %v, want ErrBusy", err)
	}
	if err := f.sync.CommitRestore(context.Background()); !errors.Is(err, domain.ErrBusy) {
		t.Errorf("CommitRestore during Connect = %v, want ErrBusy", err)
	}
	close(f.srv.snapshotGate)
	if err := <-done; err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if f.sync.State().Busy {
		t.Error("Busy after Connect returned")
	}
}

func TestCommitWithoutPlan(t *testing.T) {
	f := diverged(t)
	ctx := context.Background()
	if err := f.sync.CommitBackup(ctx); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("CommitBackup = %v, want ErrNoPendingOperation", err)
	}
	if err := f.sync.CommitRestore(ctx); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("CommitRestore = %v, want ErrNoPendingOperation", err)
	}

	if _, err := f.sync.PrepareBackup(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.sync.CommitRestore(ctx); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("CommitRestore with a Backup plan = %v, want ErrNoPendingOperation", err)
	}
	f.sync.CancelPending()
	if err := f.sync.CommitBackup(ctx); !errors.Is(err, domain.ErrNoPendingOperation) {
		t.Errorf("CommitBackup after CancelPending = %v, want ErrNoPendingOperation", err)
	}
	if len(f.srv.backups) != 0 {
		t.Errorf("Backup called %d times", len(f.srv.backups))
	}
}

func TestOnStateCalled(t *testing.T) {
	f := diverged(t)
	if _, err := f.sync.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	states := append([]State(nil), f.states...)
	f.mu.Unlock()
	if len(states) < 2 {
		t.Fatalf("OnState called %d times, want busy and the result", len(states))
	}
	if !states[0].Busy {
		t.Errorf("first state = %+v, want Busy", states[0])
	}
	want := State{Connected: true, Dirty: true, RestoreAvailable: true}
	if got := f.lastState(); got != want {
		t.Errorf("last state = %+v, want %+v", got, want)
	}
	if got := f.sync.State(); got != want {
		t.Errorf("State() = %+v, want %+v", got, want)
	}
	for i := 1; i < len(states); i++ {
		if states[i] == states[i-1] {
			t.Errorf("state %d repeats %+v", i, states[i])
		}
	}
}
