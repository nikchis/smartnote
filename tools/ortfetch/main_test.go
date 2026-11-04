package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	testLibPath    = "onnxruntime-test-1.0.0/lib/libonnxruntime.so.1.0.0"
	testLibContent = "library bytes"
	testTarget     = "linux-amd64"
)

type archiveEntry struct {
	name    string
	content string
	symlink bool
}

func sum(content []byte) string {
	digest := sha256.Sum256(content)

	return hex.EncodeToString(digest[:])
}

func makeTgz(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, entry := range entries {
		header := &tar.Header{Name: entry.name, Mode: 0o755, Size: int64(len(entry.content)), Typeflag: tar.TypeReg}
		if entry.symlink {
			header = &tar.Header{Name: entry.name, Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: entry.content}
		}

		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}

		if !entry.symlink {
			if _, err := tw.Write([]byte(entry.content)); err != nil {
				t.Fatal(err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

func makeZip(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()

	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)

	for _, entry := range entries {
		w, err := zw.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := w.Write([]byte(entry.content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	return buf.Bytes()
}

// serveArchive serves archive at /<name> and counts the requests.
func serveArchive(t *testing.T, name string, archive []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)

		if r.URL.Path != "/"+name {
			http.NotFound(w, r)

			return
		}

		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)

	return server, &requests
}

func writeLock(t *testing.T, lock lockFile) string {
	t.Helper()

	data, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "ort.lock.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func lockFor(url string, archive []byte, lib, libContent string) lockFile {
	return lockFile{
		Version: "1.0.0",
		Targets: map[string]target{
			testTarget: {URL: url, SHA256: sum(archive), Lib: lib, LibSHA256: sum([]byte(libContent))},
		},
	}
}

func assertOnlyFiles(t *testing.T, dir string, want ...string) {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}

	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("files in %s = %v, want %v", dir, got, want)
	}
}

func assertLibrary(t *testing.T, dir string) {
	t.Helper()

	got, err := os.ReadFile(filepath.Join(dir, filepath.Base(testLibPath)))
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != testLibContent {
		t.Errorf("library = %q, want %q", got, testLibContent)
	}

	assertOnlyFiles(t, dir, filepath.Base(testLibPath))
}

func libraryEntries() []archiveEntry {
	return []archiveEntry{
		{name: "onnxruntime-test-1.0.0/include/onnxruntime_c_api.h", content: "header"},
		{name: "onnxruntime-test-1.0.0/lib/libonnxruntime.so", content: "libonnxruntime.so.1.0.0", symlink: true},
		{name: testLibPath, content: testLibContent},
	}
}

func TestRunExtractsVerifiedLibraryFromTgz(t *testing.T) {
	archive := makeTgz(t, libraryEntries())
	server, _ := serveArchive(t, "ort.tgz", archive)
	out := filepath.Join(t.TempDir(), "ort")

	err := run(context.Background(), writeLock(t, lockFor(server.URL+"/ort.tgz", archive, testLibPath, testLibContent)), out, testTarget)
	if err != nil {
		t.Fatal(err)
	}

	assertLibrary(t, out)
}

func TestRunExtractsVerifiedLibraryFromZip(t *testing.T) {
	archive := makeZip(t, []archiveEntry{{name: "onnxruntime-test-1.0.0/lib/other.dll", content: "other"}, {name: testLibPath, content: testLibContent}})
	server, _ := serveArchive(t, "ort.zip", archive)
	out := t.TempDir()

	err := run(context.Background(), writeLock(t, lockFor(server.URL+"/ort.zip", archive, testLibPath, testLibContent)), out, testTarget)
	if err != nil {
		t.Fatal(err)
	}

	assertLibrary(t, out)
}

func TestRunMatchesEntriesWithDotSlashPrefix(t *testing.T) {
	archive := makeTgz(t, []archiveEntry{{name: "./" + testLibPath, content: testLibContent}})
	server, _ := serveArchive(t, "ort.tgz", archive)
	out := t.TempDir()

	err := run(context.Background(), writeLock(t, lockFor(server.URL+"/ort.tgz", archive, testLibPath, testLibContent)), out, testTarget)
	if err != nil {
		t.Fatal(err)
	}

	assertLibrary(t, out)
}

func TestRunRejectsArchiveDigestMismatch(t *testing.T) {
	archive := makeTgz(t, libraryEntries())
	server, _ := serveArchive(t, "ort.tgz", archive)
	lock := lockFor(server.URL+"/ort.tgz", archive, testLibPath, testLibContent)
	entry := lock.Targets[testTarget]
	entry.SHA256 = sum([]byte("another archive"))
	lock.Targets[testTarget] = entry
	out := t.TempDir()

	err := run(context.Background(), writeLock(t, lock), out, testTarget)
	if err == nil || !strings.Contains(err.Error(), "archive SHA-256") {
		t.Fatalf("run() error = %v, want an archive SHA-256 mismatch", err)
	}

	assertOnlyFiles(t, out)
}

func TestRunRejectsLibraryDigestMismatch(t *testing.T) {
	archive := makeTgz(t, libraryEntries())
	server, _ := serveArchive(t, "ort.tgz", archive)
	out := t.TempDir()

	err := run(context.Background(), writeLock(t, lockFor(server.URL+"/ort.tgz", archive, testLibPath, "another library")), out, testTarget)
	if err == nil || !strings.Contains(err.Error(), "library SHA-256") {
		t.Fatalf("run() error = %v, want a library SHA-256 mismatch", err)
	}

	assertOnlyFiles(t, out)
}

func TestRunRejectsPathTraversalInArchive(t *testing.T) {
	for _, name := range []string{"../evil.so", "/abs/evil.so", "onnxruntime-test-1.0.0/../../evil.so", `..\evil.dll`} {
		t.Run(name, func(t *testing.T) {
			archive := makeTgz(t, []archiveEntry{{name: name, content: "evil"}, {name: testLibPath, content: testLibContent}})
			server, _ := serveArchive(t, "ort.tgz", archive)
			out := t.TempDir()

			err := run(context.Background(), writeLock(t, lockFor(server.URL+"/ort.tgz", archive, testLibPath, testLibContent)), out, testTarget)
			if err == nil || !strings.Contains(err.Error(), "unsafe path") {
				t.Fatalf("run() error = %v, want an unsafe path", err)
			}

			assertOnlyFiles(t, out)
		})
	}
}

func TestRunRejectsLibraryThatIsNotARegularFile(t *testing.T) {
	archive := makeTgz(t, []archiveEntry{{name: testLibPath, content: "/etc/passwd", symlink: true}})
	server, _ := serveArchive(t, "ort.tgz", archive)
	out := t.TempDir()

	err := run(context.Background(), writeLock(t, lockFor(server.URL+"/ort.tgz", archive, testLibPath, testLibContent)), out, testTarget)
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("run() error = %v, want a rejected symlink", err)
	}

	assertOnlyFiles(t, out)
}

func TestRunRejectsArchiveWithoutLibrary(t *testing.T) {
	archive := makeZip(t, []archiveEntry{{name: "onnxruntime-test-1.0.0/README.md", content: "readme"}})
	server, _ := serveArchive(t, "ort.zip", archive)
	out := t.TempDir()

	err := run(context.Background(), writeLock(t, lockFor(server.URL+"/ort.zip", archive, testLibPath, testLibContent)), out, testTarget)
	if err == nil || !strings.Contains(err.Error(), "not in the archive") {
		t.Fatalf("run() error = %v, want a missing library", err)
	}

	assertOnlyFiles(t, out)
}

func TestRunKeepsValidLibraryWithoutDownload(t *testing.T) {
	archive := makeTgz(t, libraryEntries())
	server, requests := serveArchive(t, "ort.tgz", archive)
	out := t.TempDir()

	if err := os.WriteFile(filepath.Join(out, filepath.Base(testLibPath)), []byte(testLibContent), 0o600); err != nil {
		t.Fatal(err)
	}

	err := run(context.Background(), writeLock(t, lockFor(server.URL+"/ort.tgz", archive, testLibPath, testLibContent)), out, testTarget)
	if err != nil {
		t.Fatal(err)
	}

	if got := requests.Load(); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}

	assertLibrary(t, out)
}

func TestRunReplacesCorruptedLibrary(t *testing.T) {
	archive := makeTgz(t, libraryEntries())
	server, _ := serveArchive(t, "ort.tgz", archive)
	out := t.TempDir()

	if err := os.WriteFile(filepath.Join(out, filepath.Base(testLibPath)), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := run(context.Background(), writeLock(t, lockFor(server.URL+"/ort.tgz", archive, testLibPath, testLibContent)), out, testTarget)
	if err != nil {
		t.Fatal(err)
	}

	assertLibrary(t, out)
}

func TestRunRejectsUnknownTarget(t *testing.T) {
	archive := makeTgz(t, libraryEntries())
	lock := writeLock(t, lockFor("http://127.0.0.1:1/ort.tgz", archive, testLibPath, testLibContent))

	err := run(context.Background(), lock, t.TempDir(), "plan9-mips")
	if err == nil || !strings.Contains(err.Error(), "plan9-mips") {
		t.Fatalf("run() error = %v, want an unknown target", err)
	}
}

func TestReadLockRejectsInvalidEntries(t *testing.T) {
	valid := target{
		URL:       "https://example.invalid/ort.tgz",
		SHA256:    strings.Repeat("a", 64),
		Lib:       testLibPath,
		LibSHA256: strings.Repeat("b", 64),
	}

	cases := []struct {
		name   string
		mutate func(*target)
		want   string
	}{
		{"lib with parent", func(e *target) { e.Lib = "../lib/libonnxruntime.so" }, "lib"},
		{"absolute lib", func(e *target) { e.Lib = "/lib/libonnxruntime.so" }, "lib"},
		{"lib with backslash", func(e *target) { e.Lib = `lib\onnxruntime.dll` }, "lib"},
		{"short archive digest", func(e *target) { e.SHA256 = "abc" }, "sha256"},
		{"upper-case lib digest", func(e *target) { e.LibSHA256 = strings.Repeat("B", 64) }, "lib_sha256"},
		{"unsupported archive", func(e *target) { e.URL = "https://example.invalid/ort.7z" }, "archive"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry := valid
			tc.mutate(&entry)

			path := writeLock(t, lockFile{Version: "1.0.0", Targets: map[string]target{testTarget: entry}})

			_, err := readLock(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("readLock() error = %v, want one about %s", err, tc.want)
			}
		})
	}
}

func TestTargetName(t *testing.T) {
	cases := []struct{ goos, goarch, want string }{
		{"linux", "amd64", "linux-amd64"},
		{"windows", "amd64", "windows-amd64"},
		{"darwin", "arm64", "darwin-arm64"},
	}

	for _, tc := range cases {
		if got := targetName(tc.goos, tc.goarch); got != tc.want {
			t.Errorf("targetName(%s, %s) = %s, want %s", tc.goos, tc.goarch, got, tc.want)
		}
	}
}

const (
	testLicense = "license text"
	testNotices = "third-party notices"
)

// noticeEntries are the library archive with the notice files beside its lib directory.
func noticeEntries() []archiveEntry {
	return append(libraryEntries(),
		archiveEntry{name: "onnxruntime-test-1.0.0/LICENSE", content: testLicense},
		archiveEntry{name: "onnxruntime-test-1.0.0/ThirdPartyNotices.txt", content: testNotices},
	)
}

func lockWithNotices(url string, archive []byte) lockFile {
	lock := lockFor(url, archive, testLibPath, testLibContent)
	lock.Notices = []string{"LICENSE", "ThirdPartyNotices.txt"}

	return lock
}

func assertLibraryAndNotices(t *testing.T, dir string) {
	t.Helper()

	for name, want := range map[string]string{
		filepath.Base(testLibPath): testLibContent,
		"LICENSE":                  testLicense,
		"ThirdPartyNotices.txt":    testNotices,
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}

		if string(got) != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}

	assertOnlyFiles(t, dir, "LICENSE", "ThirdPartyNotices.txt", filepath.Base(testLibPath))
}

func TestRunExtractsNoticesBesideLibrary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		archive []byte
	}{
		{"ort.tgz", makeTgz(t, noticeEntries())},
		{"ort.zip", makeZip(t, noticeEntries())},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := serveArchive(t, tc.name, tc.archive)
			out := t.TempDir()

			if err := run(context.Background(), writeLock(t, lockWithNotices(server.URL+"/"+tc.name, tc.archive)), out, testTarget); err != nil {
				t.Fatal(err)
			}

			assertLibraryAndNotices(t, out)
		})
	}
}

func TestRunRejectsArchiveWithoutNotice(t *testing.T) {
	archive := makeTgz(t, noticeEntries()[:len(noticeEntries())-1])
	server, _ := serveArchive(t, "ort.tgz", archive)
	out := t.TempDir()

	err := run(context.Background(), writeLock(t, lockWithNotices(server.URL+"/ort.tgz", archive)), out, testTarget)
	if err == nil || !strings.Contains(err.Error(), "ThirdPartyNotices.txt is not in the archive") {
		t.Fatalf("run() error = %v, want a missing notice", err)
	}

	assertOnlyFiles(t, out)
}

func TestRunDownloadsWhenNoticeIsMissing(t *testing.T) {
	archive := makeTgz(t, noticeEntries())
	server, requests := serveArchive(t, "ort.tgz", archive)
	out := t.TempDir()

	if err := os.WriteFile(filepath.Join(out, filepath.Base(testLibPath)), []byte(testLibContent), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := run(context.Background(), writeLock(t, lockWithNotices(server.URL+"/ort.tgz", archive)), out, testTarget); err != nil {
		t.Fatal(err)
	}

	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1", got)
	}

	assertLibraryAndNotices(t, out)
}

func TestRunKeepsLibraryAndNoticesWithoutDownload(t *testing.T) {
	archive := makeTgz(t, noticeEntries())
	server, requests := serveArchive(t, "ort.tgz", archive)
	out := t.TempDir()

	for name, content := range map[string]string{filepath.Base(testLibPath): testLibContent, "LICENSE": testLicense, "ThirdPartyNotices.txt": testNotices} {
		if err := os.WriteFile(filepath.Join(out, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := run(context.Background(), writeLock(t, lockWithNotices(server.URL+"/ort.tgz", archive)), out, testTarget); err != nil {
		t.Fatal(err)
	}

	if got := requests.Load(); got != 0 {
		t.Errorf("requests = %d, want 0", got)
	}

	assertLibraryAndNotices(t, out)
}

func TestReadLockRejectsInvalidNotices(t *testing.T) {
	entry := target{
		URL:       "https://example.invalid/ort.tgz",
		SHA256:    strings.Repeat("a", 64),
		Lib:       testLibPath,
		LibSHA256: strings.Repeat("b", 64),
	}

	for _, notice := range []string{"", ".", "..", "docs/LICENSE", `docs\LICENSE`, "libonnxruntime.so.1.0.0"} {
		t.Run(notice, func(t *testing.T) {
			path := writeLock(t, lockFile{Version: "1.0.0", Notices: []string{notice}, Targets: map[string]target{testTarget: entry}})

			_, err := readLock(path)
			if err == nil || !strings.Contains(err.Error(), "notice") {
				t.Fatalf("readLock() error = %v, want one about the notice", err)
			}
		})
	}
}
