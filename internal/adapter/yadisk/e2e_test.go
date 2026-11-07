package yadisk

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
	"github.com/nikchis/smartnote/internal/usecase"
)

// The end-to-end tests run usecase.Sync over blobremote.Connector and the
// stores of newStoreFactory (NewStoreFactory with the emulator's clients),
// with in-memory local storage: every layer of the Yandex Disk storage
// together, which the unit tests check only over fakes of each other.

// e2eKDF is the lowest allowed cost, to keep the test fast.
var e2eKDF = sealbox.KDFParams{Time: 1, MemoryKiB: 19456, Threads: 1}

// e2eLocal is the local note set of one device with its sync state.
type e2eLocal struct {
	mu       sync.Mutex
	notes    map[uuid.UUID]domain.Note
	revision int64
	serverID uuid.UUID
}

func (l *e2eLocal) LocalSnapshot(context.Context) (usecase.LocalSnapshot, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return usecase.LocalSnapshot{Notes: slices.Collect(maps.Values(l.notes)), Revision: l.revision, ServerID: l.serverID}, nil
}

func (l *e2eLocal) ApplyRestore(_ context.Context, p usecase.RestorePlan, revision int64, serverID uuid.UUID) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	for _, n := range slices.Concat(p.Insert, p.Rename) {
		l.notes[n.ID] = n
	}

	for _, c := range p.Text {
		l.notes[c.Note.ID] = c.Note
	}

	for _, id := range p.Delete {
		delete(l.notes, id)
	}

	l.revision, l.serverID = revision, serverID

	return nil
}

func (l *e2eLocal) SetSyncState(_ context.Context, serverID uuid.UUID, revision int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.revision, l.serverID = revision, serverID

	return nil
}

func (l *e2eLocal) put(n domain.Note) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.notes[n.ID] = n
}

// content returns the names and texts of the notes by ID.
func (l *e2eLocal) content() map[uuid.UUID][2]string {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := map[uuid.UUID][2]string{}
	for id, n := range l.notes {
		out[id] = [2]string{n.Name, n.Info}
	}

	return out
}

// e2eConfig is the settings and the saved token of one device.
type e2eConfig struct {
	settings domain.ConnectionSettings
	token    usecase.CloudToken
}

func (c *e2eConfig) LoadSettings(context.Context) (domain.ConnectionSettings, error) {
	return c.settings, nil
}

func (c *e2eConfig) SaveSettings(_ context.Context, s domain.ConnectionSettings) error {
	c.settings = s
	return nil
}

func (c *e2eConfig) LoadCloudToken(context.Context) (usecase.CloudToken, bool, error) {
	return c.token, true, nil
}

func (c *e2eConfig) SaveCloudToken(_ context.Context, t usecase.CloudToken) error {
	c.token = t
	return nil
}

// e2eDevice is one Smartnote installation over the emulated Disk.
type e2eDevice struct {
	sync    *usecase.Sync
	local   *e2eLocal
	changes *usecase.ChangeTracker
	pending *fakePending
}

func newE2EDevice(t *testing.T, h *harness, pending *fakePending) *e2eDevice {
	t.Helper()

	dev := &e2eDevice{local: &e2eLocal{notes: map[uuid.UUID]domain.Note{}}, changes: &usecase.ChangeTracker{}, pending: pending}
	cfg := &e2eConfig{
		settings: domain.ConnectionSettings{StorageKind: domain.KindYandexDisk, YandexClientID: fakeClientID, SyncPhrase: "sixteen chars ok"},
		token: usecase.CloudToken{
			Provider: fakeProvider, ClientID: fakeClientID, Access: "acc-1", Expiry: testNow.Add(time.Hour),
		},
	}
	factory := newStoreFactory(h.disk.endpoints(), pending, h.options())

	s, err := usecase.NewSync(usecase.SyncDeps{
		Local: dev.local, Settings: cfg, Tokens: cfg,
		Remote:  blobremote.NewConnector(factory, e2eKDF),
		Changes: dev.changes, IDs: uuid.New, Gate: &usecase.Gate{}, Offer: &usecase.HostKeyOffer{},
		Warn: func(err error) { t.Errorf("Warn: %v", err) },
	})
	if err != nil {
		t.Fatal(err)
	}

	dev.sync = s

	return dev
}

func (dev *e2eDevice) connect(t *testing.T) usecase.ConnectOutcome {
	t.Helper()

	out, err := dev.sync.Connect(t.Context())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}

	return out
}

func (dev *e2eDevice) edit(n domain.Note) {
	dev.local.put(n)
	dev.changes.MarkChanged()
}

func (dev *e2eDevice) backup(t *testing.T) error {
	t.Helper()

	if _, err := dev.sync.PrepareBackup(t.Context()); err != nil {
		t.Fatalf("PrepareBackup: %v", err)
	}

	return dev.sync.CommitBackup(t.Context())
}

func TestEndToEndTwoDevices(t *testing.T) {
	h := newHarness(t)
	first := newE2EDevice(t, h, h.pending)
	second := newE2EDevice(t, h, &fakePending{disk: h.disk})

	garden := domain.Note{ID: uuid.New(), Name: "Garden", Info: "tomatoes", Updated: testNow}
	journal := domain.Note{ID: uuid.New(), Name: "Journal", Info: "day one", Updated: testNow}

	// The first Connect creates the container and backs up the local notes.
	first.edit(garden)

	if out := first.connect(t); out.Status != usecase.StatusCreated {
		t.Fatalf("first Connect = %+v, want created", out)
	}

	if h.disk.file(container) == nil {
		t.Fatal("the container was not created")
	}

	// A Backup reads the container right after the Write of the Connect.
	first.edit(domain.Note{ID: garden.ID, Name: "Garden", Info: "tomatoes, beans", Updated: testNow})
	first.edit(journal)

	if err := first.backup(t); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	if first.changes.Dirty() {
		t.Error("the change flag stayed after the Backup")
	}

	// The second device finds a newer storage and restores it.
	out := second.connect(t)
	if out.Status != usecase.StatusRemoteNewer || out.Restore.Added != 2 {
		t.Fatalf("second Connect = %+v, want remote_newer with 2 added", out)
	}

	if err := second.sync.CommitRestore(t.Context()); err != nil {
		t.Fatalf("CommitRestore: %v", err)
	}

	if got, want := second.local.content(), first.local.content(); !maps.Equal(got, want) {
		t.Errorf("restored notes = %v, want %v", got, want)
	}

	for name, dev := range map[string]*e2eDevice{"first": first, "second": second} {
		if out := dev.connect(t); out.Status != usecase.StatusInSync {
			t.Errorf("%s device Connect = %+v, want in_sync", name, out)
		}
	}

	wantNoTemps(t, h.disk)
}

// TestEndToEndUnknownOutcome: a Backup whose move and fence both fail is
// unknown; the next Connect fences the temporary file first, and the late
// move cannot overwrite the storage.
func TestEndToEndUnknownOutcome(t *testing.T) {
	h := newHarness(t)
	first := newE2EDevice(t, h, h.pending)
	second := newE2EDevice(t, h, &fakePending{disk: h.disk})

	garden := domain.Note{ID: uuid.New(), Name: "Garden", Info: "tomatoes", Updated: testNow}
	first.edit(garden)

	if out := first.connect(t); out.Status != usecase.StatusCreated {
		t.Fatalf("first Connect = %+v, want created", out)
	}

	saved := h.disk.file(container).data

	first.edit(domain.Note{ID: garden.ID, Name: "Garden", Info: "lost edit", Updated: testNow})
	h.disk.set(func(d *fakeDisk) { d.deferMove, d.dropPollAfter, d.deleteFail = true, 1, true })

	if err := first.backup(t); !errors.Is(err, domain.ErrRemoteOutcomeUnknown) {
		t.Fatalf("Backup: err = %v, want ErrRemoteOutcomeUnknown", err)
	}

	if first.sync.State().Connected {
		t.Error("still connected after an unknown outcome")
	}

	h.disk.set(func(d *fakeDisk) { d.deferMove, d.dropPollAfter, d.deleteFail = false, 0, false })

	if out := first.connect(t); out.Status != usecase.StatusDiverged {
		t.Errorf("Connect after the unknown outcome = %+v, want diverged", out)
	}

	if _, found := h.pending.state(); found {
		t.Error("cloud_pending kept after the fence")
	}

	h.disk.runDeferred()
	wantContainer(t, h.disk, saved)

	// The edit is still there to back up, and the other device gets it.
	if err := first.backup(t); err != nil {
		t.Fatalf("second Backup: %v", err)
	}

	if out := second.connect(t); out.Status != usecase.StatusRemoteNewer {
		t.Fatalf("second device Connect = %+v, want remote_newer", out)
	}

	if err := second.sync.CommitRestore(t.Context()); err != nil {
		t.Fatalf("CommitRestore: %v", err)
	}

	if got := second.local.content()[garden.ID]; got != [2]string{"Garden", "lost edit"} {
		t.Errorf("second device note = %v, want the edit", got)
	}
}

// TestEndToEndUnstableVersion: a container whose md5 does not settle (it
// may appear late after a move, spec §12) is remote_changed for the user,
// and the connection stays (final review I1).
func TestEndToEndUnstableVersion(t *testing.T) {
	h := newHarness(t)
	dev := newE2EDevice(t, h, h.pending)
	dev.edit(domain.Note{ID: uuid.New(), Name: "Garden", Info: "tomatoes", Updated: testNow})

	if out := dev.connect(t); out.Status != usecase.StatusCreated {
		t.Fatalf("first Connect = %+v, want created", out)
	}

	h.disk.set(func(d *fakeDisk) { d.files["app:/"+container].md5 = "" })

	if _, err := dev.sync.Connect(t.Context()); !errors.Is(err, domain.ErrRemoteChanged) {
		t.Fatalf("Connect: err = %v, want ErrRemoteChanged", err)
	}

	if !dev.sync.State().Connected {
		t.Error("disconnected by remote_changed")
	}
}
