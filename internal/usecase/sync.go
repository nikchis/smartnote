package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
)

// LocalSnapshot is the local note set with the revision and server it was last
// synchronized with; ServerID is uuid.Nil before the first synchronization.
type LocalSnapshot struct {
	Notes    []domain.Note
	Revision int64
	ServerID uuid.UUID
}

// RemoteSnapshot is the server note set read in one consistent transaction.
type RemoteSnapshot struct {
	Notes    []domain.Note
	Revision int64
	ServerID uuid.UUID
}

// SyncStateStore is the local side of synchronization.
type SyncStateStore interface {
	LocalSnapshot(ctx context.Context) (LocalSnapshot, error)
	// ApplyRestore applies p and stores revision and serverID in one transaction.
	ApplyRestore(ctx context.Context, p RestorePlan, revision int64, serverID uuid.UUID) error
	SetSyncState(ctx context.Context, serverID uuid.UUID, revision int64) error
}

// OpenMode tells RemoteConnector.Open whether it may create the schema.
type OpenMode int

const (
	// OpenCreate creates the schema and the server key when they are missing.
	OpenCreate OpenMode = iota
	// OpenCheck only checks; RemoteSession.Empty reports a missing schema.
	OpenCheck
)

// RemoteConnector opens a session with the server: SSH, PostgreSQL through
// the tunnel and the sync phrase check. It returns only a ready session.
type RemoteConnector interface {
	Open(ctx context.Context, s domain.ConnectionSettings, mode OpenMode) (RemoteSession, error)
}

// RemoteSession is one operation's connection with the server.
type RemoteSession interface {
	// Created reports whether this Open created the schema.
	Created() bool
	// Empty reports, in OpenCheck mode, that the schema has no meta row.
	Empty() bool
	Snapshot(ctx context.Context) (RemoteSnapshot, error)
	// Backup replaces the server set with notes if the server still has
	// expectedServerID at expectedRevision, and returns the new revision;
	// otherwise it returns domain.ErrRemoteChanged without changes.
	Backup(ctx context.Context, notes []domain.Note, expectedServerID uuid.UUID, expectedRevision int64) (int64, error)
	Close() error
}

// ConnectStatus is the result of the connect check (spec §6.3, scenario 1).
type ConnectStatus string

// Connect check results.
const (
	StatusOffline     ConnectStatus = "offline"      // settings are empty
	StatusCreated     ConnectStatus = "created"      // the schema was created and backed up
	StatusInSync      ConnectStatus = "in_sync"      // no differences
	StatusDiverged    ConnectStatus = "diverged"     // differences, the server is not newer
	StatusRemoteNewer ConnectStatus = "remote_newer" // differences, the server is newer: ask to Restore
)

// ConnectOutcome is what Connect found; Restore counts the pending Restore of
// StatusRemoteNewer.
type ConnectOutcome struct {
	Status  ConnectStatus
	Restore Counts
}

// State drives the Backup and Restore buttons.
type State struct {
	Connected, Dirty, RestoreAvailable, Busy bool
}

// SyncDeps are the dependencies of Sync. OnState may be nil.
type SyncDeps struct {
	Local    SyncStateStore
	Settings SettingsStore
	Remote   RemoteConnector
	Changes  *ChangeTracker
	IDs      IDGenerator
	Gate     *Gate
	OnState  func(State)
}

// Sync runs the connect check, Backup and Restore of v1 with revisions
// (spec §6.2–§6.6). It keeps one pending plan between Prepare and Commit for
// the whole app. Every method with the server enters the shared Gate.
type Sync struct {
	d SyncDeps

	mu               sync.Mutex
	connected        bool
	restoreAvailable bool
	busy             bool
	pendingBackup    *BackupPlan
	pendingRestore   *RestorePlan
	published        State
}

// NewSync returns a Sync over d.
func NewSync(d SyncDeps) (*Sync, error) {
	switch {
	case d.Local == nil:
		return nil, nilDependency("NewSync", "local")
	case d.Settings == nil:
		return nil, nilDependency("NewSync", "settings")
	case d.Remote == nil:
		return nil, nilDependency("NewSync", "remote")
	case d.Changes == nil:
		return nil, nilDependency("NewSync", "changes")
	case d.IDs == nil:
		return nil, nilDependency("NewSync", "ids")
	case d.Gate == nil:
		return nil, nilDependency("NewSync", "gate")
	}
	return &Sync{d: d}, nil
}

// State returns the current state.
func (s *Sync) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked()
}

func (s *Sync) stateLocked() State {
	return State{
		Connected:        s.connected,
		Dirty:            s.d.Changes.Dirty(),
		RestoreAvailable: s.restoreAvailable,
		Busy:             s.busy,
	}
}

// publish calls OnState if the state differs from the last one published.
func (s *Sync) publish() {
	s.mu.Lock()
	st := s.stateLocked()
	changed := st != s.published
	s.published = st
	s.mu.Unlock()
	if changed && s.d.OnState != nil {
		s.d.OnState(st)
	}
}

func (s *Sync) update(f func()) {
	s.mu.Lock()
	f()
	s.mu.Unlock()
	s.publish()
}

// begin enters the gate and marks the state busy; end undoes both.
func (s *Sync) begin() (end func(), err error) {
	leave, err := s.d.Gate.Enter()
	if err != nil {
		return nil, err
	}
	s.update(func() { s.busy = true })
	return func() {
		s.update(func() { s.busy = false })
		leave()
	}, nil
}

// afterRemote records the result of an operation with the server: success
// connects, an error disconnects unless it shows the server was reached or the
// input was wrong.
func (s *Sync) afterRemote(err error) {
	if err != nil && keepsConnection(err) {
		s.publish()
		return
	}
	s.update(func() { s.connected = err == nil })
}

func keepsConnection(err error) bool {
	if errors.Is(err, domain.ErrWrongSyncPhrase) || errors.Is(err, domain.ErrRemoteChanged) {
		return true
	}
	if _, ok := errors.AsType[*domain.ValidationError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*domain.HostKeyUnknownError](err); ok {
		return true
	}
	_, ok := errors.AsType[*domain.HostKeyMismatchError](err)
	return ok
}

// CancelPending drops the plan kept between Prepare and Commit.
func (s *Sync) CancelPending() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingBackup, s.pendingRestore = nil, nil
}

// Connect runs the connect check (spec §6.3, scenario 1). For
// StatusRemoteNewer it keeps a Restore plan for CommitRestore.
func (s *Sync) Connect(ctx context.Context) (ConnectOutcome, error) {
	end, err := s.begin()
	if err != nil {
		return ConnectOutcome{}, err
	}
	defer end()
	s.CancelPending()

	settings, err := s.d.Settings.LoadSettings(ctx)
	if err != nil {
		return ConnectOutcome{}, fmt.Errorf("connect: load settings: %w", err)
	}
	if settings.IsEmpty() {
		s.update(func() { s.connected, s.restoreAvailable = false, false })
		return ConnectOutcome{Status: StatusOffline}, nil
	}
	out, err := s.connect(ctx, settings)
	s.afterRemote(err)
	if err != nil {
		return ConnectOutcome{}, fmt.Errorf("connect: %w", err)
	}
	return out, nil
}

func (s *Sync) connect(ctx context.Context, settings domain.ConnectionSettings) (out ConnectOutcome, err error) {
	sess, err := s.d.Remote.Open(ctx, settings, OpenCreate)
	if err != nil {
		return ConnectOutcome{}, err
	}
	defer closeSession(sess, &err)

	gen := s.d.Changes.Generation()
	local, err := s.d.Local.LocalSnapshot(ctx)
	if err != nil {
		return ConnectOutcome{}, fmt.Errorf("local snapshot: %w", err)
	}
	remote, err := sess.Snapshot(ctx)
	if err != nil {
		return ConnectOutcome{}, fmt.Errorf("remote snapshot: %w", err)
	}

	if sess.Created() {
		if err := s.backup(ctx, sess, PlanBackup(local, remote, gen)); err != nil {
			return ConnectOutcome{}, err
		}
		return ConnectOutcome{Status: StatusCreated}, nil
	}

	if local.ServerID != remote.ServerID {
		if err := s.d.Local.SetSyncState(ctx, remote.ServerID, 0); err != nil {
			return ConnectOutcome{}, fmt.Errorf("reset sync state: %w", err)
		}
		local.ServerID, local.Revision = remote.ServerID, 0
	}

	if !Diff(local.Notes, remote.Notes) {
		if local.Revision != remote.Revision {
			if err := s.d.Local.SetSyncState(ctx, remote.ServerID, remote.Revision); err != nil {
				return ConnectOutcome{}, fmt.Errorf("align revision: %w", err)
			}
		}
		s.d.Changes.ClearIf(gen)
		s.update(func() { s.restoreAvailable = false })
		return ConnectOutcome{Status: StatusInSync}, nil
	}

	s.d.Changes.MarkDiverged()
	if remote.Revision <= local.Revision {
		s.update(func() { s.restoreAvailable = true })
		return ConnectOutcome{Status: StatusDiverged}, nil
	}
	plan := PlanRestore(local, remote, s.d.IDs, gen)
	s.update(func() { s.restoreAvailable, s.pendingRestore = true, &plan })
	return ConnectOutcome{Status: StatusRemoteNewer, Restore: plan.Counts}, nil
}

// PrepareBackup reads both snapshots, keeps a Backup plan and returns its counts.
func (s *Sync) PrepareBackup(ctx context.Context) (Counts, error) {
	end, err := s.begin()
	if err != nil {
		return Counts{}, err
	}
	defer end()
	s.CancelPending()

	plan, err := s.prepareBackup(ctx)
	s.afterRemote(err)
	if err != nil {
		return Counts{}, fmt.Errorf("prepare backup: %w", err)
	}
	s.update(func() { s.pendingBackup = &plan })
	return plan.Counts, nil
}

func (s *Sync) prepareBackup(ctx context.Context) (plan BackupPlan, err error) {
	sess, err := s.open(ctx, OpenCreate)
	if err != nil {
		return BackupPlan{}, err
	}
	defer closeSession(sess, &err)

	gen := s.d.Changes.Generation()
	local, err := s.d.Local.LocalSnapshot(ctx)
	if err != nil {
		return BackupPlan{}, fmt.Errorf("local snapshot: %w", err)
	}
	remote, err := sess.Snapshot(ctx)
	if err != nil {
		return BackupPlan{}, fmt.Errorf("remote snapshot: %w", err)
	}
	return PlanBackup(local, remote, gen), nil
}

// CommitBackup sends the plan from PrepareBackup. On any error the local
// revision and the change flag stay as they were (spec §6.5).
func (s *Sync) CommitBackup(ctx context.Context) error {
	end, err := s.begin()
	if err != nil {
		return err
	}
	defer end()

	s.mu.Lock()
	plan := s.pendingBackup
	s.pendingBackup = nil
	s.mu.Unlock()
	if plan == nil {
		return fmt.Errorf("commit backup: %w", domain.ErrNoPendingOperation)
	}

	err = s.commitBackup(ctx, *plan)
	s.afterRemote(err)
	if err != nil {
		return fmt.Errorf("commit backup: %w", err)
	}
	return nil
}

func (s *Sync) commitBackup(ctx context.Context, plan BackupPlan) (err error) {
	sess, err := s.open(ctx, OpenCreate)
	if err != nil {
		return err
	}
	defer closeSession(sess, &err)
	return s.backup(ctx, sess, plan)
}

// backup writes plan to the server, then the new revision locally, and clears
// the change flag if nothing was edited since the plan's snapshot.
func (s *Sync) backup(ctx context.Context, sess RemoteSession, plan BackupPlan) error {
	rev, err := sess.Backup(ctx, plan.Notes, plan.ServerID, plan.ExpectedRevision)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	if err := s.d.Local.SetSyncState(ctx, plan.ServerID, rev); err != nil {
		return fmt.Errorf("store revision: %w", err)
	}
	s.d.Changes.ClearIf(plan.Generation)
	s.update(func() { s.restoreAvailable = false })
	return nil
}

// PrepareRestore reads both snapshots, keeps a Restore plan and returns its
// counts. It never creates the schema: a missing one means the server changed.
func (s *Sync) PrepareRestore(ctx context.Context) (Counts, error) {
	end, err := s.begin()
	if err != nil {
		return Counts{}, err
	}
	defer end()
	s.CancelPending()

	plan, err := s.prepareRestore(ctx)
	s.afterRemote(err)
	if err != nil {
		return Counts{}, fmt.Errorf("prepare restore: %w", err)
	}
	s.update(func() { s.pendingRestore = &plan })
	return plan.Counts, nil
}

func (s *Sync) prepareRestore(ctx context.Context) (plan RestorePlan, err error) {
	sess, err := s.open(ctx, OpenCheck)
	if err != nil {
		return RestorePlan{}, err
	}
	defer closeSession(sess, &err)
	if sess.Empty() {
		return RestorePlan{}, fmt.Errorf("remote storage is empty: %w", domain.ErrRemoteChanged)
	}

	gen := s.d.Changes.Generation()
	local, err := s.d.Local.LocalSnapshot(ctx)
	if err != nil {
		return RestorePlan{}, fmt.Errorf("local snapshot: %w", err)
	}
	remote, err := sess.Snapshot(ctx)
	if err != nil {
		return RestorePlan{}, fmt.Errorf("remote snapshot: %w", err)
	}
	return PlanRestore(local, remote, s.d.IDs, gen), nil
}

// CommitRestore applies the plan from Connect or PrepareRestore locally. If a
// note was edited since its snapshot it returns domain.ErrLocalChanged and
// writes nothing.
func (s *Sync) CommitRestore(ctx context.Context) error {
	end, err := s.begin()
	if err != nil {
		return err
	}
	defer end()

	s.mu.Lock()
	plan := s.pendingRestore
	s.pendingRestore = nil
	s.mu.Unlock()
	if plan == nil {
		return fmt.Errorf("commit restore: %w", domain.ErrNoPendingOperation)
	}
	if s.d.Changes.Generation() != plan.Generation {
		return fmt.Errorf("commit restore: %w", domain.ErrLocalChanged)
	}
	if err := s.d.Local.ApplyRestore(ctx, *plan, plan.Revision, plan.ServerID); err != nil {
		return fmt.Errorf("commit restore: %w", err)
	}
	s.d.Changes.ClearIf(plan.Generation)
	s.update(func() { s.restoreAvailable = false })
	return nil
}

// open loads the settings and opens a session; empty settings return
// domain.ErrNotConnected.
func (s *Sync) open(ctx context.Context, mode OpenMode) (RemoteSession, error) {
	settings, err := s.d.Settings.LoadSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	if settings.IsEmpty() {
		return nil, domain.ErrNotConnected
	}
	sess, err := s.d.Remote.Open(ctx, settings, mode)
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// closeSession closes sess and adds a close error to *err.
func closeSession(sess RemoteSession, err *error) {
	if cerr := sess.Close(); cerr != nil {
		*err = errors.Join(*err, fmt.Errorf("close remote session: %w", cerr))
	}
}
