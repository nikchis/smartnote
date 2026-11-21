package gdrive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// memUploads is the UploadStore of a device: the record of its unfinished
// upload, kept across the stores of its operations as local.db keeps it.
type memUploads struct {
	mu      sync.Mutex
	data    []byte
	saveErr error
	saves   int
	onSave  func() // runs before the record is saved
}

func (m *memUploads) LoadCloudUpload(_ context.Context, p domain.StorageKind) (data []byte, found bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p != domain.KindGoogleDrive {
		return nil, false, errors.New("upload record of another provider")
	}

	if m.data == nil {
		return nil, false, nil
	}

	return append([]byte(nil), m.data...), true, nil
}

func (m *memUploads) SaveCloudUpload(_ context.Context, p domain.StorageKind, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.onSave != nil {
		m.onSave()
	}

	m.saves++

	if m.saveErr != nil {
		return m.saveErr
	}

	if p != domain.KindGoogleDrive {
		return errors.New("upload record of another provider")
	}

	m.data = append([]byte(nil), data...)

	return nil
}

func (m *memUploads) ClearCloudUpload(_ context.Context, p domain.StorageKind) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if p != domain.KindGoogleDrive {
		return errors.New("upload record of another provider")
	}

	m.data = nil

	return nil
}

// record returns the kept record and whether there is one.
func (m *memUploads) record(t *testing.T) (pendingUpload, bool) {
	t.Helper()

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.data == nil {
		return pendingUpload{}, false
	}

	var rec pendingUpload
	if err := json.Unmarshal(m.data, &rec); err != nil {
		t.Fatalf("record is not JSON: %v", err)
	}

	return rec, true
}

// set keeps rec as the record, as a store that crashed during its upload
// left it.
func (m *memUploads) set(t *testing.T, rec pendingUpload) {
	t.Helper()

	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = data
}

// wantNoRecord fails if a record is kept.
func wantNoRecord(t *testing.T, up *memUploads) {
	t.Helper()

	if _, ok := up.record(t); ok {
		t.Error("the upload record was kept")
	}
}

// wantRecord fails unless a record of the size is kept.
func wantRecord(t *testing.T, up *memUploads, size int64) pendingUpload {
	t.Helper()

	rec, ok := up.record(t)
	if !ok {
		t.Fatal("the upload record was removed")
	}

	if rec.Size != size || rec.SessionURI == "" {
		t.Errorf("record = op %q size %d, want size %d with a session", rec.Op, rec.Size, size)
	}

	return rec
}

// uploadStore is a store over d of a device whose upload record is up.
func uploadStore(t *testing.T, d *fakeDrive, up *memUploads) *Store {
	t.Helper()

	return newStore(d.endpoints(), testTokens(), fakeClient, testRefresher(t, d), up, testOptions(d))
}

// writeErr writes the container with s and returns the error.
func writeErr(t *testing.T, s *Store, data []byte, expect string) error {
	t.Helper()

	return s.Write(t.Context(), container, data, expect)
}

// mustWrite writes the container and fails the test on an error.
func mustWrite(t *testing.T, s *Store, data []byte, expect string) {
	t.Helper()

	if err := writeErr(t, s, data, expect); err != nil {
		t.Fatalf("Write(expect %q): %v", expect, err)
	}
}

// bodyPuts returns the body PUTs of upload sessions among reqs.
func bodyPuts(reqs []fakeRequest) []fakeRequest {
	var out []fakeRequest

	for _, r := range reqs {
		if r.session() && r.method == http.MethodPut && r.contentRange == "" {
			out = append(out, r)
		}
	}

	return out
}

// sessionIDs returns the IDs of the sessions the emulator opened, in order.
func (d *fakeDrive) sessionIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	ids := make([]string, 0, d.nextSession)
	for i := 1; i <= d.nextSession; i++ {
		ids = append(ids, fmt.Sprintf("UPLOAD-SECRET-%03d", i))
	}

	return ids
}

// containerData returns the data of the container files, in creation order.
func (d *fakeDrive) containerData() []string {
	d.mu.Lock()
	defer d.mu.Unlock()

	var out []string

	for _, f := range d.files {
		if f.name == container && !f.trashed {
			out = append(out, string(f.data))
		}
	}

	return out
}

// TestWriteRecordsBeforeFinalPut: the record is saved before the body PUT,
// which carries the token, and removed after the verified answer.
func TestWriteRecordsBeforeFinalPut(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}
	up.onSave = func() {
		// up.saves counts the earlier saves; onSave runs under its lock.
		if n := len(bodyPuts(d.reqs())); n != up.saves {
			t.Errorf("record saved after %d body PUTs, want before the body PUT of its upload", n)
		}
	}

	s := uploadStore(t, d, up)
	mustWrite(t, s, []byte("first"), "")
	wantNoRecord(t, up)

	_, v1, _ := mustRead(t, s)
	mustWrite(t, s, []byte("second"), v1)
	wantNoRecord(t, up)

	if up.saves != 2 {
		t.Errorf("records saved = %d, want 2", up.saves)
	}

	puts := bodyPuts(d.reqs())
	if len(puts) != 2 {
		t.Fatalf("body PUTs = %d, want 2", len(puts))
	}

	for _, r := range puts {
		if r.auth != "Bearer "+fakeAccess {
			t.Errorf("body PUT carried %q, want the access token", r.auth)
		}
	}

	if got := d.containerData(); len(got) != 1 || got[0] != "second" {
		t.Errorf("container files = %q, want one with the replacement", got)
	}
}

// TestWriteDoesNotSendIfMatch: Drive v3 does not check If-Match (П5), so it
// is never sent; the race window is accepted (spec §5.3, §11).
func TestWriteDoesNotSendIfMatch(t *testing.T) {
	d := newFakeDrive(t)
	s := uploadStore(t, d, &memUploads{})

	mustWrite(t, s, []byte("first"), "")
	_, v1, _ := mustRead(t, s)
	mustWrite(t, s, []byte("second"), v1)

	for _, r := range d.reqs() {
		if r.ifMatch != "" {
			t.Errorf("%s %s carried If-Match", r.method, r.path)
		}
	}
}

func TestWriteReplaceChecksVersion(t *testing.T) {
	tests := []struct {
		name   string
		expect func(id string) string
		change func(d *fakeDrive, id string)
	}{
		{name: "other md5", expect: func(id string) string { return id + ":" + md5Of([]byte("other")) }},
		{name: "deleted file", expect: func(id string) string { return id + ":" + md5Of([]byte("v1")) },
			change: func(d *fakeDrive, id string) { d.remove(id) }},
		{name: "trashed file", expect: func(id string) string { return id + ":" + md5Of([]byte("v1")) },
			change: func(d *fakeDrive, id string) { d.fileOf(id).trashed = true }},
		{name: "not a version", expect: func(string) string { return "no-colon" }},
		{name: "bad file id", expect: func(string) string { return "../x:" + md5Of([]byte("v1")) }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDrive(t)
			id := d.put(container, []byte("v1"), testNow)

			if tc.change != nil {
				d.set(func(d *fakeDrive) { tc.change(d, id) })
			}

			err := writeErr(t, uploadStore(t, d, &memUploads{}), []byte("v2"), tc.expect(id))
			if !errors.Is(err, blobremote.ErrVersionMismatch) {
				t.Errorf("Write: err = %v, want ErrVersionMismatch", err)
			}

			if n := len(d.sessionIDs()); n != 0 {
				t.Errorf("sessions opened = %d, want 0", n)
			}
		})
	}
}

// meetingPuts makes the body PUTs of two uploads wait for each other, so
// that both creations pass their check of absence first.
func meetingPuts(t *testing.T, d *fakeDrive) {
	t.Helper()

	var (
		mu sync.Mutex
		n  int
	)

	both := make(chan struct{})

	d.set(func(d *fakeDrive) {
		d.beforePut = func() {
			mu.Lock()

			n++
			if n == 2 {
				close(both)
			}
			mu.Unlock()

			select {
			case <-both:
			case <-time.After(5 * time.Second):
				t.Error("the second upload did not come")
			}
		}
	})
}

// TestCreateRaceLoserDeletesOwn: two devices create at once; the one whose
// file is not the earliest deletes it and gets ErrVersionMismatch, and one
// file stays (spec §5.3, creation step 2).
func TestCreateRaceLoserDeletesOwn(t *testing.T) {
	d := newFakeDrive(t)
	meetingPuts(t, d)

	errs := make([]error, 2)

	var wg sync.WaitGroup

	for i, data := range []string{"device-a", "device-b"} {
		s, err := testFactory(t, d, &memUploads{})(googleTarget(testTokens()))
		if err != nil {
			t.Fatal(err)
		}

		wg.Go(func() { errs[i] = s.Write(t.Context(), container, []byte(data), "") })
	}

	wg.Wait()

	winner := -1

	for i, err := range errs {
		switch {
		case err == nil && winner < 0:
			winner = i
		case !errors.Is(err, blobremote.ErrVersionMismatch):
			t.Errorf("Write %d: err = %v, want one success and one ErrVersionMismatch", i, err)
		}
	}

	if winner < 0 {
		t.Fatalf("errs = %v, want one success", errs)
	}

	want := []string{"device-a", "device-b"}[winner]
	if got := d.containerData(); len(got) != 1 || got[0] != want {
		t.Errorf("container files = %q, want only %q", got, want)
	}
}

// TestCreateYieldsToEqualCreatedTime: device A creates the container alone
// and succeeds; device B, whose check of absence came before A's file,
// creates a file with the same createdTime and a smaller ID afterwards. B
// yields: it deletes its own file and gets ErrVersionMismatch, and A's
// confirmed file stays and is read (spec §5.3, creation step 2).
func TestCreateYieldsToEqualCreatedTime(t *testing.T) {
	d := newFakeDrive(t)
	a, b := uploadStore(t, d, &memUploads{}), uploadStore(t, d, &memUploads{})

	var puts atomic.Int32

	bWaits, aDone := make(chan struct{}), make(chan struct{})

	d.set(func(d *fakeDrive) {
		// B's body PUT comes first and waits until A has created alone.
		d.beforePut = func() {
			if puts.Add(1) != 1 {
				return
			}

			close(bWaits)

			select {
			case <-aDone:
			case <-time.After(5 * time.Second):
				t.Error("device A did not finish")
			}
		}
	})

	var (
		wg   sync.WaitGroup
		bErr error
	)

	wg.Go(func() { bErr = writeErr(t, b, []byte("device-b"), "") })

	select {
	case <-bWaits:
	case <-time.After(5 * time.Second):
		t.Fatal("device B did not upload")
	}

	aErr := writeErr(t, a, []byte("device-a"), "")

	// B's file gets the createdTime of A's and an ID before it.
	d.set(func(d *fakeDrive) { d.clock, d.nextFileID = fakeUploadTime, "file-000" })
	close(aDone)
	wg.Wait()

	if aErr != nil {
		t.Fatalf("Write of A: %v", aErr)
	}

	if !errors.Is(bErr, blobremote.ErrVersionMismatch) {
		t.Errorf("Write of B: err = %v, want ErrVersionMismatch", bErr)
	}

	if got := deletedFiles(d); len(got) != 1 || got[0] != "file-000" {
		t.Errorf("deleted = %v, want only B's own file-000", got)
	}

	if got := d.containerData(); len(got) != 1 || got[0] != "device-a" {
		t.Errorf("container files = %q, want only A's", got)
	}

	data, version, _ := mustRead(t, uploadStore(t, d, &memUploads{}))
	if string(data) != "device-a" || !strings.HasPrefix(version, "file-001:") {
		t.Errorf("Read = %q, version %q; want A's file-001", data, version)
	}
}

// TestCreateYieldsWithOwnSmallerID: a file of another device with the same
// createdTime makes the creating device yield even when its own ID is the
// smaller one (spec §5.3, creation step 2).
func TestCreateYieldsWithOwnSmallerID(t *testing.T) {
	d := newFakeDrive(t)

	d.set(func(d *fakeDrive) {
		d.nextFileID = "file-000"
		d.onPut = func(d *fakeDrive, _ *fakeSession) putAnswer {
			// The other device's file, missed by the check of absence.
			d.add("file-zzz", container, []byte("other"), d.clock)
			return putAnswer{}
		}
	})

	err := writeErr(t, uploadStore(t, d, &memUploads{}), []byte("mine"), "")
	if !errors.Is(err, blobremote.ErrVersionMismatch) {
		t.Fatalf("Write: err = %v, want ErrVersionMismatch", err)
	}

	if got := d.containerData(); len(got) != 1 || got[0] != "other" {
		t.Errorf("container files = %q, want only the other device's", got)
	}
}

// TestCreateStaysBeforeLaterFiles: files of other devices that are all
// strictly later leave the own file effective; they are removed as its
// duplicates.
func TestCreateStaysBeforeLaterFiles(t *testing.T) {
	d := newFakeDrive(t)

	d.set(func(d *fakeDrive) {
		d.onPut = func(d *fakeDrive, _ *fakeSession) putAnswer {
			d.add("file-000", container, []byte("later"), d.clock.Add(time.Millisecond))
			return putAnswer{}
		}
	})

	mustWrite(t, uploadStore(t, d, &memUploads{}), []byte("mine"), "")

	if got := deletedFiles(d); len(got) != 1 || got[0] != "file-000" {
		t.Errorf("deleted = %v, want only the later file-000", got)
	}

	if got := d.containerData(); len(got) != 1 || got[0] != "mine" {
		t.Errorf("container files = %q, want only the own one", got)
	}
}

// refusedOnce answers the first body PUT with 401 and does not commit it;
// change, if set, is what another device does meanwhile. The API then
// rejects the old access token.
func refusedOnce(change func(d *fakeDrive)) func(d *fakeDrive, s *fakeSession) putAnswer {
	first := true

	return func(d *fakeDrive, _ *fakeSession) putAnswer {
		if !first {
			return putAnswer{}
		}

		first = false
		d.access = "acc-expired"

		if change != nil {
			change(d)
		}

		return putAnswer{reject: true, status: http.StatusUnauthorized}
	}
}

// refusingStore is a store over d with a real clock, whose token the
// emulator can refresh.
func refusingStore(t *testing.T, d *fakeDrive, up *memUploads) *Store {
	t.Helper()

	opts := testOptions(d)
	opts.now = time.Now

	return newStore(d.endpoints(), signedIn(fakeAccess, time.Hour), fakeClient, testRefresher(t, d), up, opts)
}

// wantFirstSessionCancelled checks that the refused session was resolved
// by its cancellation.
func wantFirstSessionCancelled(t *testing.T, d *fakeDrive) {
	t.Helper()

	ids := d.sessionIDs()
	if len(ids) == 0 {
		t.Fatal("no session was opened")
	}

	if st := d.sessionState(ids[0]); st != sessCancelled {
		t.Errorf("first session state = %d, want cancelled", st)
	}
}

// TestPut401CreateRechecksAbsence: a final PUT refused with 401 is never
// repeated on its URI; after a refresh and the resolution of the old
// session the creation is done once more from its check of absence
// (spec §5.4).
func TestPut401CreateRechecksAbsence(t *testing.T) {
	t.Run("another device created meanwhile", func(t *testing.T) {
		d := newFakeDrive(t)
		up := &memUploads{}

		d.set(func(d *fakeDrive) {
			d.onPut = refusedOnce(func(d *fakeDrive) { d.add("file-other", container, []byte("other"), testNow) })
		})

		err := writeErr(t, refusingStore(t, d, up), []byte("mine"), "")
		if !errors.Is(err, blobremote.ErrVersionMismatch) {
			t.Fatalf("Write: err = %v, want ErrVersionMismatch", err)
		}

		wantNoRecord(t, up)
		wantFirstSessionCancelled(t, d)

		if got := d.containerData(); len(got) != 1 || got[0] != "other" {
			t.Errorf("container files = %q, want only the other device's", got)
		}

		if n := len(d.forms()); n != 1 {
			t.Errorf("token refreshes = %d, want 1", n)
		}

		if n := d.count(http.MethodGet, filesPath, false); n != 2 {
			t.Errorf("lists = %d, want 2 (the check of absence repeated)", n)
		}

		if n := len(bodyPuts(d.reqs())); n != 1 {
			t.Errorf("body PUTs = %d, want 1", n)
		}
	})

	t.Run("nothing changed", func(t *testing.T) {
		d := newFakeDrive(t)
		up := &memUploads{}

		d.set(func(d *fakeDrive) { d.onPut = refusedOnce(nil) })

		mustWrite(t, refusingStore(t, d, up), []byte("mine"), "")
		wantNoRecord(t, up)
		wantFirstSessionCancelled(t, d)

		if got := d.containerData(); len(got) != 1 || got[0] != "mine" {
			t.Errorf("container files = %q, want only the created one", got)
		}

		puts := bodyPuts(d.reqs())
		if len(puts) != 2 || puts[0].query.Get("upload_id") == puts[1].query.Get("upload_id") {
			t.Errorf("body PUTs = %d, want 2 on different sessions", len(puts))
		}

		if puts[1].auth != "Bearer acc-2" {
			t.Errorf("second body PUT carried %q, want the refreshed token", puts[1].auth)
		}
	})

	t.Run("refused again asks for a sign-in", func(t *testing.T) {
		d := newFakeDrive(t)
		up := &memUploads{}

		d.set(func(d *fakeDrive) {
			d.onPut = func(*fakeDrive, *fakeSession) putAnswer {
				return putAnswer{reject: true, status: http.StatusUnauthorized}
			}
		})

		err := writeErr(t, refusingStore(t, d, up), []byte("mine"), "")
		if re, ok := errors.AsType[*domain.ReauthRequiredError](err); !ok || re.Kind != domain.KindGoogleDrive {
			t.Fatalf("Write: err = %v, want ReauthRequiredError{google_drive}", err)
		}

		wantNoRecord(t, up)

		if n := len(bodyPuts(d.reqs())); n != 2 {
			t.Errorf("body PUTs = %d, want 2 (one repeat of the operation)", n)
		}

		if got := d.containerData(); len(got) != 0 {
			t.Errorf("container files = %q, want none", got)
		}
	})
}

// TestPut401ReplaceRechecksVersion: the repeat of a replacement after a
// refused final PUT checks the version again.
func TestPut401ReplaceRechecksVersion(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}
	id := d.put(container, []byte("v1"), testNow)
	d.set(func(d *fakeDrive) {
		d.onPut = refusedOnce(func(d *fakeDrive) { d.replaceData(id, []byte("other")) })
	})

	err := writeErr(t, refusingStore(t, d, up), []byte("mine"), id+":"+md5Of([]byte("v1")))
	if !errors.Is(err, blobremote.ErrVersionMismatch) {
		t.Fatalf("Write: err = %v, want ErrVersionMismatch", err)
	}

	wantNoRecord(t, up)
	wantFirstSessionCancelled(t, d)

	if got := d.containerData(); len(got) != 1 || got[0] != "other" {
		t.Errorf("container files = %q, want the other device's replacement", got)
	}

	if n := d.count(http.MethodGet, filesPath+"/"+id, false); n != 2 {
		t.Errorf("version checks = %d, want 2", n)
	}
}

// TestRecordWriteFailureCancelsSession: without the record no final PUT is
// sent; the session is cancelled and the error is local (spec §5.4, 1).
func TestRecordWriteFailureCancelsSession(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{saveErr: errors.New("disk full")}

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	if _, ok := errors.AsType[usecase.LocalError](err); !ok {
		t.Fatalf("Write: err = %v, want usecase.LocalError", err)
	}

	if n := len(bodyPuts(d.reqs())); n != 0 {
		t.Errorf("body PUTs = %d, want 0", n)
	}

	cancels := 0

	for _, r := range d.reqs() {
		if r.session() && r.method == http.MethodDelete {
			cancels++

			if r.auth != "" {
				t.Errorf("DELETE of the session carried %q, want no Authorization", r.auth)
			}
		}
	}

	if cancels != 1 {
		t.Errorf("DELETE of the session = %d, want 1", cancels)
	}

	wantFirstSessionCancelled(t, d)

	if got := d.containerData(); len(got) != 0 {
		t.Errorf("container files = %q, want none", got)
	}
}

// TestCreateEmptyListAfterCreateIsUnknown: a list without any file right
// after our own creation is an anomaly, not a lost race: the own file is
// not deleted and the outcome is unknown.
func TestCreateEmptyListAfterCreateIsUnknown(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	d.set(func(d *fakeDrive) {
		d.onPut = func(d *fakeDrive, _ *fakeSession) putAnswer {
			d.hideFromList = true // the next lists miss the new file
			return putAnswer{}
		}
	})

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	if !errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
	}

	wantNoRecord(t, up)

	if n := len(deletedFiles(d)); n != 0 {
		t.Errorf("deleted files = %d, want 0", n)
	}

	if got := d.containerData(); len(got) != 1 || got[0] != "mine" {
		t.Errorf("container files = %q, want the created one", got)
	}
}

// ownMissing makes the creation's file file-own, which the lists do not
// give, as lists behind its commit; other, when it has data, is the file of
// another device created meanwhile at the createdTime of the own one plus
// shift.
func ownMissing(d *fakeDrive, other string, shift time.Duration) {
	d.set(func(d *fakeDrive) {
		d.nextFileID, d.hideID = "file-own", "file-own"
		d.onPut = func(d *fakeDrive, _ *fakeSession) putAnswer {
			if other != "" {
				d.add("file-zzz", container, []byte(other), d.clock.Add(shift))
			}

			return putAnswer{}
		}
	})
}

// TestCreateOwnMissingBeforeLaterIsUnknown: the list after the creation
// gives only a strictly later file of another device and not the own one.
// The own file may be the earliest, effective for every device, so nothing
// is deleted and the outcome is unknown (spec §5.2, §5.3).
func TestCreateOwnMissingBeforeLaterIsUnknown(t *testing.T) {
	d := newFakeDrive(t)
	ownMissing(d, "later", time.Second)

	err := writeErr(t, uploadStore(t, d, &memUploads{}), []byte("mine"), "")
	if !errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
	}

	if got := deletedFiles(d); len(got) != 0 {
		t.Errorf("deleted = %v, want none", got)
	}

	if got := d.containerData(); len(got) != 2 {
		t.Errorf("container files = %q, want the own and the other one", got)
	}
}

// TestCreateOwnMissingYieldsToEarlier: the own file is not listed, but
// another file is not later than its createdTime from the upload answer,
// so the creation yields as if its file were listed.
func TestCreateOwnMissingYieldsToEarlier(t *testing.T) {
	for _, tc := range []struct {
		name  string
		shift time.Duration
	}{
		{"equal createdTime", 0},
		{"earlier createdTime", -time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDrive(t)
			ownMissing(d, "other", tc.shift)

			err := writeErr(t, uploadStore(t, d, &memUploads{}), []byte("mine"), "")
			if !errors.Is(err, blobremote.ErrVersionMismatch) {
				t.Fatalf("Write: err = %v, want ErrVersionMismatch", err)
			}

			if got := deletedFiles(d); len(got) != 1 || got[0] != "file-own" {
				t.Errorf("deleted = %v, want only the own file", got)
			}

			if got := d.containerData(); len(got) != 1 || got[0] != "other" {
				t.Errorf("container files = %q, want only the other device's", got)
			}
		})
	}
}

// TestCreateOwnMissingWithoutCreatedIsUnknown: without createdTime in the
// upload answer an own file the list does not give cannot be ordered, so
// nothing is deleted and the outcome is unknown.
func TestCreateOwnMissingWithoutCreatedIsUnknown(t *testing.T) {
	d := newFakeDrive(t)
	ownMissing(d, "earlier", -time.Second)
	d.set(func(d *fakeDrive) { d.omitCreated = true })

	err := writeErr(t, uploadStore(t, d, &memUploads{}), []byte("mine"), "")
	if !errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
	}

	if got := deletedFiles(d); len(got) != 0 {
		t.Errorf("deleted = %v, want none", got)
	}
}
