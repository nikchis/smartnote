package gdrive

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

const (
	container = blobremote.ContainerName
	filesPath = fakeAPIPath + "/files"
	listMask  = "files(id,size,createdTime,md5Checksum,sha256Checksum,headRevisionId),nextPageToken"
)

var testNow = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// fakeClient is the Google client of the tests.
var fakeClient = usecase.CloudClient{Provider: domain.KindGoogleDrive, ID: fakeClientID, Secret: fakeClientSecret}

// testTokens is the token source of a device signed in to the emulator.
func testTokens() usecase.CloudTokenSource {
	return usecase.NewMemoryTokenSource(usecase.CloudToken{
		Provider: domain.KindGoogleDrive, ClientID: fakeClientID, Access: fakeAccess, Refresh: fakeRefresh,
		Expiry: testNow.Add(time.Hour),
	}, fakeClient)
}

// testOptions are the seams of a store over d.
func testOptions(d *fakeDrive) storeOptions {
	return storeOptions{
		http:      d.api.Client(),
		now:       func() time.Time { return testNow },
		allowHost: func(string) bool { return true }, // the emulator is on 127.0.0.1
	}
}

func newTestStore(t *testing.T, d *fakeDrive) *Store {
	t.Helper()

	return newStore(d.endpoints(), testTokens(), fakeClient, testRefresher(t, d), testOptions(d))
}

// testRefresher is the real refresh of the Google sign-in against the
// emulator's token endpoint.
func testRefresher(t *testing.T, d *fakeDrive) Refresher {
	t.Helper()

	return googleAuthorizer(t, oauthProvider(d.endpoints()), d.oauth.Client(), new([]string))
}

// readErr reads the container with s and returns only the error.
func readErr(t *testing.T, s *Store) error {
	t.Helper()

	_, _, _, err := s.Read(t.Context(), container) //nolint:dogsled // only the error matters here

	return err
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

// cloudError returns the CloudError in err and fails the test without one.
func cloudError(t *testing.T, err error) cloudhttp.CloudError {
	t.Helper()

	ce, ok := errors.AsType[cloudhttp.CloudError](err)
	if !ok {
		t.Fatalf("err = %v, want cloudhttp.CloudError", err)
	}

	return ce
}

// logBuffer is a log of the tests that can be read while the store writes.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestReadMissing(t *testing.T) {
	d := newFakeDrive(t)
	d.put("other.bin", []byte("not ours"), testNow)

	data, version, found := mustRead(t, newTestStore(t, d))
	if found || data != nil || version != "" {
		t.Errorf("Read = %q, %q, %v; want nothing", data, version, found)
	}
}

func TestReadSkipsTrashed(t *testing.T) {
	d := newFakeDrive(t)
	id := d.put(container, []byte("trashed"), testNow)
	d.set(func(d *fakeDrive) { d.file(id).trashed = true })

	if _, _, found := mustRead(t, newTestStore(t, d)); found {
		t.Error("a trashed file was read")
	}
}

// TestReadVersionIsFileIDAndMD5: the MD5 path of П4. The version is
// "<fileId>:<md5Checksum>" and belongs to the bytes: a list md5 that the
// downloaded bytes do not have is not taken as their version.
func TestReadVersionIsFileIDAndMD5(t *testing.T) {
	t.Run("matching md5", func(t *testing.T) {
		d := newFakeDrive(t)
		id := d.put(container, []byte("sealed container"), testNow)

		data, version, found := mustRead(t, newTestStore(t, d))
		if !found || string(data) != "sealed container" || version != id+":"+md5Of([]byte("sealed container")) {
			t.Errorf("Read = %q, %q, %v; want the container with version <id>:<md5>", data, version, found)
		}
	})

	for _, sum := range []string{"0123456789abcdef0123456789abcdef", ""} {
		t.Run("list md5 "+cmpLabel(sum), func(t *testing.T) {
			d := newFakeDrive(t)
			id := d.put(container, []byte("sealed container"), testNow)
			d.set(func(d *fakeDrive) { d.file(id).md5 = sum })

			if err := readErr(t, newTestStore(t, d)); !errors.Is(err, blobremote.ErrVersionMismatch) {
				t.Fatalf("Read: err = %v, want ErrVersionMismatch", err)
			}

			if n := d.count(http.MethodGet, filesPath+"/"+id, true); n != 2 {
				t.Errorf("downloads = %d, want 2 (one repeat)", n)
			}
		})
	}
}

func cmpLabel(sum string) string {
	if sum == "" {
		return "missing"
	}

	return "differs"
}

// TestReadVersionBoundToBytes: a replacement between the list and alt=media
// is found by the MD5 check. Once gives one repeat with the new version;
// every time gives ErrVersionMismatch after one repeat.
func TestReadVersionBoundToBytes(t *testing.T) {
	t.Run("replaced once", func(t *testing.T) {
		d := newFakeDrive(t)
		id := d.put(container, []byte("old"), testNow)

		d.set(func(d *fakeDrive) {
			d.beforeMedia = func(d *fakeDrive) {
				d.replaceData(id, []byte("new"))
				d.beforeMedia = nil
			}
		})

		data, version, found := mustRead(t, newTestStore(t, d))
		if !found || string(data) != "new" || version != id+":"+md5Of([]byte("new")) {
			t.Errorf("Read = %q, %q, %v; want the new bytes with their version", data, version, found)
		}

		if n := d.count(http.MethodGet, filesPath, false); n != 2 {
			t.Errorf("lists = %d, want 2", n)
		}
	})

	t.Run("replaced every time", func(t *testing.T) {
		d := newFakeDrive(t)
		id := d.put(container, []byte("v0"), testNow)
		n := 0

		d.set(func(d *fakeDrive) {
			d.beforeMedia = func(d *fakeDrive) {
				n++
				d.replaceData(id, []byte{'v', byte('0' + n)})
			}
		})

		if err := readErr(t, newTestStore(t, d)); !errors.Is(err, blobremote.ErrVersionMismatch) {
			t.Fatalf("Read: err = %v, want ErrVersionMismatch", err)
		}

		if lists, media := d.count(http.MethodGet, filesPath, false), d.count(http.MethodGet, filesPath+"/"+id, true); lists != 2 || media != 2 {
			t.Errorf("lists = %d, downloads = %d; want 2 and 2 (one repeat)", lists, media)
		}
	})
}

// TestReadFileDeletedAfterList: the file the list named is gone at
// alt=media; the repeat lists again and finds nothing.
func TestReadFileDeletedAfterList(t *testing.T) {
	d := newFakeDrive(t)
	id := d.put(container, []byte("gone"), testNow)
	d.set(func(d *fakeDrive) {
		d.beforeMedia = func(d *fakeDrive) {
			d.remove(id)
			d.beforeMedia = nil
		}
	})

	data, version, found := mustRead(t, newTestStore(t, d))
	if found || data != nil || version != "" {
		t.Errorf("Read = %q, %q, %v; want nothing", data, version, found)
	}
}

func TestReadTooLarge(t *testing.T) {
	t.Run("by the list", func(t *testing.T) {
		d := newFakeDrive(t)
		id := d.put(container, []byte("small"), testNow)
		d.set(func(d *fakeDrive) { d.file(id).size = 64<<20 + 1 })

		if err := readErr(t, newTestStore(t, d)); !errors.Is(err, domain.ErrRemoteDamaged) {
			t.Fatalf("Read: err = %v, want ErrRemoteDamaged", err)
		}

		if n := d.count(http.MethodGet, filesPath+"/"+id, true); n != 0 {
			t.Errorf("downloads = %d, want 0", n)
		}
	})

	t.Run("by the bytes", func(t *testing.T) {
		d := newFakeDrive(t)
		id := d.put(container, make([]byte, 64<<20+1), testNow)
		d.set(func(d *fakeDrive) { d.file(id).size = 10 })

		if err := readErr(t, newTestStore(t, d)); !errors.Is(err, domain.ErrRemoteDamaged) {
			t.Fatalf("Read: err = %v, want ErrRemoteDamaged", err)
		}
	})
}

// TestListPicksEarliestAcrossPages: the effective file is the earliest by
// createdTime among the candidates of all pages, whatever the order of the
// answer; on equal createdTime the smaller ID. Duplicates are logged by
// number, without IDs.
func TestListPicksEarliestAcrossPages(t *testing.T) {
	t.Run("earliest on the last page", func(t *testing.T) {
		d := newFakeDrive(t)
		d.set(func(d *fakeDrive) { d.ignoreOrder, d.pageLimit = true, 2 })

		ids := []string{
			d.put(container, []byte("c"), testNow.Add(3*time.Minute)),
			d.put(container, []byte("b"), testNow.Add(2*time.Minute)),
			d.put(container, []byte("d"), testNow.Add(4*time.Minute)),
			d.put(container, []byte("e"), testNow.Add(5*time.Minute)),
			d.put(container, []byte("a"), testNow.Add(time.Minute)),
		}

		var log logBuffer

		opts := testOptions(d)
		opts.log = slog.New(slog.NewTextHandler(&log, nil))

		data, version, found := mustRead(t, newStore(d.endpoints(), testTokens(), fakeClient, testRefresher(t, d), opts))
		if !found || string(data) != "a" || version != ids[4]+":"+md5Of([]byte("a")) {
			t.Errorf("Read = %q, %q, %v; want the earliest file a", data, version, found)
		}

		if n := d.count(http.MethodGet, filesPath, false); n != 3 {
			t.Errorf("list pages = %d, want 3", n)
		}

		if got := log.String(); !strings.Contains(got, "count=4") {
			t.Errorf("log = %q, want the number of duplicates", got)
		}

		for _, id := range ids {
			if strings.Contains(log.String(), id) {
				t.Errorf("log carries file ID %s", id)
			}
		}
	})

	t.Run("equal createdTime takes the smaller ID", func(t *testing.T) {
		d := newFakeDrive(t)
		d.set(func(d *fakeDrive) { d.ignoreOrder, d.pageLimit = true, 1 })
		d.putID("b-file", container, []byte("b"), testNow)
		d.putID("c-file", container, []byte("c"), testNow.Add(time.Second))
		d.putID("a-file", container, []byte("a"), testNow)

		data, version, _ := mustRead(t, newTestStore(t, d))
		if string(data) != "a" || !strings.HasPrefix(version, "a-file:") {
			t.Errorf("Read = %q, %q; want a-file", data, version)
		}
	})
}

func TestListMoreThanTenPagesIsDamaged(t *testing.T) {
	tests := []struct {
		files int
		err   error
	}{
		{files: 10},
		{files: 11, err: domain.ErrRemoteDamaged},
	}

	for _, tc := range tests {
		d := newFakeDrive(t)
		d.set(func(d *fakeDrive) { d.pageLimit = 1 })

		for i := range tc.files {
			d.put(container, []byte("x"), testNow.Add(time.Duration(i)*time.Second))
		}

		err := readErr(t, newTestStore(t, d))
		if !errors.Is(err, tc.err) {
			t.Errorf("%d pages: err = %v, want %v", tc.files, err, tc.err)
		}

		if n := d.count(http.MethodGet, filesPath, false); n != min(tc.files, 10) {
			t.Errorf("%d pages: lists = %d, want at most 10", tc.files, n)
		}
	}
}

// TestRequestsUseFieldMasks: every request that returns files names the
// fields of §5.1, and every request carries the token.
func TestRequestsUseFieldMasks(t *testing.T) {
	d := newFakeDrive(t)
	d.set(func(d *fakeDrive) { d.pageLimit = 1 })
	d.put(container, []byte("first"), testNow)
	d.put(container, []byte("second"), testNow.Add(time.Second))

	mustRead(t, newTestStore(t, d))

	reqs := d.reqs()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 2 list pages and 1 download", len(reqs))
	}

	for i, r := range reqs {
		if r.auth != "Bearer "+fakeAccess {
			t.Errorf("request %d: Authorization = %q", i, r.auth)
		}

		switch {
		case r.path == filesPath:
			q := r.query
			if q.Get("fields") != listMask || q.Get("spaces") != fakeAppData || q.Get("orderBy") != "createdTime" ||
				q.Get("pageSize") != "100" || q.Get("q") != "name='"+container+"' and trashed=false" {
				t.Errorf("list query = %v", q)
			}

			if (i == 0) != (q.Get("pageToken") == "") {
				t.Errorf("list %d: pageToken = %q", i, q.Get("pageToken"))
			}
		case strings.HasPrefix(r.path, filesPath+"/") && r.query.Get("alt") == "media":
		default:
			t.Errorf("unexpected request %s %s", r.method, r.path)
		}
	}
}

// TestListQueryEscapesName: a quote or a backslash in the name cannot end
// the string of the query.
func TestListQueryEscapesName(t *testing.T) {
	d := newFakeDrive(t)
	name := `it's\odd.bin`
	d.put(name, []byte("odd"), testNow)

	data, _, found, err := newTestStore(t, d).Read(t.Context(), name)
	if err != nil || !found || string(data) != "odd" {
		t.Errorf("Read = %q, %v, %v; want the file", data, found, err)
	}
}

func TestReadReportsDownloadProgress(t *testing.T) {
	d := newFakeDrive(t)
	d.put(container, bytes.Repeat([]byte("x"), 1000), testNow)

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

	mustRead(t, newStore(d.endpoints(), testTokens(), fakeClient, testRefresher(t, d), opts))

	mu.Lock()
	defer mu.Unlock()

	want := usecase.TransferProgress{
		Storage: domain.KindGoogleDrive, Direction: usecase.DirectionDownload, Unit: usecase.UnitBytes, Done: 1000, Total: 1000,
	}

	if len(reports) == 0 || reports[len(reports)-1] != want {
		t.Errorf("reports = %+v, want the last %+v", reports, want)
	}

	if !slices.ContainsFunc(reports, func(p usecase.TransferProgress) bool { return p.Done == 0 }) {
		t.Error("no report at the start")
	}
}
