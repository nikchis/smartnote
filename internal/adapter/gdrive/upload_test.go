package gdrive

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

const wrongMD5 = "00000000000000000000000000000000"

// failPut makes every body PUT answer with a; commit unless a.reject.
func failPut(d *fakeDrive, a putAnswer) {
	d.set(func(d *fakeDrive) { d.onPut = func(*fakeDrive, *fakeSession) putAnswer { return a } })
}

// statusRequests returns the status requests of upload sessions among reqs.
func statusRequests(reqs []fakeRequest) []fakeRequest {
	var out []fakeRequest

	for _, r := range reqs {
		if r.session() && r.method == http.MethodPut && r.contentRange != "" {
			out = append(out, r)
		}
	}

	return out
}

// TestUploadSuccessVerifiesCheck: a 200 answer to the final PUT whose
// md5Checksum is not the MD5 of the data is a cloud error; the session is
// committed, so the record goes (spec §5.1, §5.4).
func TestUploadSuccessVerifiesCheck(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	d.set(func(d *fakeDrive) { d.putMD5 = wrongMD5 })

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	_ = cloudError(t, err)

	if errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Errorf("err = %v, want a cloud error, not an unknown outcome", err)
	}

	wantNoRecord(t, up)
}

// TestPut503ThenStatus200: the body was committed and the answer is 503;
// the status gives the file with our MD5, which is our commit.
func TestPut503ThenStatus200(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	failPut(d, putAnswer{status: http.StatusServiceUnavailable})

	mustWrite(t, uploadStore(t, d, up), []byte("mine"), "")
	wantNoRecord(t, up)

	st := statusRequests(d.reqs())
	if len(st) != 1 || st[0].contentRange != "bytes */4" || st[0].auth != "" {
		t.Errorf("status requests = %+v, want one bytes */4 without Authorization", st)
	}

	if got := d.containerData(); len(got) != 1 || got[0] != "mine" {
		t.Errorf("container files = %q, want the created one", got)
	}
}

// TestPut503ThenStatus200WrongCheck: the status of the session inside Write
// gives a file whose md5Checksum is not ours: Google took other bytes.
func TestPut503ThenStatus200WrongCheck(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	failPut(d, putAnswer{status: http.StatusServiceUnavailable})
	d.set(func(d *fakeDrive) { d.statusMD5 = wrongMD5 })

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	_ = cloudError(t, err)

	if errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Errorf("err = %v, want a cloud error", err)
	}

	wantNoRecord(t, up)
}

// TestPut503StatusUnavailable: neither the PUT nor the status answers: the
// outcome is unknown and the record stays; the next open resolves it.
func TestPut503StatusUnavailable(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	failPut(d, putAnswer{reject: true, status: http.StatusServiceUnavailable})
	d.set(func(d *fakeDrive) { d.statusAnswers = []int{http.StatusServiceUnavailable} })

	s := uploadStore(t, d, up)

	err := writeErr(t, s, []byte("mine"), "")
	if !errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
	}

	rec := wantRecord(t, up, 4)
	if rec.Op != opCreate {
		t.Errorf("record op = %q, want %q", rec.Op, opCreate)
	}

	// The same store resolves before its next access.
	if _, _, found := mustRead(t, s); found {
		t.Error("Read found a container that was never committed")
	}

	wantNoRecord(t, up)
}

// TestStatus308CancelThenRemoved: the session is not complete; its
// cancellation is confirmed by 499 and the original error is returned.
func TestStatus308CancelThenRemoved(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	failPut(d, putAnswer{reject: true, status: http.StatusServiceUnavailable})

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	if ce := cloudError(t, err); ce.Status != http.StatusServiceUnavailable {
		t.Errorf("CloudError = %+v, want the 503 of the PUT", ce)
	}

	if errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Errorf("err = %v, want the original error", err)
	}

	wantNoRecord(t, up)
	wantFirstSessionCancelled(t, d)

	if got := d.containerData(); len(got) != 0 {
		t.Errorf("container files = %q, want none", got)
	}
}

// TestCancelAnswerLostThen404: the answer to DELETE is lost; the repeated
// status (404, or 499 as Drive answers for a cancelled session) confirms
// the cancellation.
func TestCancelAnswerLostThen404(t *testing.T) {
	for name, second := range map[string]int{"404": http.StatusNotFound, "499": answerServe, "410": http.StatusGone} {
		t.Run(name, func(t *testing.T) {
			d := newFakeDrive(t)
			up := &memUploads{}

			failPut(d, putAnswer{reject: true, status: http.StatusServiceUnavailable})
			d.set(func(d *fakeDrive) {
				d.deleteAnswers = []int{answerDrop}
				d.statusAnswers = []int{answerServe, second}
			})

			err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
			if ce := cloudError(t, err); ce.Status != http.StatusServiceUnavailable {
				t.Errorf("CloudError = %+v, want the 503 of the PUT", ce)
			}

			wantNoRecord(t, up)
			wantFirstSessionCancelled(t, d)

			if n := len(statusRequests(d.reqs())); n != 2 {
				t.Errorf("status requests = %d, want 2", n)
			}
		})
	}
}

// TestCancelAnswer200IsCommitted: the session completed between the status
// 308 and the cancellation; DELETE answers 200 with the file, which is
// step 2 (spec §5.4, rev. 7).
func TestCancelAnswer200IsCommitted(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	failPut(d, putAnswer{status: http.StatusServiceUnavailable})
	d.set(func(d *fakeDrive) { d.statusAnswers = []int{http.StatusPermanentRedirect} })

	mustWrite(t, uploadStore(t, d, up), []byte("mine"), "")
	wantNoRecord(t, up)

	if got := d.containerData(); len(got) != 1 || got[0] != "mine" {
		t.Errorf("container files = %q, want the created one", got)
	}

	if n := len(statusRequests(d.reqs())); n != 1 {
		t.Errorf("status requests = %d, want 1 (the answer to DELETE settles it)", n)
	}
}

// TestStatus404RemovesRecordWithoutReadingFiles: an unknown session cannot
// commit any more; the record goes without a request to the files, and the
// outcome is unknown.
func TestStatus404RemovesRecordWithoutReadingFiles(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	d.set(func(d *fakeDrive) {
		d.onPut = func(d *fakeDrive, s *fakeSession) putAnswer {
			delete(d.sessions, s.id)
			return putAnswer{reject: true, drop: true}
		}
	})

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	if !errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
	}

	wantNoRecord(t, up)

	reqs := d.reqs()
	put := slices.IndexFunc(reqs, func(r fakeRequest) bool { return len(bodyPuts([]fakeRequest{r})) == 1 })

	for _, r := range reqs[put+1:] {
		if strings.HasPrefix(r.path, filesPath) {
			t.Errorf("request %s %s after the PUT, want none to the files", r.method, r.path)
		}

		if r.method == http.MethodDelete {
			t.Errorf("DELETE %s after a 404 status", r.path)
		}
	}
}

// TestOtherPut4xxResolvesThenCloud: a final PUT refused with another 4xx is
// resolved and gives a cloud error, without a repeat on the same URI.
func TestOtherPut4xxResolvesThenCloud(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	failPut(d, putAnswer{reject: true, status: http.StatusBadRequest})

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	if ce := cloudError(t, err); ce.Status != http.StatusBadRequest {
		t.Errorf("CloudError = %+v, want the 400 of the PUT", ce)
	}

	wantNoRecord(t, up)

	if n := len(bodyPuts(d.reqs())); n != 1 {
		t.Errorf("body PUTs = %d, want 1", n)
	}

	if n := len(statusRequests(d.reqs())); n != 1 {
		t.Errorf("status requests = %d, want 1", n)
	}
}

// TestPut412IsMismatchWithoutStatus: a refusal of a precondition at the
// commit is never committed: the record goes without a status request.
// Drive does not check If-Match (П5); the branch of §5.4 stays cheap.
func TestPut412IsMismatchWithoutStatus(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	failPut(d, putAnswer{reject: true, status: http.StatusPreconditionFailed})

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	if !errors.Is(err, blobremote.ErrVersionMismatch) {
		t.Fatalf("Write: err = %v, want ErrVersionMismatch", err)
	}

	wantNoRecord(t, up)

	if n := len(statusRequests(d.reqs())); n != 0 {
		t.Errorf("status requests = %d, want 0", n)
	}
}

// TestOpen412IsMismatch: a refusal of a precondition at the opening of the
// session leaves nothing to record.
func TestOpen412IsMismatch(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	d.set(func(d *fakeDrive) {
		d.fail[epOpen] = fakeFailure{status: http.StatusPreconditionFailed, reason: "conditionNotMet"}
	})

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	if !errors.Is(err, blobremote.ErrVersionMismatch) {
		t.Fatalf("Write: err = %v, want ErrVersionMismatch", err)
	}

	if up.saves != 0 {
		t.Errorf("records saved = %d, want 0", up.saves)
	}
}

// TestForeignSessionURIRejected: a session URI outside www.googleapis.com
// is never contacted nor recorded; a recorded one is not used.
func TestForeignSessionURIRejected(t *testing.T) {
	onlyEmulator := func(h string) bool { return h == "127.0.0.1" }

	t.Run("Location of the opening", func(t *testing.T) {
		d := newFakeDrive(t)
		up := &memUploads{}

		d.set(func(d *fakeDrive) { d.sessionHost = "uploads.example.org" })

		opts := testOptions(d)
		opts.allowHost = onlyEmulator

		err := newStore(d.endpoints(), testTokens(), fakeClient, testRefresher(t, d), up, opts).
			Write(t.Context(), container, []byte("mine"), "")
		if err == nil {
			t.Fatal("Write with a foreign session URI succeeded")
		}

		if strings.Contains(err.Error(), "uploads.example.org") || strings.Contains(err.Error(), "UPLOAD-SECRET") {
			t.Errorf("error %q quotes the session URI", err)
		}

		if up.saves != 0 {
			t.Errorf("records saved = %d, want 0", up.saves)
		}
	})

	t.Run("recorded URI", func(t *testing.T) {
		d := newFakeDrive(t)
		up := &memUploads{}
		up.set(t, pendingUpload{SessionURI: "https://uploads.example.org/upload?upload_id=UPLOAD-SECRET-9", Op: opCreate, Size: 4})

		opts := testOptions(d)
		opts.allowHost = onlyEmulator

		err := readErr(t, newStore(d.endpoints(), testTokens(), fakeClient, testRefresher(t, d), up, opts))
		if _, ok := errors.AsType[usecase.LocalError](err); !ok {
			t.Errorf("Read: err = %v, want usecase.LocalError", err)
		}

		if n := len(d.reqs()); n != 0 {
			t.Errorf("requests = %d, want 0", n)
		}

		wantRecord(t, up, 4)
	})
}

func TestWriteReportsUploadProgress(t *testing.T) {
	d := newFakeDrive(t)

	var (
		mu      sync.Mutex
		reports []usecase.TransferProgress
	)

	opts := testOptions(d)
	opts.progress = func(p usecase.TransferProgress) {
		mu.Lock()
		defer mu.Unlock()

		reports = append(reports, p)
	}

	data := []byte(strings.Repeat("x", 1000))
	s := newStore(d.endpoints(), testTokens(), fakeClient, testRefresher(t, d), &memUploads{}, opts)
	mustWrite(t, s, data, "")

	mu.Lock()
	defer mu.Unlock()

	want := usecase.TransferProgress{
		Storage: domain.KindGoogleDrive, Direction: usecase.DirectionUpload, Unit: usecase.UnitBytes, Done: 1000, Total: 1000,
	}

	if !slices.Contains(reports, want) {
		t.Errorf("reports = %+v, want %+v", reports, want)
	}
}

// TestPutTimeoutStillResolves: the operation's time runs out during the
// final PUT; the resolution gets its own deadline, cancels the incomplete
// session and the operation's timeout is returned. The PUT is held until
// the operation's context is done and then dropped uncommitted, so no
// timing decides the test.
func TestPutTimeoutStillResolves(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	d.set(func(d *fakeDrive) {
		d.beforePut = cancel // the operation ends while the PUT is on the way
		d.onPut = func(*fakeDrive, *fakeSession) putAnswer { return putAnswer{reject: true, drop: true} }
	})

	err := uploadStore(t, d, up).Write(ctx, container, []byte("mine"), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Write: err = %v, want the end of the operation's context", err)
	}

	wantNoRecord(t, up)
	wantFirstSessionCancelled(t, d)
}
