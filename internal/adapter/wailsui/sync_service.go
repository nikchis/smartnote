package wailsui

import "github.com/nikchis/smartnote/internal/usecase"

// SyncService runs the connect check, Backup and Restore. Every operation
// with the server gets operationTimeout.
type SyncService struct {
	env  Env
	sync *usecase.Sync
}

// NewSyncService returns a SyncService over s.
func NewSyncService(env Env, s *usecase.Sync) *SyncService {
	return &SyncService{env: env, sync: s}
}

// Connect runs the connect check (spec §6.3, scenario 1).
func (s *SyncService) Connect() (ConnectDTO, error) {
	ctx, cancel := s.env.remote()
	defer cancel()

	out, err := s.sync.Connect(ctx)
	if err != nil {
		return ConnectDTO{}, s.env.fail("connect", err)
	}

	return ConnectDTO{Status: string(out.Status), Restore: toCountsDTO(out.Restore)}, nil
}

// PrepareBackup plans a Backup and keeps the plan for CommitBackup.
func (s *SyncService) PrepareBackup() (BackupPreviewDTO, error) {
	ctx, cancel := s.env.remote()
	defer cancel()

	p, err := s.sync.PrepareBackup(ctx)
	if err != nil {
		return BackupPreviewDTO{}, s.env.fail("prepare backup", err)
	}

	return toBackupPreviewDTO(p), nil
}

// CommitBackup runs the planned Backup.
func (s *SyncService) CommitBackup() error {
	ctx, cancel := s.env.remote()
	defer cancel()

	return s.env.fail("backup", s.sync.CommitBackup(ctx))
}

// PrepareRestore plans a Restore and keeps the plan for CommitRestore.
func (s *SyncService) PrepareRestore() (CountsDTO, error) {
	ctx, cancel := s.env.remote()
	defer cancel()

	c, err := s.sync.PrepareRestore(ctx)
	if err != nil {
		return CountsDTO{}, s.env.fail("prepare restore", err)
	}

	return toCountsDTO(c), nil
}

// CommitRestore runs the planned Restore, from PrepareRestore or Connect.
func (s *SyncService) CommitRestore() error {
	ctx, cancel := s.env.remote()
	defer cancel()

	return s.env.fail("restore", s.sync.CommitRestore(ctx))
}

// CancelPending drops the plan the user declined.
func (s *SyncService) CancelPending() { s.sync.CancelPending() }

// State returns the current synchronization state.
func (s *SyncService) State() StateDTO { return ToStateDTO(s.sync.State()) }
