package gdrive

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
// stores of newStoreFactory with the emulator's clients, with in-memory
// local storage: every layer of the Google Drive storage together, which
// the unit tests check only over fakes of each other.

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

// LoadCloudToken returns the token of Google; there is none of other providers.
func (c *e2eConfig) LoadCloudToken(_ context.Context, p domain.StorageKind) (usecase.CloudToken, bool, error) {
	return c.token, p == domain.KindGoogleDrive, nil
}

func (c *e2eConfig) SaveCloudToken(_ context.Context, t usecase.CloudToken) error {
	c.token = t
	return nil
}

// e2eDevice is one Smartnote installation over the emulated Drive.
type e2eDevice struct {
	sync    *usecase.Sync
	local   *e2eLocal
	changes *usecase.ChangeTracker
	uploads *memUploads
}

func newE2EDevice(t *testing.T, d *fakeDrive) *e2eDevice {
	t.Helper()

	return newE2EDeviceWith(t, d, testOptions(d))
}

// newE2EDeviceWith builds a device whose stores take opts, as a progress.
func newE2EDeviceWith(t *testing.T, d *fakeDrive, opts storeOptions) *e2eDevice {
	t.Helper()

	dev := &e2eDevice{
		local: &e2eLocal{notes: map[uuid.UUID]domain.Note{}}, changes: &usecase.ChangeTracker{}, uploads: &memUploads{},
	}
	cfg := &e2eConfig{
		// The client is the built-in one: the settings carry no own client.
		settings: domain.ConnectionSettings{StorageKind: domain.KindGoogleDrive, SyncPhrase: "sixteen chars ok"},
		token: usecase.CloudToken{
			Provider: domain.KindGoogleDrive, ClientID: fakeClientID, Access: fakeAccess, Refresh: fakeRefresh,
			Expiry: testNow.Add(time.Hour),
		},
	}
	factory := newStoreFactory(d.endpoints(), dev.uploads, testRefresher(t, d), fakeClient, opts)

	s, err := usecase.NewSync(usecase.SyncDeps{
		Local: dev.local, Settings: cfg, Tokens: cfg,
		Builtin: map[domain.StorageKind]usecase.CloudClient{domain.KindGoogleDrive: fakeClient},
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

// containerFiles returns the number of live container files.
func containerFiles(d *fakeDrive) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	n := 0

	for _, f := range d.files {
		if f.name == container && !f.trashed {
			n++
		}
	}

	return n
}

func TestEndToEndTwoDevices(t *testing.T) {
	d := newFakeDrive(t)
	first, second := newE2EDevice(t, d), newE2EDevice(t, d)

	garden := domain.Note{ID: uuid.New(), Name: "Garden", Info: "tomatoes", Updated: testNow}
	journal := domain.Note{ID: uuid.New(), Name: "Journal", Info: "day one", Updated: testNow}

	// The first Connect creates the container and backs up the local notes.
	first.edit(garden)

	if out := first.connect(t); out.Status != usecase.StatusCreated {
		t.Fatalf("first Connect = %+v, want created", out)
	}

	if n := containerFiles(d); n != 1 {
		t.Fatalf("container files = %d, want 1", n)
	}

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

	if n := containerFiles(d); n != 1 {
		t.Errorf("container files = %d at the end, want 1", n)
	}

	wantNoRecord(t, first.uploads)
	wantNoRecord(t, second.uploads)
}

// TestEndToEndUnknownOutcome: the final PUT of a Backup is committed but its
// answer and the status are lost, so the outcome is unknown and the record
// of the upload stays (spec §5.4). The next Connect resolves the record
// before anything else and works with what the storage really has.
func TestEndToEndUnknownOutcome(t *testing.T) {
	d := newFakeDrive(t)
	first, second := newE2EDevice(t, d), newE2EDevice(t, d)

	garden := domain.Note{ID: uuid.New(), Name: "Garden", Info: "tomatoes", Updated: testNow}
	first.edit(garden)

	if out := first.connect(t); out.Status != usecase.StatusCreated {
		t.Fatalf("first Connect = %+v, want created", out)
	}

	first.edit(domain.Note{ID: garden.ID, Name: "Garden", Info: "late edit", Updated: testNow})

	// The body is committed, the answer is dropped, and the status does not
	// answer for as long as the Backup asks.
	failPut(d, putAnswer{drop: true})
	d.set(func(d *fakeDrive) { d.statusAnswers = []int{503, 503, 503, 503, 503, 503} })

	if err := first.backup(t); !errors.Is(err, domain.ErrRemoteOutcomeUnknown) {
		t.Fatalf("Backup: err = %v, want ErrRemoteOutcomeUnknown", err)
	}

	if first.sync.State().Connected {
		t.Error("still connected after an unknown outcome")
	}

	if _, found := first.uploads.record(t); !found {
		t.Error("the upload record was removed while the outcome is unknown")
	}

	// The status answers again: the next Connect resolves the record first.
	d.set(func(d *fakeDrive) { d.statusAnswers = nil })

	first.connect(t)

	wantNoRecord(t, first.uploads)

	if n := containerFiles(d); n != 1 {
		t.Errorf("container files = %d after the resolution, want 1", n)
	}

	// The storage is usable: the other device restores what the Backup wrote.
	out := second.connect(t)
	if out.Status != usecase.StatusRemoteNewer {
		t.Fatalf("second device Connect = %+v, want remote_newer", out)
	}

	if err := second.sync.CommitRestore(t.Context()); err != nil {
		t.Fatalf("CommitRestore: %v", err)
	}

	if got := second.local.content()[garden.ID]; got != [2]string{"Garden", "late edit"} {
		t.Errorf("second device note = %v, want the late edit", got)
	}
}

// transferRecorder runs calls of a device and returns the directions of the
// transfers each started: every meter reports 0 first.
type transferRecorder struct {
	mu  sync.Mutex
	got []usecase.TransferProgress
}

func (r *transferRecorder) add(p usecase.TransferProgress) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.got = append(r.got, p)
}

func (r *transferRecorder) list() []usecase.TransferProgress {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.got)
}

func (r *transferRecorder) run(t *testing.T, name string, call func(ctx context.Context) error) []string {
	t.Helper()

	before := len(r.list())

	if err := call(t.Context()); err != nil {
		t.Fatalf("%s: %v", name, err)
	}

	var out []string

	for _, p := range r.list()[before:] {
		if p.Done == 0 {
			out = append(out, p.Direction)
		}
	}

	return out
}

// TestEndToEndTransfers pins the transfers that report their progress in each
// call of the synchronization: the interface splits the bar of a process into
// equal shares by them (TRANSFERS in frontend/src/lib/overlay.svelte.ts).
func TestEndToEndTransfers(t *testing.T) {
	d := newFakeDrive(t)
	first := newE2EDevice(t, d)
	first.edit(domain.Note{ID: uuid.New(), Name: "Garden", Info: "tomatoes", Updated: testNow})

	if out := first.connect(t); out.Status != usecase.StatusCreated {
		t.Fatalf("first Connect = %+v, want created", out)
	}

	var rec transferRecorder

	opts := testOptions(d)
	opts.progress = rec.add
	second := newE2EDeviceWith(t, d, opts)

	got := map[string][]string{
		"Connect": rec.run(t, "Connect", func(ctx context.Context) error {
			_, err := second.sync.Connect(ctx)
			return err
		}),
		"PrepareRestore": rec.run(t, "PrepareRestore", func(ctx context.Context) error {
			_, err := second.sync.PrepareRestore(ctx)
			return err
		}),
		"CommitRestore": rec.run(t, "CommitRestore", second.sync.CommitRestore),
	}

	second.edit(domain.Note{ID: uuid.New(), Name: "Journal", Info: "day one", Updated: testNow})

	got["PrepareBackup"] = rec.run(t, "PrepareBackup", func(ctx context.Context) error {
		_, err := second.sync.PrepareBackup(ctx)
		return err
	})
	got["CommitBackup"] = rec.run(t, "CommitBackup", second.sync.CommitBackup)

	down, up := usecase.DirectionDownload, usecase.DirectionUpload
	want := map[string][]string{
		"Connect":        {down},
		"PrepareRestore": {down},
		"CommitRestore":  nil,
		"PrepareBackup":  {down},
		"CommitBackup":   {down, up},
	}

	for name, w := range want {
		if !slices.Equal(got[name], w) {
			t.Errorf("%s transfers %v, want %v", name, got[name], w)
		}
	}
}

// TestEndToEndFirstConnectTransfers: the first Connect to an empty storage
// writes the empty container, reads it for the snapshot and writes the first
// Backup: three transfers (spec §5.8).
func TestEndToEndFirstConnectTransfers(t *testing.T) {
	d := newFakeDrive(t)

	var rec transferRecorder

	opts := testOptions(d)
	opts.progress = rec.add
	dev := newE2EDeviceWith(t, d, opts)
	dev.edit(domain.Note{ID: uuid.New(), Name: "Garden", Info: "tomatoes", Updated: testNow})

	got := rec.run(t, "Connect", func(ctx context.Context) error {
		out, err := dev.sync.Connect(ctx)
		if err == nil && out.Status != usecase.StatusCreated {
			t.Errorf("Connect = %+v, want created", out)
		}

		return err
	})

	down, up := usecase.DirectionDownload, usecase.DirectionUpload
	if want := []string{up, down, up}; !slices.Equal(got, want) {
		t.Errorf("first Connect transfers %v, want %v", got, want)
	}

	if n := containerFiles(d); n != 1 {
		t.Errorf("container files = %d, want 1", n)
	}
}
