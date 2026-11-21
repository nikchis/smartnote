package gdrive

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// filesRequests counts the requests to the files of the API among reqs.
func filesRequests(reqs []fakeRequest) int {
	n := 0

	for _, r := range reqs {
		if r.server == "api" && strings.HasPrefix(r.path, filesPath) {
			n++
		}
	}

	return n
}

// TestStoreResolvesUploadAfterRestart: the network fails exactly on the
// final PUT and the app closes. After the restart every open resolves the
// record first; while the status is unavailable, Read and Write give
// ErrRemoteOutcomeUnknown without touching the container (spec §5.4, 3).
func TestStoreResolvesUploadAfterRestart(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}

	failPut(d, putAnswer{drop: true}) // committed, the answer is lost
	d.set(func(d *fakeDrive) { d.statusAnswers = []int{503, 503, 503} })

	err := writeErr(t, uploadStore(t, d, up), []byte("mine"), "")
	if !errors.Is(err, blobremote.ErrOutcomeUnknown) {
		t.Fatalf("Write: err = %v, want ErrOutcomeUnknown", err)
	}

	wantRecord(t, up, 4)

	restarted := uploadStore(t, d, up)
	before := len(d.reqs())

	if err := readErr(t, restarted); !errors.Is(err, domain.ErrRemoteOutcomeUnknown) {
		t.Errorf("Read: err = %v, want ErrRemoteOutcomeUnknown", err)
	}

	if err := writeErr(t, restarted, []byte("next"), ""); !errors.Is(err, domain.ErrRemoteOutcomeUnknown) {
		t.Errorf("Write: err = %v, want ErrRemoteOutcomeUnknown", err)
	}

	if n := filesRequests(d.reqs()[before:]); n != 0 {
		t.Errorf("requests to the files = %d while unresolved, want 0", n)
	}

	wantRecord(t, up, 4)

	data, _, found := mustRead(t, restarted)
	if !found || string(data) != "mine" {
		t.Errorf("Read = %q, %v; want the committed container", data, found)
	}

	wantNoRecord(t, up)
}

// TestResolveOnOpenSkipsCheck: at the open a committed session (200 or 201)
// removes the record whatever its check value; the open goes on, and no
// file or revision is read before the usual list (spec §5.4, step 2).
func TestResolveOnOpenSkipsCheck(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			d := newFakeDrive(t)
			up := &memUploads{}
			id := d.put(container, []byte("v1"), testNow)
			sid, uri := d.newSession(opReplace, id, "")
			d.commitSession(sid, []byte("mine"))
			d.set(func(d *fakeDrive) {
				d.replaceData(id, []byte("theirs")) // another device wrote later
				d.statusMD5 = wrongMD5

				if status != http.StatusOK {
					d.statusAnswers = []int{status}
				}
			})
			up.set(t, pendingUpload{SessionURI: uri, Op: opReplace, Size: 4})

			data, _, found := mustRead(t, uploadStore(t, d, up))
			if !found || string(data) != "theirs" {
				t.Errorf("Read = %q, %v; want the current container", data, found)
			}

			wantNoRecord(t, up)

			reqs := d.reqs()
			if len(reqs) < 2 || !reqs[0].session() || reqs[1].path != filesPath {
				t.Fatalf("requests = %+v, want the status and then the list", reqs)
			}

			for _, r := range reqs {
				if r.method == http.MethodDelete || strings.Contains(r.path, "/revisions") {
					t.Errorf("request %s %s, want none", r.method, r.path)
				}
			}
		})
	}
}

// TestResolveOnOpenSettledOutcomes: every proven outcome removes the record
// and the open goes on.
func TestResolveOnOpenSettledOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(d *fakeDrive, sid string)
		cancels int
	}{
		{name: "incomplete session is cancelled", cancels: 1},
		{name: "cancelled session (499)", prepare: func(d *fakeDrive, sid string) {
			d.set(func(d *fakeDrive) { d.sessions[sid].state = sessCancelled })
		}},
		{name: "unknown session (404)", prepare: func(d *fakeDrive, _ string) { d.forgetSessions() }},
		{name: "gone session (410)", prepare: func(d *fakeDrive, _ string) {
			d.set(func(d *fakeDrive) { d.statusAnswers = []int{http.StatusGone} })
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDrive(t)
			up := &memUploads{}
			sid, uri := d.newSession(opCreate, "", container)

			if tc.prepare != nil {
				tc.prepare(d, sid)
			}

			up.set(t, pendingUpload{SessionURI: uri, Op: opCreate, Size: 4})

			if _, _, found := mustRead(t, uploadStore(t, d, up)); found {
				t.Error("Read found a container")
			}

			wantNoRecord(t, up)

			cancels := 0

			for _, r := range d.reqs() {
				if r.session() && r.method == http.MethodDelete {
					cancels++
				}
			}

			if cancels != tc.cancels {
				t.Errorf("DELETE of the session = %d, want %d", cancels, tc.cancels)
			}
		})
	}
}

// brokenSource is the token source of a device signed out after the crash.
type brokenSource struct{}

func (brokenSource) Token(context.Context) (usecase.CloudToken, error) {
	return usecase.CloudToken{}, domain.ReauthRequired(domain.KindGoogleDrive)
}

func (brokenSource) Update(context.Context, usecase.CloudToken) error {
	return errors.New("no token")
}

// TestResolveIgnoresCurrentAccount: the resolution uses only the session
// URI, without Authorization (П6 е): it works after a sign-in to another
// account, and even without any token.
func TestResolveIgnoresCurrentAccount(t *testing.T) {
	t.Run("other account", func(t *testing.T) {
		d := newFakeDrive(t)
		up := &memUploads{}
		_, uri := d.newSession(opCreate, "", container)
		up.set(t, pendingUpload{SessionURI: uri, Op: opCreate, Size: 4})
		d.set(func(d *fakeDrive) { d.access = "acc-other-account" })

		other := usecase.NewMemoryTokenSource(usecase.CloudToken{
			Provider: domain.KindGoogleDrive, ClientID: "another-client", Access: "acc-other-account", Refresh: "r",
			Expiry: testNow.Add(3600e9),
		}, usecase.CloudClient{Provider: domain.KindGoogleDrive, ID: "another-client", Secret: "s"})

		s := newStore(d.endpoints(), other, fakeClient, testRefresher(t, d), up, testOptions(d))
		if _, _, found := mustRead(t, s); found {
			t.Error("Read found a container")
		}

		wantNoRecord(t, up)
		wantFirstSessionCancelled(t, d)
		wantSessionRequestsUnsigned(t, d)
	})

	t.Run("no token", func(t *testing.T) {
		d := newFakeDrive(t)
		up := &memUploads{}
		_, uri := d.newSession(opCreate, "", container)
		up.set(t, pendingUpload{SessionURI: uri, Op: opCreate, Size: 4})

		s := newStore(d.endpoints(), brokenSource{}, fakeClient, testRefresher(t, d), up, testOptions(d))
		if err := readErr(t, s); !errors.Is(err, domain.ErrReauthRequired) {
			t.Errorf("Read: err = %v, want ErrReauthRequired after the resolution", err)
		}

		wantNoRecord(t, up)
		wantFirstSessionCancelled(t, d)
		wantSessionRequestsUnsigned(t, d)
	})
}

func wantSessionRequestsUnsigned(t *testing.T, d *fakeDrive) {
	t.Helper()

	n := 0

	for _, r := range d.reqs() {
		if r.session() {
			n++

			if r.auth != "" {
				t.Errorf("%s of the session carried Authorization", r.method)
			}
		}
	}

	if n == 0 {
		t.Error("no request to the session")
	}
}

// TestExpiredSessionAfterOtherDeviceWriteDoesNotBlock: the session expired
// (404) while another device replaced the file; the record goes and the
// device reads and replaces as usual.
func TestExpiredSessionAfterOtherDeviceWriteDoesNotBlock(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{}
	id := d.put(container, []byte("v1"), testNow)
	_, uri := d.newSession(opReplace, id, "")
	up.set(t, pendingUpload{SessionURI: uri, Op: opReplace, Size: 4})
	d.forgetSessions()
	d.set(func(d *fakeDrive) { d.replaceData(id, []byte("v2")) })

	s := uploadStore(t, d, up)

	data, version, found := mustRead(t, s)
	if !found || string(data) != "v2" {
		t.Fatalf("Read = %q, %v; want the other device's container", data, found)
	}

	wantNoRecord(t, up)
	mustWrite(t, s, []byte("v3"), version)

	if got := d.containerData(); len(got) != 1 || got[0] != "v3" {
		t.Errorf("container files = %q, want the replacement", got)
	}
}

// TestUnknownStatusKeepsRecord: a status answer that §5.4 does not name, or
// none, leaves the outcome unknown: the record stays and the open fails
// without reading the container.
func TestUnknownStatusKeepsRecord(t *testing.T) {
	for _, status := range []int{http.StatusFound, http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusInternalServerError, http.StatusServiceUnavailable, answerDrop} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			d := newFakeDrive(t)
			up := &memUploads{}
			_, uri := d.newSession(opCreate, "", container)
			up.set(t, pendingUpload{SessionURI: uri, Op: opCreate, Size: 4})
			d.set(func(d *fakeDrive) { d.statusAnswers = []int{status} })

			err := readErr(t, uploadStore(t, d, up))
			if !errors.Is(err, domain.ErrRemoteOutcomeUnknown) {
				t.Errorf("Read: err = %v, want ErrRemoteOutcomeUnknown", err)
			}

			if err != nil && strings.Contains(err.Error(), "UPLOAD-SECRET") {
				t.Errorf("error %q quotes the session URI", err)
			}

			wantRecord(t, up, 4)

			reqs := d.reqs()
			if filesRequests(reqs) != 0 || slices.ContainsFunc(reqs, func(r fakeRequest) bool { return r.method == http.MethodDelete }) {
				t.Errorf("requests = %+v, want only the status", reqs)
			}
		})
	}
}

// TestBadRecordIsLocalError: a record that cannot be read is a local
// error; it stays, and nothing is sent.
func TestBadRecordIsLocalError(t *testing.T) {
	d := newFakeDrive(t)
	up := &memUploads{data: []byte("not json")}

	err := readErr(t, uploadStore(t, d, up))
	if _, ok := errors.AsType[usecase.LocalError](err); !ok {
		t.Errorf("Read: err = %v, want usecase.LocalError", err)
	}

	if n := len(d.reqs()); n != 0 {
		t.Errorf("requests = %d, want 0", n)
	}
}
