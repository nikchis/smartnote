package blobremote

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/adapter/remotetest"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// fakeStore is an in-memory ObjectStore with error hooks and call counters.
type fakeStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	version int
	reads   int
	writes  int

	// readHook and writeHook run first; a non-nil result is returned as is.
	readHook  func(n int) error
	writeHook func(n int, name string, expect string) error
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string][]byte{}} }

func (f *fakeStore) Read(_ context.Context, name string) (data []byte, version string, found bool, err error) {
	f.mu.Lock()
	f.reads++
	n, hook := f.reads, f.readHook
	f.mu.Unlock()

	// Hooks run outside the lock: they may use the store themselves.
	if hook != nil {
		if err := hook(n); err != nil {
			return nil, "", false, err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	obj, ok := f.objects[name]
	if !ok {
		return nil, "", false, nil
	}

	return bytes.Clone(obj), strconv.Itoa(f.version), true, nil
}

func (f *fakeStore) Write(_ context.Context, name string, data []byte, expect string) error {
	f.mu.Lock()
	f.writes++
	n, hook := f.writes, f.writeHook
	f.mu.Unlock()

	if hook != nil {
		if err := hook(n, name, expect); err != nil {
			return err
		}
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	if _, ok := f.objects[name]; ok != (expect != "") || (ok && expect != strconv.Itoa(f.version)) {
		return ErrVersionMismatch
	}

	f.version++
	f.objects[name] = bytes.Clone(data)

	return nil
}

func openOver(f *fakeStore, phrase string, mode usecase.OpenMode) (*Session, error) {
	return NewSession(context.Background(), f, Options{Mode: mode, Phrase: phrase, KDF: testKDF})
}

func mustOpenOver(t *testing.T, f *fakeStore, mode usecase.OpenMode) *Session {
	t.Helper()

	s, err := openOver(f, testPhrase, mode)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	return s
}

func TestSessionContract(t *testing.T) {
	remotetest.RunSessionContract(t, func(*testing.T) remotetest.Opener {
		f := newFakeStore()

		return func(phrase string, mode usecase.OpenMode) (usecase.RemoteSession, error) {
			s, err := openOver(f, phrase, mode)
			if err != nil {
				return nil, err
			}

			return s, nil
		}
	})
}

func TestSnapshotNoSecondRead(t *testing.T) {
	f := newFakeStore()
	mustOpenOver(t, f, usecase.OpenCreate)

	f.reads = 0
	s := mustOpenOver(t, f, usecase.OpenCheck)

	if _, err := s.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	if f.reads != 1 {
		t.Errorf("reads = %d, want 1", f.reads)
	}
}

func TestBackupKeepsHeaderBytes(t *testing.T) {
	f := newFakeStore()
	s := mustOpenOver(t, f, usecase.OpenCreate)

	parsed, _, _, err := parseHeader(f.objects[ContainerName])
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}

	before := bytes.Clone(parsed)

	snap, _ := s.Snapshot(context.Background())
	if _, err := s.Backup(context.Background(), nil, snap.ServerID, snap.Revision); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	after, _, _, err := parseHeader(f.objects[ContainerName])
	if err != nil || !bytes.Equal(before, after) {
		t.Errorf("header changed: err %v", err)
	}
}

func TestBackupAfterBackupRereads(t *testing.T) {
	f := newFakeStore()
	s := mustOpenOver(t, f, usecase.OpenCreate)
	snap, _ := s.Snapshot(context.Background())

	rev, err := s.Backup(context.Background(), nil, snap.ServerID, snap.Revision)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	f.reads = 0

	if _, err := s.Backup(context.Background(), nil, snap.ServerID, rev); err != nil {
		t.Fatalf("second Backup: %v", err)
	}

	if f.reads != 1 {
		t.Errorf("reads = %d, want 1", f.reads)
	}
}

func TestBackupTooLarge(t *testing.T) {
	f := newFakeStore()
	s := mustOpenOver(t, f, usecase.OpenCreate)
	snap, _ := s.Snapshot(context.Background())
	f.writes = 0

	// maxFile of incompressible-size text: names fit the 64 MiB limit only
	// in small numbers, so use many large infos.
	big := make([]domain.Note, 70)
	for i := range big {
		big[i] = domain.Note{ID: uuid.New(), Name: "n", Info: string(bytes.Repeat([]byte("x"), 1<<20))}
	}

	_, err := s.Backup(context.Background(), big, snap.ServerID, snap.Revision)
	if !errors.Is(err, domain.ErrRemoteTooLarge) {
		t.Fatalf("err = %v, want ErrRemoteTooLarge", err)
	}

	if f.writes != 0 {
		t.Errorf("writes = %d, want 0", f.writes)
	}
}

func TestBackupErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   error
		want error
	}{
		{"mismatch", ErrVersionMismatch, domain.ErrRemoteChanged},
		{"unknown", ErrOutcomeUnknown, domain.ErrRemoteOutcomeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeStore()
			s := mustOpenOver(t, f, usecase.OpenCreate)
			snap, _ := s.Snapshot(context.Background())
			f.writeHook = func(int, string, string) error { return tc.in }

			if _, err := s.Backup(context.Background(), nil, snap.ServerID, snap.Revision); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestBackupOtherStoreErrorPassesThrough(t *testing.T) {
	boom := errors.New("boom")
	f := newFakeStore()
	s := mustOpenOver(t, f, usecase.OpenCreate)
	snap, _ := s.Snapshot(context.Background())
	f.writeHook = func(int, string, string) error { return boom }

	_, err := s.Backup(context.Background(), nil, snap.ServerID, snap.Revision)
	if !errors.Is(err, boom) || errors.Is(err, domain.ErrRemoteChanged) {
		t.Errorf("err = %v", err)
	}
}

func TestOpenReadOutcomeUnknown(t *testing.T) {
	f := newFakeStore()
	f.readHook = func(int) error { return ErrOutcomeUnknown }

	if _, err := openOver(f, testPhrase, usecase.OpenCheck); !errors.Is(err, domain.ErrRemoteOutcomeUnknown) {
		t.Errorf("err = %v, want ErrRemoteOutcomeUnknown", err)
	}
}

func TestCreateWriteOutcomeUnknown(t *testing.T) {
	f := newFakeStore()
	f.writeHook = func(int, string, string) error { return ErrOutcomeUnknown }

	if _, err := openOver(f, testPhrase, usecase.OpenCreate); !errors.Is(err, domain.ErrRemoteOutcomeUnknown) {
		t.Errorf("err = %v, want ErrRemoteOutcomeUnknown", err)
	}
}

func TestCreateRace(t *testing.T) {
	f := newFakeStore()
	// Another device creates the container between our Read and Write.
	f.writeHook = func(n int, _, expect string) error {
		if n != 1 {
			return nil
		}

		if expect != "" {
			t.Errorf("expect = %q, want empty", expect)
		}

		f.writeHook = nil

		other, err := NewSession(context.Background(), f, Options{Mode: usecase.OpenCreate, Phrase: testPhrase, KDF: testKDF})
		if err != nil {
			t.Fatalf("other device: %v", err)
		}

		_ = other.Close()

		return ErrVersionMismatch
	}

	s := mustOpenOver(t, f, usecase.OpenCreate)
	if s.Created() || s.Empty() {
		t.Errorf("Created = %v, Empty = %v; want false, false", s.Created(), s.Empty())
	}

	if _, err := s.Snapshot(context.Background()); err != nil {
		t.Errorf("Snapshot: %v", err)
	}
}

func TestCreateRaceWrongPhrase(t *testing.T) {
	f := newFakeStore()
	f.writeHook = func(int, string, string) error {
		f.writeHook = nil

		other, err := NewSession(context.Background(), f, Options{Mode: usecase.OpenCreate, Phrase: "another phrase ok!", KDF: testKDF})
		if err != nil {
			t.Fatalf("other device: %v", err)
		}

		_ = other.Close()

		return ErrVersionMismatch
	}

	if _, err := openOver(f, testPhrase, usecase.OpenCreate); !errors.Is(err, domain.ErrWrongSyncPhrase) {
		t.Errorf("err = %v, want ErrWrongSyncPhrase", err)
	}
}

func TestDamagedContainer(t *testing.T) {
	f := newFakeStore()
	f.objects[ContainerName] = []byte("garbage")

	if _, err := openOver(f, testPhrase, usecase.OpenCheck); !errors.Is(err, domain.ErrRemoteDamaged) {
		t.Errorf("err = %v, want ErrRemoteDamaged", err)
	}
}

func TestReplacedContainerAfterBackupIsChanged(t *testing.T) {
	f := newFakeStore()
	s := mustOpenOver(t, f, usecase.OpenCreate)
	snap, _ := s.Snapshot(context.Background())

	rev, err := s.Backup(context.Background(), nil, snap.ServerID, snap.Revision)
	if err != nil {
		t.Fatal(err)
	}

	// Another device replaces the storage with a new one.
	delete(f.objects, ContainerName)
	mustOpenOver(t, f, usecase.OpenCreate)

	if _, err := s.Backup(context.Background(), nil, snap.ServerID, rev); !errors.Is(err, domain.ErrRemoteChanged) {
		t.Errorf("err = %v, want ErrRemoteChanged", err)
	}
}

func TestCloseWipesKey(t *testing.T) {
	s := mustOpenOver(t, newFakeStore(), usecase.OpenCreate)
	key := s.key

	if len(key) == 0 || bytes.Equal(key, make([]byte, len(key))) {
		t.Fatal("no key to wipe")
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if !bytes.Equal(key, make([]byte, len(key))) {
		t.Error("key not wiped")
	}

	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}

	if _, err := s.Snapshot(context.Background()); err == nil {
		t.Error("Snapshot after Close succeeded")
	}
}

func TestConnectorOpen(t *testing.T) {
	f := newFakeStore()
	c := NewConnector(func(usecase.RemoteTarget) (ObjectStore, error) { return f, nil }, testKDF)

	target := usecase.RemoteTarget{Settings: domain.ConnectionSettings{SyncPhrase: testPhrase}}

	s, err := c.Open(context.Background(), target, usecase.OpenCreate)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	defer func() { _ = s.Close() }()

	if !s.Created() {
		t.Error("not created")
	}

	boom := errors.New("factory")
	bad := NewConnector(func(usecase.RemoteTarget) (ObjectStore, error) { return nil, boom }, testKDF)

	if _, err := bad.Open(context.Background(), target, usecase.OpenCheck); !errors.Is(err, boom) {
		t.Errorf("err = %v, want factory error", err)
	}
}
