package yadisk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

const container = blobremote.ContainerName

var testNow = time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)

// harness is one emulated Disk with the stores, token source and pending
// store of one device.
type harness struct {
	t       *testing.T
	disk    *fakeDisk
	pending *fakePending
	tokens  usecase.CloudTokenSource

	mu      sync.Mutex
	sleeps  []time.Duration
	onSleep func() // called before each Sleep returns
	ids     int
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	return newHarnessWith(t, func(*usecase.CloudToken) {})
}

// newHarnessWith lets edit change the device's token before the store starts.
func newHarnessWith(t *testing.T, edit func(*usecase.CloudToken)) *harness {
	t.Helper()

	d := newFakeDisk(t, testNow)
	h := &harness{t: t, disk: d, pending: &fakePending{disk: d}}

	tok := usecase.CloudToken{
		Provider: fakeProvider, ClientID: fakeClientID, Access: "acc-1", Expiry: testNow.Add(time.Hour),
	}
	edit(&tok)

	h.tokens = usecase.NewMemoryTokenSource(tok, fakeClientID)

	return h
}

// store returns a new Store of the device, as after a restart.
func (h *harness) store() *Store {
	return NewStore(h.disk.endpoints(), h.tokens, h.pending, h.options())
}

func (h *harness) options() StoreOptions {
	return StoreOptions{
		HTTP:  h.disk.api.Client(),
		Href:  h.disk.href.Client(),
		Now:   func() time.Time { return testNow },
		Sleep: h.sleep,
		NewID: h.newID,

		allowLinkHost: func(string) bool { return true }, // the emulator is on 127.0.0.1
	}
}

// factory returns a factory of the device's stores over the emulator.
func (h *harness) factory() blobremote.StoreFactory {
	return newStoreFactory(h.disk.endpoints(), h.pending, h.options())
}

// fromFactory opens a store with f for the device's token.
func (h *harness) fromFactory(f blobremote.StoreFactory) blobremote.ObjectStore {
	h.t.Helper()

	s, err := f(usecase.RemoteTarget{Token: h.tokens})
	if err != nil {
		h.t.Fatalf("factory: %v", err)
	}

	return s
}

func (h *harness) sleep(ctx context.Context, d time.Duration) error {
	h.mu.Lock()
	h.sleeps = append(h.sleeps, d)
	hook := h.onSleep
	h.mu.Unlock()

	if hook != nil {
		hook()
	}

	return ctx.Err()
}

func (h *harness) newID() uuid.UUID {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.ids++

	var id uuid.UUID

	id[15] = byte(h.ids)

	return id
}

// tempName is the name of the temporary file of the n-th Write.
func tempName(n int) string {
	var id uuid.UUID

	id[15] = byte(n)

	return container + ".tmp-" + id.String()
}

func (h *harness) sleepLog() []time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()

	return slices.Clone(h.sleeps)
}

// mustRead reads the container and fails the test on an error.
func mustRead(t *testing.T, s *Store) (data []byte, version string, found bool) {
	t.Helper()

	data, version, found, err := s.Read(t.Context(), container)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	return data, version, found
}

// wantContainer checks the container's content on the Disk.
func wantContainer(t *testing.T, d *fakeDisk, want []byte) {
	t.Helper()

	f := d.file(container)
	if f == nil {
		t.Fatal("container is missing")
	}

	if !bytes.Equal(f.data, want) {
		t.Errorf("container = %q, want %q", f.data, want)
	}
}

func wantNoTemps(t *testing.T, d *fakeDisk) {
	t.Helper()

	if got := d.temps(); len(got) != 0 {
		t.Errorf("temporary files left: %v", got)
	}
}

// cloudError returns the CloudError in err and fails the test without one.
func cloudError(t *testing.T, err error) (ce CloudError) {
	t.Helper()

	ce, ok := errors.AsType[CloudError](err)
	if !ok {
		t.Fatalf("err = %v, want CloudError", err)
	}

	return ce
}

func wantCloudError(t *testing.T, err error) {
	t.Helper()

	_ = cloudError(t, err)
}

// readErr reads the container with s and returns only the error.
func readErr(t *testing.T, s *Store) error {
	t.Helper()

	_, _, _, err := s.Read(t.Context(), container) //nolint:dogsled // only the error matters here

	return err
}

// moveQueries returns the queries of the move requests.
func moveQueries(d *fakeDisk) []string {
	var out []string

	for _, r := range d.reqs() {
		if r.server == "api" && r.path == "/v1/disk/resources/move" {
			out = append(out, r.query.Encode())
		}
	}

	return out
}

func TestReadMissing(t *testing.T) {
	h := newHarness(t)

	data, version, found := mustRead(t, h.store())
	if found || data != nil || version != "" {
		t.Errorf("Read = %q, %q, %v; want nothing", data, version, found)
	}
}

func TestReadRoundTrip(t *testing.T) {
	h := newHarness(t)
	sum := h.disk.put(container, []byte("sealed container"), testNow)

	data, version, found := mustRead(t, h.store())
	if !found || string(data) != "sealed container" || version != sum {
		t.Errorf("Read = %q, %q, %v; want the container with version %s", data, version, found, sum)
	}
}

func TestReadSizeLimit(t *testing.T) {
	h := newHarness(t)
	h.disk.put(container, []byte("small"), testNow)
	h.disk.set(func(d *fakeDisk) { d.files["app:/"+container].size = 64<<20 + 1 })

	err := readErr(t, h.store())
	if !errors.Is(err, domain.ErrRemoteDamaged) {
		t.Fatalf("Read: err = %v, want ErrRemoteDamaged", err)
	}

	if n := h.disk.count("api", http.MethodGet, "/v1/disk/resources/download"); n != 0 {
		t.Errorf("download links requested %d times, want 0", n)
	}
}

func TestReadDownloadOverLimit(t *testing.T) {
	h := newHarness(t)
	data := make([]byte, 64<<20+1)
	h.disk.put(container, data, testNow)
	h.disk.set(func(d *fakeDisk) { d.files["app:/"+container].size = 10 })

	err := readErr(t, h.store())
	if !errors.Is(err, domain.ErrRemoteDamaged) {
		t.Fatalf("Read: err = %v, want ErrRemoteDamaged", err)
	}
}

func TestReadMD5Retries(t *testing.T) {
	tests := []struct {
		name string
		md5  string
	}{
		{name: "empty md5", md5: ""},
		{name: "md5 mismatch", md5: "0123456789abcdef0123456789abcdef"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.disk.put(container, []byte("sealed container"), testNow)
			h.disk.set(func(d *fakeDisk) { d.files["app:/"+container].md5 = tc.md5 })

			err := readErr(t, h.store())
			if !errors.Is(err, blobremote.ErrVersionMismatch) {
				t.Fatalf("Read: err = %v, want ErrVersionMismatch", err)
			}

			hrefGets := 0

			for _, r := range h.disk.reqs() {
				if r.server == "href" && r.method == http.MethodGet {
					hrefGets++
				}
			}

			if hrefGets != 2 {
				t.Errorf("downloaded %d times, want 2 (one repeat)", hrefGets)
			}

			if n := h.disk.count("api", http.MethodGet, "/v1/disk/resources/download"); n != 2 {
				t.Errorf("downloads = %d, want 2 (one repeat)", n)
			}
		})
	}
}

func TestWriteCreate(t *testing.T) {
	h := newHarness(t)
	s := h.store()

	if err := s.Write(t.Context(), container, []byte("first"), ""); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := moveQueries(h.disk); len(got) != 1 || !strings.Contains(got[0], "overwrite=false") {
		t.Errorf("move queries = %v, want one with overwrite=false", got)
	}

	wantContainer(t, h.disk, []byte("first"))
	wantNoTemps(t, h.disk)

	// Read right after Write sees the new object (Task 8 relies on it).
	data, version, found := mustRead(t, s)
	if !found || string(data) != "first" || version != md5Of([]byte("first")) {
		t.Errorf("Read after Write = %q, %q, %v", data, version, found)
	}
}

// TestWriteRecordsTempBeforeMove: the temporary file is recorded before its
// move is sent and forgotten after a definite outcome (spec §5.5, final
// review M3).
func TestWriteRecordsTempBeforeMove(t *testing.T) {
	tmp := tempName(1)
	saved := []string{"save " + tmp, "move app:/" + tmp}

	tests := []struct {
		name  string
		mode  func(d *fakeDisk)
		ok    bool
		after []string // events after the move
		kept  bool     // the record stays
	}{
		{name: "moved", mode: func(*fakeDisk) {}, ok: true, after: []string{"clear"}},
		{name: "moved after 202", mode: func(d *fakeDisk) { d.move202 = true }, ok: true, after: []string{"clear"}},
		{
			name: "operation failed", mode: func(d *fakeDisk) { d.move202, d.moveFail = true, true },
			after: []string{"DELETE app:/" + tmp + " permanently=true", "clear"},
		},
		{
			name: "rejected, temporary file not removed",
			mode: func(d *fakeDisk) {
				d.fail = map[string]fakeFailure{epMove: {http.StatusForbidden, "ForbiddenError"}}
				d.deleteFail = true
			},
			after: []string{"DELETE app:/" + tmp + " permanently=true"},
			kept:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			old := h.disk.put(container, []byte("old"), testNow)
			h.disk.set(tc.mode)

			err := h.store().Write(t.Context(), container, []byte("new"), old)
			if (err == nil) != tc.ok || errors.Is(err, blobremote.ErrOutcomeUnknown) {
				t.Fatalf("Write: err = %v, want success %v", err, tc.ok)
			}

			if got, want := h.disk.eventLog(), append(slices.Clone(saved), tc.after...); !slices.Equal(got, want) {
				t.Errorf("events = %v, want %v", got, want)
			}

			if name, found := h.pending.state(); found != tc.kept || tc.kept && name != tmp {
				t.Errorf("cloud_pending = %q, %v; want kept %v", name, found, tc.kept)
			}
		})
	}
}

// TestFenceAfterKilledMove: the process dies right after the move is sent.
// The record saved before the move makes the restarted app fence the
// temporary file before anything else, so the late move cannot apply.
func TestFenceAfterKilledMove(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	tmp := tempName(1)

	var (
		restartErr error
		firstReq   fakeRequest
	)

	// The first app is frozen while its move is on the way; the restarted
	// app, a fresh factory over the same local.db, starts meanwhile.
	h.disk.set(func(d *fakeDisk) {
		d.hold = func(ep string) {
			if ep != epMove {
				return
			}

			h.disk.set(func(d *fakeDisk) { d.hold, d.requests = nil, nil })

			_, _, _, restartErr = h.fromFactory(h.factory()).Read(context.Background(), container)
			if reqs := h.disk.reqs(); len(reqs) > 0 {
				firstReq = reqs[0]
			}
		}
	})

	if err := h.store().Write(t.Context(), container, []byte("lost"), old); err == nil {
		t.Error("Write succeeded, want the late move refused")
	}

	if restartErr != nil {
		t.Fatalf("Read after restart: %v", restartErr)
	}

	if firstReq.method != http.MethodDelete || firstReq.query.Get("path") != "app:/"+tmp {
		t.Errorf("first request after restart = %+v, want the DELETE of %s", firstReq, tmp)
	}

	wantContainer(t, h.disk, []byte("old"))
	wantNoTemps(t, h.disk)
}

func TestWriteReplace(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)

	if err := h.store().Write(t.Context(), container, []byte("new"), old); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := moveQueries(h.disk); len(got) != 1 || !strings.Contains(got[0], "overwrite=true") {
		t.Errorf("move queries = %v, want one with overwrite=true", got)
	}

	wantContainer(t, h.disk, []byte("new"))
	wantNoTemps(t, h.disk)
}

func TestWriteVersionMismatch(t *testing.T) {
	tests := []struct {
		name     string
		existing bool
		expect   string
		fail     map[string]fakeFailure
		moves    int
	}{
		{name: "other version", existing: true, expect: "ffffffffffffffffffffffffffffffff"},
		{name: "missing object", expect: "ffffffffffffffffffffffffffffffff"},
		{name: "object exists, none expected", existing: true},
		{
			name: "created by another device before the move",
			fail: map[string]fakeFailure{epMove: {http.StatusConflict, "DiskResourceAlreadyExistsError"}}, moves: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			if tc.existing {
				h.disk.put(container, []byte("old"), testNow)
			}

			h.disk.set(func(d *fakeDisk) { d.fail = tc.fail })

			err := h.store().Write(t.Context(), container, []byte("new"), tc.expect)
			if !errors.Is(err, blobremote.ErrVersionMismatch) {
				t.Fatalf("Write: err = %v, want ErrVersionMismatch", err)
			}

			if got := len(moveQueries(h.disk)); got != tc.moves {
				t.Errorf("moves = %d, want %d", got, tc.moves)
			}

			wantNoTemps(t, h.disk)

			if tc.existing {
				wantContainer(t, h.disk, []byte("old"))
			}
		})
	}
}

func TestUpload202WaitsForMD5(t *testing.T) {
	h := newHarness(t)
	h.disk.set(func(d *fakeDisk) { d.upload202, d.md5Delay = true, 2 })

	if err := h.store().Write(t.Context(), container, []byte("first"), ""); err != nil {
		t.Fatalf("Write: %v", err)
	}

	want := []time.Duration{500 * time.Millisecond, time.Second}
	if got := h.sleepLog(); !slices.Equal(got, want) {
		t.Errorf("sleeps = %v, want %v", got, want)
	}

	wantContainer(t, h.disk, []byte("first"))
}

func TestUploadMD5Mismatch(t *testing.T) {
	h := newHarness(t)
	h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.uploadMD5 = "0123456789abcdef0123456789abcdef" })

	err := h.store().Write(t.Context(), container, []byte("new"), md5Of([]byte("old")))
	wantCloudError(t, err)

	if len(moveQueries(h.disk)) != 0 {
		t.Error("move was sent after an md5 mismatch")
	}

	wantContainer(t, h.disk, []byte("old"))
	wantNoTemps(t, h.disk)
}

// uploadOverwrites returns the overwrite parameter of each upload link request.
func uploadOverwrites(d *fakeDisk) []string {
	var out []string

	for _, r := range d.reqs() {
		if r.server == "api" && r.path == "/v1/disk/resources/upload" {
			out = append(out, r.query.Get("overwrite"))
		}
	}

	return out
}

// An upload node that drops the connection costs one more upload with a new
// link, which may overwrite the Write's own temporary file.
func TestUploadRetriedOnceAfterNetworkError(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.dropPuts = 1 })

	if err := h.store().Write(t.Context(), container, []byte("new"), old); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got, want := uploadOverwrites(h.disk), []string{"false", "true"}; !slices.Equal(got, want) {
		t.Errorf("upload links overwrite = %v, want %v", got, want)
	}

	wantContainer(t, h.disk, []byte("new"))
	wantNoTemps(t, h.disk)
}

// The lost PUT stored the file all the same: the retry overwrites it.
func TestUploadRetryOverwritesStoredTemp(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.dropStored = 1 })

	if err := h.store().Write(t.Context(), container, []byte("new"), old); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := len(uploadOverwrites(h.disk)); got != 2 {
		t.Errorf("upload links = %d, want 2", got)
	}

	wantContainer(t, h.disk, []byte("new"))
	wantNoTemps(t, h.disk)
}

func TestLostUpload(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	network := CloudError{Code: codeNetwork, Step: stepUpload}

	tests := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{name: "no error", ctx: t.Context(), err: nil},
		{name: "network at upload", ctx: t.Context(), err: fmt.Errorf("upload: %w", network), want: true},
		{name: "operation over", ctx: canceled, err: network},
		{name: "network at upload link", ctx: t.Context(), err: CloudError{Code: codeNetwork, Step: stepUploadLink}},
		{name: "answer of the node", ctx: t.Context(), err: CloudError{Status: http.StatusInsufficientStorage, Code: codeOther, Step: stepUpload}},
		{name: "context error", ctx: t.Context(), err: context.DeadlineExceeded},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := lostUpload(tc.ctx, tc.err); got != tc.want {
				t.Errorf("lostUpload = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestUploadNotRetriedTwice(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.dropPuts = 2 })

	err := h.store().Write(t.Context(), container, []byte("new"), old)
	if ce := cloudError(t, err); ce.Code != codeNetwork || ce.Step != stepUpload {
		t.Errorf("error = %+v, want a network error of step upload", ce)
	}

	if got := len(uploadOverwrites(h.disk)); got != 2 {
		t.Errorf("upload links = %d, want 2", got)
	}

	if len(moveQueries(h.disk)) != 0 {
		t.Error("move was sent after a failed upload")
	}

	wantContainer(t, h.disk, []byte("old"))
	wantNoTemps(t, h.disk)
}

// An answer of the upload node is not a lost connection: no retry.
func TestUploadAnswerNotRetried(t *testing.T) {
	h := newHarness(t)
	h.disk.set(func(d *fakeDisk) { d.fail[epPut] = fakeFailure{status: http.StatusInternalServerError} })

	err := h.store().Write(t.Context(), container, []byte("new"), "")
	if ce := cloudError(t, err); ce.Status != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", ce.Status)
	}

	if got := len(uploadOverwrites(h.disk)); got != 1 {
		t.Errorf("upload links = %d, want 1", got)
	}
}

// When the operation ends during the upload, its own error is returned and
// nothing is retried.
func TestUploadNotRetriedAfterCancel(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(t.Context())

	defer cancel()

	h.disk.set(func(d *fakeDisk) { d.dropPuts, d.putHook = 1, cancel })

	err := h.store().Write(ctx, container, []byte("new"), "")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}

	if got := len(uploadOverwrites(h.disk)); got != 1 {
		t.Errorf("upload links = %d, want 1", got)
	}
}

func TestMove202PollSuccess(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.move202 = true })

	if err := h.store().Write(t.Context(), container, []byte("new"), old); err != nil {
		t.Fatalf("Write: %v", err)
	}

	polled := false

	for _, r := range h.disk.reqs() {
		if r.server == "api" && strings.HasPrefix(r.path, "/v1/disk/operations/") {
			polled = true

			if r.auth != "OAuth acc-1" {
				t.Errorf("operation poll Authorization = %q", r.auth)
			}
		}
	}

	if !polled {
		t.Error("the operation link from the move answer was not polled")
	}

	wantContainer(t, h.disk, []byte("new"))
	wantNoTemps(t, h.disk)
}

func TestMove202Failed(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.move202, d.moveFail = true, true })

	err := h.store().Write(t.Context(), container, []byte("new"), old)
	wantCloudError(t, err)

	if errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Errorf("err = %v, a failed operation is a known outcome", err)
	}

	wantContainer(t, h.disk, []byte("old"))
	wantNoTemps(t, h.disk)
}

// TestMoveServerErrorIsFenced: a 5xx answer to move may still be applied, so
// the temporary file is fenced like after a lost answer.
func TestMoveServerErrorIsFenced(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.fail = map[string]fakeFailure{epMove: {http.StatusGatewayTimeout, ""}} })

	err := h.store().Write(t.Context(), container, []byte("new"), old)
	if ce := cloudError(t, err); ce.Status != http.StatusGatewayTimeout {
		t.Errorf("CloudError = %+v, want the move's 504", ce)
	}

	if !slices.Contains(h.disk.eventLog(), "save "+tempName(1)) {
		t.Errorf("events = %v, want the temporary file recorded", h.disk.eventLog())
	}

	wantNoTemps(t, h.disk)
}

func TestFencePollLostApplied(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.move202, d.dropPollAfter = true, 1 })

	if err := h.store().Write(t.Context(), container, []byte("new"), old); err != nil {
		t.Fatalf("Write: %v", err)
	}

	tmp := tempName(1)
	want := []string{"save " + tmp, "move app:/" + tmp, "DELETE app:/" + tmp + " permanently=true", "clear"}

	if got := h.disk.eventLog(); !slices.Equal(got[:min(len(got), len(want))], want) {
		t.Errorf("events = %v, want %v first", got, want)
	}

	if _, found := h.pending.state(); found {
		t.Error("cloud_pending kept after a settled outcome")
	}

	wantContainer(t, h.disk, []byte("new"))
}

func TestFencePollLostNotApplied(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.deferMove, d.dropPollAfter = true, 1 })

	err := h.store().Write(t.Context(), container, []byte("new"), old)
	if ce := cloudError(t, err); ce.Code != codeNetwork {
		t.Errorf("CloudError = %+v, want the lost poll's network error", ce)
	}

	if errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Errorf("err = %v, the fence settled the outcome", err)
	}

	if _, found := h.pending.state(); found {
		t.Error("cloud_pending kept after a settled outcome")
	}

	wantContainer(t, h.disk, []byte("old"))
	wantNoTemps(t, h.disk)
}

func TestDeferredMoveCannotOverwrite(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.deferMove, d.dropPollAfter = true, 1 })

	if err := h.store().Write(t.Context(), container, []byte("first backup"), old); err == nil {
		t.Fatal("first Write succeeded, want the lost poll's error")
	}

	h.disk.set(func(d *fakeDisk) { d.deferMove, d.dropPollAfter = false, 0 })

	if err := h.store().Write(t.Context(), container, []byte("second backup"), old); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	h.disk.runDeferred()

	if got := h.disk.opStatuses(); !slices.Equal(got, []string{opStatusFailed}) {
		t.Errorf("operations = %v, want the deferred move failed", got)
	}

	wantContainer(t, h.disk, []byte("second backup"))
}

func TestFenceDeleteFails(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.deferMove, d.dropPollAfter, d.deleteFail = true, 1, true })

	err := h.store().Write(t.Context(), container, []byte("new"), old)
	if !errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
	}

	tmp := tempName(1)
	if name, found := h.pending.state(); !found || name != tmp {
		t.Errorf("cloud_pending = %q, %v; want %q kept", name, found, tmp)
	}

	events := h.disk.eventLog()

	save := slices.Index(events, "save "+tmp)
	del := slices.Index(events, "DELETE app:/"+tmp+" permanently=true")

	if save < 0 || del < 0 || save > del {
		t.Errorf("events = %v, want the record saved before the DELETE", events)
	}

	if slices.Contains(events, "clear") {
		t.Errorf("events = %v, the record was cleared", events)
	}
}

// TestFenceAfterCancel: the fence runs on its own deadline when the
// operation's context is already done.
func TestFenceAfterCancel(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.deferMove = true })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	h.onSleep = cancel

	err := h.store().Write(ctx, container, []byte("new"), old)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Write: err = %v, want context.Canceled", err)
	}

	if _, found := h.pending.state(); found {
		t.Error("cloud_pending kept after a settled outcome")
	}

	wantNoTemps(t, h.disk)
	wantContainer(t, h.disk, []byte("old"))
}

// TestMoveUnparsedAnswerIsFenced: a 2xx answer to move whose body cannot be
// read says the move was accepted, so its outcome is settled by the fence.
func TestMoveUnparsedAnswerIsFenced(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.moveBadBody = true })

	if err := h.store().Write(t.Context(), container, []byte("new"), old); err != nil {
		t.Fatalf("Write: %v, want the applied move found by the fence", err)
	}

	tmp := tempName(1)
	want := []string{"save " + tmp, "move app:/" + tmp, "DELETE app:/" + tmp + " permanently=true", "clear"}

	if got := h.disk.eventLog(); !slices.Equal(got[:min(len(got), len(want))], want) {
		t.Errorf("events = %v, want %v first", got, want)
	}

	wantContainer(t, h.disk, []byte("new"))
}

// TestFenceRetriedOnSameStore: after a failed fence in Write, the same Store
// fences again before its next request.
func TestFenceRetriedOnSameStore(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.deferMove, d.dropPollAfter, d.deleteFail = true, 1, true })

	s := h.store()

	if err := s.Write(t.Context(), container, []byte("new"), old); !errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
	}

	h.disk.set(func(d *fakeDisk) { d.deleteFail, d.requests = false, nil })

	if data, _, _ := mustRead(t, s); string(data) != "old" {
		t.Errorf("Read = %q", data)
	}

	reqs := h.disk.reqs()
	if len(reqs) == 0 || reqs[0].method != http.MethodDelete || reqs[0].query.Get("path") != "app:/"+tempName(1) {
		t.Fatalf("first request after the failed fence = %+v, want the DELETE of the temporary file", reqs)
	}

	if _, found := h.pending.state(); found {
		t.Error("cloud_pending kept after the second fence")
	}
}

// TestFenceRememberedWithoutRecord: when neither the pending record nor the
// DELETE succeeds, the stores of the same factory still fence the file.
func TestFenceRememberedWithoutRecord(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.deferMove, d.dropPollAfter, d.deleteFail = true, 1, true })
	h.pending.saveErr = errors.New("disk I/O error")

	f := h.factory()

	if err := h.fromFactory(f).Write(t.Context(), container, []byte("new"), old); !errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
	}

	if _, found := h.pending.state(); found {
		t.Fatal("the failing pending store kept a record")
	}

	h.disk.set(func(d *fakeDisk) { d.deleteFail, d.requests = false, nil })

	if _, _, _, err := h.fromFactory(f).Read(t.Context(), container); err != nil {
		t.Fatalf("Read: %v", err)
	}

	reqs := h.disk.reqs()
	if len(reqs) == 0 || reqs[0].method != http.MethodDelete || reqs[0].query.Get("path") != "app:/"+tempName(1) {
		t.Fatalf("first request of the next store = %+v, want the DELETE of the temporary file", reqs)
	}

	wantNoTemps(t, h.disk)

	// Settled: the next store does not fence again.
	h.disk.set(func(d *fakeDisk) { d.requests = nil })

	if _, _, _, err := h.fromFactory(f).Read(t.Context(), container); err != nil {
		t.Fatalf("third Read: %v", err)
	}

	for _, r := range h.disk.reqs() {
		if r.method == http.MethodDelete {
			t.Errorf("the settled file was fenced again: %v", r.query)
		}
	}
}

// TestFenceReauthRequired: a token refused (401) during the fence asks for a new
// sign-in and keeps the record for the fence after it.
func TestFenceReauthRequired(t *testing.T) {
	refuse := func(d *fakeDisk) { d.access = "acc-0" }

	t.Run("on restart", func(t *testing.T) {
		h := newHarness(t)
		tmp := tempName(9)

		h.disk.put(container, []byte("container"), testNow)
		h.disk.put(tmp, []byte("pending"), testNow)
		h.disk.set(refuse)

		h.pending.name, h.pending.found = tmp, true

		err := readErr(t, h.store())
		if !errors.Is(err, domain.ErrReauthRequired) || errors.Is(err, blobremote.ErrOutcomeUnknown) {
			t.Fatalf("Read: err = %v, want ErrReauthRequired", err)
		}

		if name, found := h.pending.state(); !found || name != tmp {
			t.Errorf("cloud_pending = %q, %v; want %q kept", name, found, tmp)
		}

		if n := h.disk.count("api", http.MethodGet, "/v1/disk/resources"); n != 0 {
			t.Errorf("container metadata read %d times before the fence", n)
		}
	})

	t.Run("inside Write", func(t *testing.T) {
		h := newHarness(t)
		old := h.disk.put(container, []byte("old"), testNow)
		h.disk.set(func(d *fakeDisk) { d.deferMove = true })
		// The token is revoked while the move is in progress.
		h.onSleep = func() { h.disk.set(refuse) }

		err := h.store().Write(t.Context(), container, []byte("new"), old)
		if !errors.Is(err, domain.ErrReauthRequired) || errors.Is(err, blobremote.ErrOutcomeUnknown) {
			t.Fatalf("Write: err = %v, want ErrReauthRequired", err)
		}

		if name, found := h.pending.state(); !found || name != tempName(1) {
			t.Errorf("cloud_pending = %q, %v; want %q kept", name, found, tempName(1))
		}
	})
}

func TestFenceOnRestart(t *testing.T) {
	tmp := tempName(9)

	tests := []struct {
		name      string
		tmpExists bool
		delete202 bool
	}{
		{name: "temporary file deleted", tmpExists: true},
		{name: "temporary file taken by the move", tmpExists: false},
		{name: "deletion accepted", tmpExists: true, delete202: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.disk.put(container, []byte("container"), testNow)

			if tc.tmpExists {
				h.disk.put(tmp, []byte("pending"), testNow)
			}

			h.disk.set(func(d *fakeDisk) { d.delete202 = tc.delete202 })
			h.pending.name, h.pending.found = tmp, true

			s := h.store()

			data, _, found := mustRead(t, s)
			if !found || string(data) != "container" {
				t.Errorf("Read = %q, %v", data, found)
			}

			reqs := h.disk.reqs()
			if len(reqs) < 2 || reqs[0].method != http.MethodDelete || reqs[0].query.Get("path") != "app:/"+tmp ||
				reqs[0].query.Get("permanently") != "true" {
				t.Fatalf("first request = %+v, want the DELETE of %s", reqs[0], tmp)
			}

			if _, found := h.pending.state(); found {
				t.Error("cloud_pending kept after the fence")
			}

			wantNoTemps(t, h.disk)

			// The fence runs once per Store.
			before := len(h.disk.eventLog())

			mustRead(t, s)

			if got := h.disk.eventLog()[before:]; len(got) != 0 {
				t.Errorf("second Read fenced again: %v", got)
			}
		})
	}

	t.Run("deletion fails again", func(t *testing.T) {
		h := newHarness(t)
		h.disk.put(container, []byte("container"), testNow)
		h.disk.put(tmp, []byte("pending"), testNow)
		h.disk.set(func(d *fakeDisk) { d.deleteFail = true })
		h.pending.name, h.pending.found = tmp, true

		s := h.store()

		if err := readErr(t, s); !errors.Is(err, blobremote.ErrOutcomeUnknown) {
			t.Fatalf("Read: err = %v, want ErrOutcomeUnknown", err)
		}

		if err := s.Write(t.Context(), container, []byte("new"), md5Of([]byte("container"))); !errors.Is(err, blobremote.ErrOutcomeUnknown) {
			t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
		}

		for _, r := range h.disk.reqs() {
			if r.method != http.MethodDelete {
				t.Errorf("request %s %s %v after a failed fence", r.method, r.path, r.query)
			}
		}

		if name, found := h.pending.state(); !found || name != tmp {
			t.Errorf("cloud_pending = %q, %v; want %q kept", name, found, tmp)
		}
	})
}

func TestPendingStoreErrorsAreLocal(t *testing.T) {
	h := newHarness(t)
	h.pending.loadErr = errors.New("disk I/O error")

	err := readErr(t, h.store())
	if _, ok := errors.AsType[usecase.LocalError](err); !ok {
		t.Fatalf("Read: err = %v, want LocalError", err)
	}

	if len(h.disk.reqs()) != 0 {
		t.Error("the Disk was reached without the pending record")
	}
}

func TestHrefWithoutAuthorization(t *testing.T) {
	h := newHarness(t)
	h.disk.set(func(d *fakeDisk) { d.downloadPrefix = "/redirect/" })
	s := h.store()

	if err := s.Write(t.Context(), container, []byte("first"), ""); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if data, _, _ := mustRead(t, s); string(data) != "first" {
		t.Errorf("Read = %q through the redirect", data)
	}

	paths := map[string]bool{}

	for _, r := range h.disk.reqs() {
		if r.server == "href" {
			paths[strings.SplitAfter(r.path, "/")[1]] = true

			if r.auth != "" {
				t.Errorf("%s %s carried Authorization", r.method, r.path)
			}
		}
	}

	for _, p := range []string{"upload/", "redirect/", "download/"} {
		if !paths[p] {
			t.Errorf("no href request to /%s; got %v", p, paths)
		}
	}
}

func TestHrefRedirectToHTTPRejected(t *testing.T) {
	tests := []struct {
		name string
		set  func(d *fakeDisk)
	}{
		{name: "redirect to http", set: func(d *fakeDisk) { d.downloadPrefix = "/redirect-http/" }},
		{name: "http link", set: func(d *fakeDisk) { d.downloadBase = d.plain.URL }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.disk.put(container, []byte("container"), testNow)
			h.disk.set(tc.set)

			err := readErr(t, h.store())
			if !errors.Is(err, errInsecureLink) {
				t.Fatalf("Read: err = %v, want errInsecureLink", err)
			}

			wantCloudError(t, err)

			if n := len(slices.DeleteFunc(h.disk.reqs(), func(r fakeRequest) bool { return r.server != "plain" })); n != 0 {
				t.Errorf("the plain-http server got %d requests", n)
			}
		})
	}
}

func TestAPIRedirectNotFollowed(t *testing.T) {
	h := newHarness(t)
	h.disk.set(func(d *fakeDisk) { d.apiRedirect = true })

	err := readErr(t, h.store())
	if ce := cloudError(t, err); ce.Status != http.StatusFound {
		t.Errorf("CloudError = %+v, want the 302", ce)
	}

	if n := h.disk.count("href", http.MethodGet, "/probe"); n != 0 {
		t.Errorf("the redirect was followed %d times", n)
	}
}

func TestOperationLinkOutsideAPI(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.move202, d.opBase = true, d.href.URL })

	// The poll is refused, so the fence settles the applied move.
	if err := h.store().Write(t.Context(), container, []byte("new"), old); err != nil {
		t.Fatalf("Write: %v", err)
	}

	for _, r := range h.disk.reqs() {
		if r.server == "href" && strings.HasPrefix(r.path, "/v1/disk/operations/") {
			t.Errorf("operation link outside the API host was requested (Authorization %q)", r.auth)
		}
	}

	wantContainer(t, h.disk, []byte("new"))
}

func TestCleanupOldTemps(t *testing.T) {
	h := newHarness(t)
	young := container + ".tmp-00000000-0000-0000-0000-0000000000a1"
	old := container + ".tmp-00000000-0000-0000-0000-0000000000a2"

	h.disk.put(young, []byte("young"), testNow.Add(-30*time.Minute))
	h.disk.put(old, []byte("old"), testNow.Add(-2*time.Hour))
	h.disk.put("notes.txt", []byte("other"), testNow.Add(-5*time.Hour))

	if err := h.store().Write(t.Context(), container, []byte("first"), ""); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got, want := h.disk.names(), []string{young, "notes.txt", container}; !slices.Equal(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}

	if !slices.Contains(h.disk.eventLog(), "DELETE app:/"+old+" permanently=true") {
		t.Errorf("events = %v, want a permanent DELETE of %s", h.disk.eventLog(), old)
	}
}

func TestCleanupPaginates(t *testing.T) {
	seed := func(h *harness, young, old, others int) {
		for i := range young {
			h.disk.put(fmt.Sprintf("%s.tmp-00000000-0000-0000-0001-%012d", container, i), nil, testNow.Add(-time.Minute))
		}

		for i := range old {
			h.disk.put(fmt.Sprintf("%s.tmp-00000000-0000-0000-0002-%012d", container, i), nil, testNow.Add(-2*time.Hour))
		}

		for i := range others {
			h.disk.put(fmt.Sprintf("other-%d", i), nil, testNow.Add(-2*time.Hour))
		}
	}

	countOld := func(h *harness) int {
		n := 0

		for _, name := range h.disk.temps() {
			if strings.Contains(name, "-0002-") {
				n++
			}
		}

		return n
	}

	t.Run("old temps on the second page", func(t *testing.T) {
		h := newHarness(t)
		seed(h, 100, 5, 45)

		if err := h.store().Write(t.Context(), container, []byte("first"), ""); err != nil {
			t.Fatalf("Write: %v", err)
		}

		if n := countOld(h); n != 0 {
			t.Errorf("%d old temps left", n)
		}

		if n := len(h.disk.temps()); n != 100 {
			t.Errorf("%d temps left, want the 100 young ones", n)
		}
	})

	t.Run("at most 20 deletions per Write", func(t *testing.T) {
		h := newHarness(t)
		seed(h, 100, 30, 20)

		if err := h.store().Write(t.Context(), container, []byte("first"), ""); err != nil {
			t.Fatalf("Write: %v", err)
		}

		if n := countOld(h); n != 10 {
			t.Errorf("%d old temps left after the first Write, want 10", n)
		}

		if err := h.store().Write(t.Context(), container, []byte("second"), md5Of([]byte("first"))); err != nil {
			t.Fatalf("second Write: %v", err)
		}

		if n := countOld(h); n != 0 {
			t.Errorf("%d old temps left after the second Write", n)
		}
	})

	t.Run("at most 10 pages per Write", func(t *testing.T) {
		h := newHarness(t)
		seed(h, 1100, 1, 0)

		if err := h.store().Write(t.Context(), container, []byte("first"), ""); err != nil {
			t.Fatalf("Write: %v", err)
		}

		lists := 0

		for _, r := range h.disk.reqs() {
			if r.path == "/v1/disk/resources" && r.query.Get("path") == "app:/" {
				lists++

				if r.query.Get("limit") != "100" {
					t.Errorf("listing limit = %q", r.query.Get("limit"))
				}
			}
		}

		if lists != 10 {
			t.Errorf("listing pages = %d, want 10", lists)
		}

		if n := countOld(h); n != 1 {
			t.Errorf("the old temp past page 10 was deleted")
		}
	})
}

func TestExpiredTokenReauth(t *testing.T) {
	tests := []struct {
		name   string
		expiry time.Time
	}{
		{name: "passed", expiry: testNow.Add(-time.Second)},
		{name: "exactly now", expiry: testNow},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarnessWith(t, func(tok *usecase.CloudToken) { tok.Expiry = tc.expiry })
			h.disk.put(container, []byte("container"), testNow)

			if err := readErr(t, h.store()); !errors.Is(err, domain.ErrReauthRequired) {
				t.Fatalf("Read: err = %v, want ErrReauthRequired", err)
			}

			if n := len(h.disk.reqs()); n != 0 {
				t.Errorf("requests = %d, want none", n)
			}
		})
	}
}

// TestSoonExpiringTokenUsed: a token that has not passed its expiry is used
// as it is; nothing is refreshed.
func TestSoonExpiringTokenUsed(t *testing.T) {
	h := newHarnessWith(t, func(tok *usecase.CloudToken) { tok.Expiry = testNow.Add(30 * time.Second) })
	h.disk.put(container, []byte("container"), testNow)

	if data, _, _ := mustRead(t, h.store()); string(data) != "container" {
		t.Errorf("Read = %q", data)
	}

	if n := len(h.disk.forms()); n != 0 {
		t.Errorf("token requests = %d, want none", n)
	}
}

func TestUnauthorizedReauth(t *testing.T) {
	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)
	h.disk.set(func(d *fakeDisk) { d.access = "acc-0" })

	s := h.store()

	if err := s.Write(t.Context(), container, []byte("new"), old); !errors.Is(err, domain.ErrReauthRequired) {
		t.Fatalf("Write: err = %v, want ErrReauthRequired", err)
	}

	if n := len(h.disk.forms()); n != 0 {
		t.Errorf("token requests = %d, want none", n)
	}

	if n := len(h.disk.reqs()); n != 1 {
		t.Errorf("requests = %d, want 1 (no retry)", n)
	}
}

// describeToken prints the fields of a test token; CloudToken itself is redacted.
func describeToken(t usecase.CloudToken) string {
	return fmt.Sprintf("{%s %s %s %s %s %v}", t.Provider, t.ClientID, t.Access, t.Refresh, t.Account, t.Expiry)
}

func TestErrorsMapped(t *testing.T) {
	tests := []struct {
		name   string
		write  bool
		ep     string
		fail   fakeFailure
		status int
		code   string
	}{
		{name: "429", ep: epMeta, fail: fakeFailure{429, "TooManyRequestsError"}, status: 429, code: "TooManyRequestsError"},
		{name: "500", ep: epMeta, fail: fakeFailure{500, "DiskInternalError"}, status: 500, code: "DiskInternalError"},
		{name: "unknown code", ep: epDownload, fail: fakeFailure{503, "SomethingNewError"}, status: 503, code: codeOther},
		{name: "423", ep: epMeta, fail: fakeFailure{423, "LockedError"}, status: 423, code: "LockedError"},
		{
			name: "507", write: true, ep: epUpload,
			fail: fakeFailure{507, "DiskStorageQuotaExhaustedError"}, status: 507, code: "DiskStorageQuotaExhaustedError",
		},
		{name: "413", write: true, ep: epPut, fail: fakeFailure{413, ""}, status: 413, code: codeOther},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.disk.put(container, []byte("container"), testNow)
			h.disk.set(func(d *fakeDisk) { d.fail = map[string]fakeFailure{tc.ep: tc.fail} })

			var err error
			if tc.write {
				err = h.store().Write(t.Context(), container, []byte("new"), md5Of([]byte("container")))
			} else {
				err = readErr(t, h.store())
			}

			ce := cloudError(t, err)
			if ce.Status != tc.status || ce.Code != tc.code {
				t.Errorf("CloudError = %d %s, want %d %s", ce.Status, ce.Code, tc.status, tc.code)
			}

			wantSafeText(t, h.disk, err)
		})
	}
}

// wantSafeText checks that err's text has no description, URL, path or host.
func wantSafeText(t *testing.T, d *fakeDisk, err error) {
	t.Helper()

	text := err.Error()
	for _, leak := range []string{fakeDescription, "https://", "app:/", "/v1/disk", strings.TrimPrefix(d.api.URL, "https://")} {
		if strings.Contains(text, leak) {
			t.Errorf("error text %q contains %q", text, leak)
		}
	}
}

func TestNetworkError(t *testing.T) {
	h := newHarness(t)
	h.disk.api.Close()

	err := readErr(t, h.store())
	if ce := cloudError(t, err); ce.Status != 0 || ce.Code != codeNetwork || ce.Step != "stat" || ce.Net != "refused" {
		t.Errorf("CloudError = %+v, want 0 network at stat, refused", ce)
	}

	cause := errors.Unwrap(cloudError(t, err))
	if cause == nil {
		t.Fatal("network error lost its cause")
	}

	// The cause is reachable through Unwrap, so it must not quote the URL.
	for _, leak := range []string{"/v1/disk", "app%3A", "path="} {
		if strings.Contains(cause.Error(), leak) {
			t.Errorf("cause %q contains %q", cause, leak)
		}
	}

	wantSafeText(t, h.disk, err)
}

func TestStoreFactory(t *testing.T) {
	d := newFakeDisk(t, testNow)
	pending := &fakePending{disk: d}
	tokens := usecase.NewMemoryTokenSource(usecase.CloudToken{ClientID: "builtin"}, "builtin")

	tests := []struct {
		name    string
		target  usecase.RemoteTarget
		pending PendingStore
		wantErr error
	}{
		{name: "no token", target: usecase.RemoteTarget{}, pending: pending, wantErr: domain.ErrReauthRequired},
		{name: "with token", target: usecase.RemoteTarget{Token: tokens}, pending: pending},
		{name: "no pending store", target: usecase.RemoteTarget{Token: tokens}, wantErr: errNoPendingStore},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewStoreFactory(DefaultEndpoints, tc.pending)(tc.target)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("factory: %v", err)
			}

			if _, ok := got.(*Store); !ok {
				t.Fatalf("store = %T, want *Store", got)
			}
		})
	}
}

func TestDefaultEndpoints(t *testing.T) {
	want := Endpoints{API: "https://cloud-api.yandex.net", OAuth: "https://oauth.yandex.ru"}
	if DefaultEndpoints != want {
		t.Errorf("DefaultEndpoints = %+v, want %+v", DefaultEndpoints, want)
	}
}

// progressLog collects the progress reports of a store; the transport may
// report from its own goroutine.
type progressLog struct {
	mu  sync.Mutex
	got []usecase.TransferProgress
}

func (l *progressLog) add(p usecase.TransferProgress) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.got = append(l.got, p)
}

func (l *progressLog) list() []usecase.TransferProgress {
	l.mu.Lock()
	defer l.mu.Unlock()

	return slices.Clone(l.got)
}

// tickingNow is a clock that moves 200 ms on every call, so that the
// throttle lets every report of a transfer pass.
func tickingNow() func() time.Time {
	var ticks atomic.Int64

	return func() time.Time { return testNow.Add(time.Duration(ticks.Add(1)) * 200 * time.Millisecond) }
}

// wantTransfer checks that reports are one whole transfer of total bytes in
// direction: from 0, never back, up to total.
func wantTransfer(t *testing.T, reports []usecase.TransferProgress, direction string, total int64, minReports int) {
	t.Helper()

	if len(reports) < minReports {
		t.Fatalf("%d reports, want at least %d: %+v", len(reports), minReports, reports)
	}

	want := usecase.TransferProgress{Storage: domain.KindYandexDisk, Direction: direction, Unit: usecase.UnitBytes, Total: total}

	var last int64

	for i, p := range reports {
		if p.Storage != want.Storage || p.Direction != want.Direction || p.Unit != want.Unit || p.Total != want.Total {
			t.Fatalf("report %d = %+v, want %+v", i, p, want)
		}

		if p.Done < last {
			t.Fatalf("report %d goes back: %d after %d", i, p.Done, last)
		}

		last = p.Done
	}

	if reports[0].Done != 0 || last != total {
		t.Errorf("reports go from %d to %d, want 0 to %d", reports[0].Done, last, total)
	}
}

func TestWriteReportsUploadProgress(t *testing.T) {
	h := newHarness(t)
	data := bytes.Repeat([]byte("u"), 300_000)

	var log progressLog

	opts := h.options()
	opts.Progress, opts.Now = log.add, tickingNow()

	if err := NewStore(h.disk.endpoints(), h.tokens, h.pending, opts).Write(t.Context(), container, data, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}

	wantTransfer(t, log.list(), usecase.DirectionUpload, int64(len(data)), 3)
	wantContainer(t, h.disk, data)
}

func TestReadReportsDownloadProgress(t *testing.T) {
	h := newHarness(t)
	data := bytes.Repeat([]byte("d"), 300_000)
	h.disk.put(container, data, testNow)

	var log progressLog

	opts := h.options()
	opts.Progress, opts.Now = log.add, tickingNow()

	got, _, _ := mustRead(t, NewStore(h.disk.endpoints(), h.tokens, h.pending, opts))
	if !bytes.Equal(got, data) {
		t.Fatal("Read returned other bytes")
	}

	wantTransfer(t, log.list(), usecase.DirectionDownload, int64(len(data)), 3)
}

func TestProgressThrottled(t *testing.T) {
	h := newHarness(t)
	data := bytes.Repeat([]byte("t"), 300_000)

	var log progressLog

	opts := h.options() // the clock stands still: only the first and the last report pass
	opts.Progress = log.add

	if err := NewStore(h.disk.endpoints(), h.tokens, h.pending, opts).Write(t.Context(), container, data, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := log.list()
	wantTransfer(t, got, usecase.DirectionUpload, int64(len(data)), 2)

	if len(got) != 2 {
		t.Errorf("%d reports in one instant, want 2 (the first and the last)", len(got))
	}
}

func TestFactoryWithProgress(t *testing.T) {
	h := newHarness(t)
	h.disk.put(container, []byte("container"), testNow)

	var log progressLog

	s := h.fromFactory(NewStoreFactory(h.disk.endpoints(), h.pending, WithProgress(log.add), withClients(h.options())))
	if _, _, _, err := s.Read(t.Context(), container); err != nil {
		t.Fatalf("Read: %v", err)
	}

	wantTransfer(t, log.list(), usecase.DirectionDownload, int64(len("container")), 2)
}

// idleCounter is a transport that counts CloseIdleConnections.
type idleCounter struct {
	http.RoundTripper

	closed atomic.Int32
}

func (c *idleCounter) CloseIdleConnections() { c.closed.Add(1) }

func TestStoreCloseClosesIdleConnections(t *testing.T) {
	h := newHarness(t)
	api := &idleCounter{RoundTripper: h.disk.api.Client().Transport}
	href := &idleCounter{RoundTripper: h.disk.href.Client().Transport}

	opts := h.options()
	opts.HTTP, opts.Href = &http.Client{Transport: api}, &http.Client{Transport: href}

	s := NewStore(h.disk.endpoints(), h.tokens, h.pending, opts)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if api.closed.Load() == 0 || href.closed.Load() == 0 {
		t.Errorf("CloseIdleConnections: api %d, href %d times, want both", api.closed.Load(), href.closed.Load())
	}
}

func TestErrorSteps(t *testing.T) {
	old := []byte("container")

	tests := []struct {
		name  string
		write bool
		edit  func(d *fakeDisk)
		step  string
	}{
		{name: "metadata", edit: func(d *fakeDisk) { d.fail[epMeta] = fakeFailure{500, "DiskInternalError"} }, step: "stat"},
		{name: "download link", edit: func(d *fakeDisk) { d.fail[epDownload] = fakeFailure{500, ""} }, step: "download_link"},
		{name: "download", edit: func(d *fakeDisk) { d.fail[epGet] = fakeFailure{500, ""} }, step: "download"},
		{name: "upload link", write: true, edit: func(d *fakeDisk) { d.fail[epUpload] = fakeFailure{500, ""} }, step: "upload_link"},
		{name: "upload", write: true, edit: func(d *fakeDisk) { d.fail[epPut] = fakeFailure{500, ""} }, step: "upload"},
		{name: "md5 of the upload", write: true, edit: func(d *fakeDisk) { d.uploadMD5 = md5Of([]byte("other")) }, step: "wait_md5"},
		{name: "move", write: true, edit: func(d *fakeDisk) { d.fail[epMove] = fakeFailure{403, "ForbiddenError"} }, step: "move"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.disk.put(container, old, testNow)
			h.disk.set(tc.edit)

			var err error
			if tc.write {
				err = h.store().Write(t.Context(), container, []byte("new"), md5Of(old))
			} else {
				err = readErr(t, h.store())
			}

			if ce := cloudError(t, err); ce.Step != tc.step || ce.Net != "" {
				t.Errorf("CloudError = %+v, want step %s without net", ce, tc.step)
			}
		})
	}
}

// TestLoggedSteps checks the steps of the failures that the store only logs:
// the deletion of its own temporary file, the fence and the cleanup.
func TestLoggedSteps(t *testing.T) {
	old := []byte("container")

	tests := []struct {
		name string
		edit func(d *fakeDisk)
		want string
	}{
		{
			name: "delete after a version mismatch",
			edit: func(d *fakeDisk) { d.deleteFail = true },
			want: "at delete",
		},
		{
			name: "fence after a failed move",
			edit: func(d *fakeDisk) { d.deleteFail, d.fail[epMove] = true, fakeFailure{500, "DiskInternalError"} },
			want: "at fence",
		},
		{
			name: "cleanup",
			edit: func(d *fakeDisk) { d.fail[epList] = fakeFailure{500, "DiskInternalError"} },
			want: "at cleanup",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.disk.put(container, old, testNow)
			h.disk.set(tc.edit)

			var buf bytes.Buffer

			opts := h.options()
			opts.Log = slog.New(slog.NewTextHandler(&buf, nil))

			expect := md5Of(old)
			if tc.name == "delete after a version mismatch" {
				expect = md5Of([]byte("stale"))
			}

			_ = NewStore(h.disk.endpoints(), h.tokens, h.pending, opts).Write(t.Context(), container, []byte("new"), expect) // the log is checked

			if !strings.Contains(buf.String(), tc.want) {
				t.Errorf("log %q lacks %q", buf.String(), tc.want)
			}
		})
	}
}

// withClients makes a factory's stores use the emulator's clients and the
// test's seams of o.
func withClients(o StoreOptions) FactoryOption {
	return func(opts *StoreOptions) {
		opts.HTTP, opts.Href, opts.Now, opts.Sleep, opts.NewID = o.HTTP, o.Href, o.Now, o.Sleep, o.NewID
		opts.allowLinkHost = o.allowLinkHost
	}
}

// recordingClient wraps c so that the hosts of all its requests are kept.
type recordingClient struct {
	*http.Client

	mu    sync.Mutex
	hosts []string
}

func recordHosts(c *http.Client) *recordingClient {
	rc := &recordingClient{}
	rc.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		rc.mu.Lock()
		rc.hosts = append(rc.hosts, r.URL.Host)
		rc.mu.Unlock()

		return c.Transport.RoundTrip(r)
	})}

	return rc
}

func (rc *recordingClient) seen() []string {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	return slices.Clone(rc.hosts)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLinkHostsRefusedBeforeSending(t *testing.T) {
	foreign := []string{
		"https://evilyandex.net", "https://yandex.net.evil.com", "https://yandex.com", "http://x.yandex.ru",
	}

	for _, base := range foreign {
		for _, op := range []string{"download", "upload"} {
			t.Run(op+" "+base, func(t *testing.T) {
				h := newHarness(t)
				h.disk.put(container, []byte("container"), testNow)
				h.disk.set(func(d *fakeDisk) { d.downloadBase, d.uploadBase = base, base })

				opts := h.options()
				opts.allowLinkHost = nil // the production rule
				rec := recordHosts(h.disk.href.Client())
				opts.Href = rec.Client
				s := NewStore(h.disk.endpoints(), h.tokens, h.pending, opts)

				var err error
				if op == "download" {
					err = readErr(t, s)
				} else {
					err = s.Write(t.Context(), container, []byte("x"), "")
				}

				if !errors.Is(err, errInsecureLink) {
					t.Fatalf("%s: err = %v, want errInsecureLink", op, err)
				}

				wantCloudError(t, err)

				if strings.Contains(err.Error(), "evil") || strings.Contains(err.Error(), "x.yandex.ru") {
					t.Errorf("the error carries the link: %v", err)
				}

				if got := rec.seen(); len(got) != 0 {
					t.Errorf("requests left for %v", got)
				}
			})
		}
	}
}

func TestLinkHostDefaultRefusesLoopback(t *testing.T) {
	h := newHarness(t)
	h.disk.put(container, []byte("container"), testNow)

	opts := h.options()
	opts.allowLinkHost = nil

	err := readErr(t, NewStore(h.disk.endpoints(), h.tokens, h.pending, opts))
	if !errors.Is(err, errInsecureLink) {
		t.Fatalf("Read: err = %v, want errInsecureLink", err)
	}

	if n := len(slices.DeleteFunc(h.disk.reqs(), func(r fakeRequest) bool { return r.server != "href" })); n != 0 {
		t.Errorf("the href server got %d requests", n)
	}
}

func TestRedirectToForeignHostRefused(t *testing.T) {
	h := newHarness(t)
	h.disk.put(container, []byte("container"), testNow)
	h.disk.set(func(d *fakeDisk) {
		d.downloadPrefix = "/redirect/"
		d.redirectTo = "https://localhost:1/stolen"
	})

	opts := h.options()
	opts.allowLinkHost = func(host string) bool { return host == "127.0.0.1" } // the first hop only
	rec := recordHosts(h.disk.href.Client())
	opts.Href = rec.Client

	err := readErr(t, NewStore(h.disk.endpoints(), h.tokens, h.pending, opts))
	if !errors.Is(err, errInsecureLink) {
		t.Fatalf("Read: err = %v, want errInsecureLink", err)
	}

	if strings.Contains(err.Error(), "localhost") || strings.Contains(err.Error(), "stolen") {
		t.Errorf("the error carries the link: %v", err)
	}

	for _, host := range rec.seen() {
		if strings.HasPrefix(host, "localhost") {
			t.Errorf("a request went to the foreign host %s", host)
		}
	}
}

func TestUploadProgressAfterRedirect307(t *testing.T) {
	h := newHarness(t)
	h.disk.set(func(d *fakeDisk) { d.uploadPrefix = "/upload-307/" })

	data := bytes.Repeat([]byte("u"), 300_000)

	var log progressLog

	opts := h.options()
	opts.Progress, opts.Now = log.add, tickingNow()

	if err := NewStore(h.disk.endpoints(), h.tokens, h.pending, opts).Write(t.Context(), container, data, ""); err != nil {
		t.Fatalf("Write: %v", err)
	}

	wantContainer(t, h.disk, data)

	redirected := false

	for _, r := range h.disk.reqs() {
		redirected = redirected || (r.server == "href" && strings.HasPrefix(r.path, "/upload-307/"))
	}

	if !redirected {
		t.Fatal("the upload was not redirected")
	}

	reports := log.list()
	if len(reports) == 0 {
		t.Fatal("no reports")
	}

	for i, p := range reports {
		if p.Total != int64(len(data)) || p.Done > p.Total {
			t.Fatalf("report %d = %+v, want Total %d and Done <= Total", i, p, len(data))
		}
	}

	if last := reports[len(reports)-1]; last.Done != int64(len(data)) {
		t.Errorf("last report = %+v, want Done = Total", last)
	}
}

// moveInFlight runs a Write whose operation context ends (by deadline, or
// by cancel when withDeadline is false) while the move request is in flight.
// applied tells whether the Disk applies the move anyway.
func moveInFlight(t *testing.T, applied, withDeadline bool) (*harness, error) {
	t.Helper()

	h := newHarness(t)
	old := h.disk.put(container, []byte("old"), testNow)

	ctx, cancel := context.WithCancel(t.Context())
	if withDeadline {
		ctx, cancel = context.WithTimeout(t.Context(), 100*time.Millisecond)
	}

	defer cancel()

	h.disk.set(func(d *fakeDisk) {
		d.hold = func(ep string) {
			if ep != epMove {
				return
			}

			h.disk.set(func(d *fakeDisk) {
				d.hold = nil
				if applied {
					d.moveFile("app:/"+tempName(1), "app:/"+container)
				} else {
					d.fail = map[string]fakeFailure{epMove: {http.StatusInternalServerError, ""}}
				}
			})

			if withDeadline {
				<-ctx.Done()
			} else {
				cancel()
			}

			time.Sleep(50 * time.Millisecond) // the client has given up by now
		}
	})

	return h, h.store().Write(ctx, container, []byte("new"), old)
}

// TestMoveContextEndsAppliedIsFenced: the context ends during the move and
// the Disk applied it; the fence finds the new md5 and the Write succeeds.
func TestMoveContextEndsAppliedIsFenced(t *testing.T) {
	for _, withDeadline := range []bool{false, true} {
		h, err := moveInFlight(t, true, withDeadline)
		if err != nil {
			t.Fatalf("deadline=%v: Write: %v", withDeadline, err)
		}

		if _, found := h.pending.state(); found {
			t.Error("cloud_pending kept after a settled outcome")
		}

		wantContainer(t, h.disk, []byte("new"))
		wantNoTemps(t, h.disk)
	}
}

// TestMoveContextEndsNotAppliedIsFenced: the move was not applied; the fence
// removes the temporary file and the error stays the context's.
func TestMoveContextEndsNotAppliedIsFenced(t *testing.T) {
	for _, tc := range []struct {
		withDeadline bool
		want         error
	}{{true, context.DeadlineExceeded}, {false, context.Canceled}} {
		h, err := moveInFlight(t, false, tc.withDeadline)
		if !errors.Is(err, tc.want) {
			t.Errorf("deadline=%v: Write err = %v, want %v", tc.withDeadline, err, tc.want)
		}

		if !slices.ContainsFunc(h.disk.eventLog(), func(e string) bool { return strings.HasPrefix(e, "DELETE app:/"+tempName(1)) }) {
			t.Errorf("events = %v, want the fence's DELETE of the temporary file", h.disk.eventLog())
		}

		if _, found := h.pending.state(); found {
			t.Error("cloud_pending kept after a settled outcome")
		}

		wantContainer(t, h.disk, []byte("old"))
		wantNoTemps(t, h.disk)
	}
}
