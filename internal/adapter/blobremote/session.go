package blobremote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/adapter/remotecrypto"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
	"github.com/nikchis/smartnote/internal/usecase"
)

// errNoStorage is returned by Snapshot and Backup of a session without a
// container (Empty, or closed).
var errNoStorage = errors.New("remote storage is not open")

// Options configure NewSession. KDF is used only when the container is created.
type Options struct {
	Mode   usecase.OpenMode
	Phrase string
	KDF    sealbox.KDFParams
}

// Connector implements usecase.RemoteConnector over an ObjectStore.
type Connector struct {
	factory StoreFactory
	kdf     sealbox.KDFParams
}

// NewConnector returns a Connector that builds stores with f.
func NewConnector(f StoreFactory, kdf sealbox.KDFParams) *Connector {
	return &Connector{factory: f, kdf: kdf}
}

// Open builds the store for t and opens a session over it.
func (c *Connector) Open(ctx context.Context, t usecase.RemoteTarget, mode usecase.OpenMode) (usecase.RemoteSession, error) {
	store, err := c.factory(t)
	if err != nil {
		return nil, fmt.Errorf("open remote store: %w", err)
	}

	s, err := NewSession(ctx, store, Options{Mode: mode, Phrase: t.Settings.SyncPhrase, KDF: c.kdf})
	if err != nil {
		return nil, err
	}

	return s, nil
}

// Session is one operation's view of the container. It implements
// usecase.RemoteSession and is not safe for concurrent use.
type Session struct {
	store   ObjectStore
	created bool
	empty   bool

	key      []byte
	header   []byte
	meta     remotecrypto.Meta
	version  string
	revision int64
	notes    []domain.Note
	stale    bool // a Backup succeeded: the cached state is behind the store
	closed   bool
}

// NewSession opens the container as spec §5.1 describes.
func NewSession(ctx context.Context, store ObjectStore, o Options) (*Session, error) {
	s := &Session{store: store}

	if err := s.open(ctx, o); err != nil {
		_ = s.Close()
		return nil, err
	}

	return s, nil
}

func (s *Session) open(ctx context.Context, o Options) error {
	file, version, found, err := s.read(ctx)
	if err != nil {
		return err
	}

	if found {
		return s.adopt(file, version, o.Phrase)
	}

	if o.Mode == usecase.OpenCheck {
		s.empty = true
		return nil
	}

	return s.create(ctx, o)
}

// create writes a new container with revision 0, or, if another device was
// first, adopts the one it wrote.
func (s *Session) create(ctx context.Context, o Options) error {
	m, key, err := remotecrypto.NewMeta(o.Phrase, o.KDF)
	if err != nil {
		return err
	}

	defer func() { sealbox.Wipe(key) }()

	header, err := newHeader(m)
	if err != nil {
		return err
	}

	body, err := sealBody(key, header, m.ServerID, 0, nil)
	if err != nil {
		return err
	}

	file, err := assemble(header, body)
	if err != nil {
		return err
	}

	switch err := s.write(ctx, file, ""); {
	case err == nil:
		// Our own header bytes; the version is unknown, so the next use re-reads.
		s.key, key = key, nil
		s.header, s.meta, s.revision, s.notes = header, m, 0, []domain.Note{}
		s.created, s.stale = true, true

		return nil
	case errors.Is(err, domain.ErrRemoteChanged):
	default:
		return err
	}

	file, version, found, err := s.read(ctx)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("container disappeared during creation: %w", domain.ErrRemoteChanged)
	}

	return s.adopt(file, version, o.Phrase)
}

// adopt parses and unlocks a container file and caches its state.
func (s *Session) adopt(file []byte, version, phrase string) error {
	header, m, body, err := parseHeader(file)
	if err != nil {
		return err
	}

	key, err := m.Unlock(phrase)
	if err != nil {
		return err
	}

	revision, notes, err := openBody(key, header, m.ServerID, body)
	if err != nil {
		sealbox.Wipe(key)
		return err
	}

	sealbox.Wipe(s.key)
	s.key, s.header, s.meta, s.version = key, slices.Clone(header), m, version
	s.revision, s.notes, s.stale = revision, notes, false

	return nil
}

// refresh re-reads the container after this session's own Backup. A container
// that is gone or has other header bytes is another storage.
func (s *Session) refresh(ctx context.Context) error {
	file, version, found, err := s.read(ctx)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("container is gone: %w", domain.ErrRemoteChanged)
	}

	header, m, body, err := parseHeader(file)
	if err != nil {
		return err
	}

	if !slices.Equal(header, s.header) {
		return fmt.Errorf("container was replaced: %w", domain.ErrRemoteChanged)
	}

	revision, notes, err := openBody(s.key, header, m.ServerID, body)
	if err != nil {
		return err
	}

	s.version, s.revision, s.notes, s.stale = version, revision, notes, false

	return nil
}

func (s *Session) read(ctx context.Context) (file []byte, version string, found bool, err error) {
	file, version, found, err = s.store.Read(ctx, ContainerName)

	switch {
	case err == nil:
		return file, version, found, nil
	case errors.Is(err, ErrVersionMismatch):
		return nil, "", false, fmt.Errorf("read container: %w", domain.ErrRemoteChanged)
	case errors.Is(err, ErrOutcomeUnknown):
		return nil, "", false, fmt.Errorf("read container: %w", domain.ErrRemoteOutcomeUnknown)
	default:
		return nil, "", false, fmt.Errorf("read container: %w", err)
	}
}

// write maps the store errors of Write (spec §5.3).
func (s *Session) write(ctx context.Context, file []byte, expect string) error {
	err := s.store.Write(ctx, ContainerName, file, expect)

	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrVersionMismatch):
		return fmt.Errorf("write container: %w", domain.ErrRemoteChanged)
	case errors.Is(err, ErrOutcomeUnknown):
		return fmt.Errorf("write container: %w", domain.ErrRemoteOutcomeUnknown)
	default:
		return fmt.Errorf("write container: %w", err)
	}
}

// Created reports whether this session created the container.
func (s *Session) Created() bool { return s.created }

// Empty reports, for OpenCheck, that there is no container.
func (s *Session) Empty() bool { return s.empty }

// Snapshot returns the state read at Open, or re-read after a Backup.
func (s *Session) Snapshot(ctx context.Context) (usecase.RemoteSnapshot, error) {
	if s.key == nil {
		return usecase.RemoteSnapshot{}, errNoStorage
	}

	if s.stale {
		if err := s.refresh(ctx); err != nil {
			return usecase.RemoteSnapshot{}, fmt.Errorf("remote snapshot: %w", err)
		}
	}

	return usecase.RemoteSnapshot{Notes: slices.Clone(s.notes), Revision: s.revision, ServerID: s.meta.ServerID}, nil
}

// Backup replaces the notes in the container if it is still the expected
// storage at the expected revision, and returns the new revision.
func (s *Session) Backup(ctx context.Context, notes []domain.Note, expectedServerID uuid.UUID, expectedRevision int64) (int64, error) {
	if s.key == nil {
		return 0, errNoStorage
	}

	if s.stale {
		if err := s.refresh(ctx); err != nil {
			return 0, fmt.Errorf("backup: %w", err)
		}
	}

	if s.meta.ServerID != expectedServerID || s.revision != expectedRevision {
		return 0, fmt.Errorf("backup: %w", domain.ErrRemoteChanged)
	}

	next := s.revision + 1

	body, err := sealBody(s.key, s.header, s.meta.ServerID, next, notes)
	if err != nil {
		return 0, fmt.Errorf("backup: %w", err)
	}

	file, err := assemble(s.header, body)
	if err != nil {
		return 0, fmt.Errorf("backup: %w", err)
	}

	if err := s.write(ctx, file, s.version); err != nil {
		return 0, fmt.Errorf("backup: %w", err)
	}

	// The new version is unknown: the next use re-reads.
	s.stale = true

	return next, nil
}

// Close wipes the key and closes the store when it is an io.Closer (the
// Yandex Disk store closes its idle connections). It is idempotent: the
// store is closed once, and later calls return nil.
func (s *Session) Close() error {
	sealbox.Wipe(s.key)
	s.key = nil

	if s.closed {
		return nil
	}

	s.closed = true

	if c, ok := s.store.(io.Closer); ok {
		if err := c.Close(); err != nil {
			return fmt.Errorf("close remote store: %w", err)
		}
	}

	return nil
}
