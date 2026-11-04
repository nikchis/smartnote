// Command ortfetch downloads the ONNX Runtime archive of one target listed in ort.lock.json,
// checks its SHA-256, extracts the runtime library and the notice files (the lock's "notices",
// next to the library's directory in the archive, e.g. LICENSE) into a directory and checks the
// library's SHA-256; the notices are covered by the archive digest. A library already present
// with the expected digest is kept when every notice file is present too; otherwise the archive
// is fetched again, and nothing that fails a check is left behind.
//
// Usage: go run ./tools/ortfetch [-lock ort.lock.json] [-out build/ort] [-target linux-amd64]
//
// The target defaults to the current OS and architecture (GOOS-GOARCH).
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

const (
	// downloadTimeout bounds the archive download; the Windows archive is about 80 MB.
	downloadTimeout = 30 * time.Minute
	// maxLibrary bounds each extracted file (the library is about 45 MB on macOS) against
	// archive bombs.
	maxLibrary = 256 << 20

	kindTgz = "tgz"
	kindZip = "zip"
)

// target is the archive of one OS and architecture and the library inside it.
type target struct {
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Lib       string `json:"lib"`
	LibSHA256 string `json:"lib_sha256"`
}

type lockFile struct {
	Version string `json:"version"`
	// Notices are file names in the top directory of every archive (the first element of the
	// lib path) that are extracted beside the library: the licence texts shipped with it.
	Notices []string          `json:"notices"`
	Targets map[string]target `json:"targets"`
}

func main() {
	lock := flag.String("lock", "ort.lock.json", "lock file with the archive URLs and SHA-256 digests")
	out := flag.String("out", filepath.Join("build", "ort"), "directory to extract the library to")
	name := flag.String("target", targetName(runtime.GOOS, runtime.GOARCH), "target of the lock file")

	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, *lock, *out, *name)

	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, "ortfetch:", err)
		os.Exit(1)
	}
}

// targetName is the lock file key of an OS and architecture.
func targetName(goos, goarch string) string {
	return goos + "-" + goarch
}

// run makes outDir hold the verified library of target name.
func run(ctx context.Context, lockPath, outDir, name string) error {
	lock, err := readLock(lockPath)
	if err != nil {
		return err
	}

	entry, ok := lock.Targets[name]
	if !ok {
		return fmt.Errorf("%s: no target %s", lockPath, name)
	}

	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}

	libPath := filepath.Join(outDir, path.Base(entry.Lib))
	if got, err := fileSHA256(libPath); err == nil && got == entry.LibSHA256 && filesExist(outDir, lock.Notices) {
		fmt.Println("ok", libPath)

		return nil
	}

	if err := os.Remove(libPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return fetchLibrary(ctx, entry, lock.Notices, outDir, libPath)
}

// filesExist reports whether every one of names is a regular file in dir.
func filesExist(dir string, names []string) bool {
	for _, name := range names {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}

	return true
}

func readLock(lockPath string) (lockFile, error) {
	var lock lockFile

	data, err := os.ReadFile(lockPath)
	if err != nil {
		return lock, err
	}

	if err := json.Unmarshal(data, &lock); err != nil {
		return lock, fmt.Errorf("%s: %w", lockPath, err)
	}

	if lock.Version == "" || len(lock.Targets) == 0 {
		return lock, fmt.Errorf("%s: version and targets are required", lockPath)
	}

	names := make([]string, 0, len(lock.Targets))
	for name := range lock.Targets {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		if err := checkTarget(lock.Targets[name], lock.Notices); err != nil {
			return lock, fmt.Errorf("%s: %s: %w", lockPath, name, err)
		}
	}

	return lock, nil
}

func checkTarget(entry target, notices []string) error {
	if archiveKind(entry.URL) == "" {
		return fmt.Errorf("unsupported archive %q (want .tgz, .tar.gz or .zip)", entry.URL)
	}

	if !isDigest(entry.SHA256) {
		return fmt.Errorf("sha256 %q must be 64 lower-case hex digits", entry.SHA256)
	}

	if !isDigest(entry.LibSHA256) {
		return fmt.Errorf("lib_sha256 %q must be 64 lower-case hex digits", entry.LibSHA256)
	}

	if clean, ok := safeName(entry.Lib); !ok || clean != entry.Lib || path.Base(clean) == clean {
		return fmt.Errorf("lib %q must be a clean relative path inside a directory", entry.Lib)
	}

	seen := map[string]bool{path.Base(entry.Lib): true}
	for _, notice := range notices {
		if !filepath.IsLocal(notice) || notice == "." || strings.ContainsAny(notice, `/\`) || seen[notice] {
			return fmt.Errorf("notice %q must be a distinct plain file name", notice)
		}

		seen[notice] = true
	}

	return nil
}

func isDigest(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}

	return strings.Trim(s, "0123456789abcdef") == ""
}

// archiveKind is kindTgz or kindZip by the URL's extension, or "" for another one.
func archiveKind(rawURL string) string {
	switch {
	case strings.HasSuffix(rawURL, ".tgz"), strings.HasSuffix(rawURL, ".tar.gz"):
		return kindTgz
	case strings.HasSuffix(rawURL, ".zip"):
		return kindZip
	default:
		return ""
	}
}

// safeName returns the archive entry name cleaned (which drops "./" prefixes) and whether it
// stays inside the archive root: not absolute, no "..", no backslash.
func safeName(name string) (string, bool) {
	if strings.Contains(name, `\`) || path.IsAbs(name) {
		return "", false
	}

	clean := path.Clean(name)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}

	return clean, true
}

// fetchLibrary downloads the archive of entry into outDir, checks it, extracts the library to
// libPath and the notices into outDir. The archive is removed afterwards.
func fetchLibrary(ctx context.Context, entry target, notices []string, outDir, libPath string) error {
	archive, err := os.CreateTemp(outDir, "archive-*.part")
	if err != nil {
		return err
	}

	defer func() {
		_ = archive.Close()
		_ = os.Remove(archive.Name())
	}()

	fmt.Println("downloading", entry.URL)

	got, err := download(ctx, entry.URL, archive)
	if err != nil {
		return err
	}

	if got != entry.SHA256 {
		return fmt.Errorf("archive SHA-256 %s, expected %s", got, entry.SHA256)
	}

	// Every file is extracted to <dst>.part and renamed only after all checks passed.
	wanted := map[string]string{entry.Lib: libPath}

	root := strings.SplitN(entry.Lib, "/", 2)[0]
	for _, notice := range notices {
		wanted[root+"/"+notice] = filepath.Join(outDir, notice)
	}

	err = extract(archive, archiveKind(entry.URL), wanted)
	if err == nil {
		got, err = fileSHA256(libPath + ".part")
		if err == nil && got != entry.LibSHA256 {
			err = fmt.Errorf("library SHA-256 %s, expected %s", got, entry.LibSHA256)
		}
	}

	for _, dst := range wanted {
		if err == nil {
			err = os.Rename(dst+".part", dst)
		}

		_ = os.Remove(dst + ".part")
	}

	if err != nil {
		return err
	}

	fmt.Println("extracted", libPath)

	return nil
}

// download writes the content at the http(s) rawURL to file and returns its SHA-256.
func download(ctx context.Context, rawURL string, file *os.File) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}

	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", fmt.Errorf("unsupported URL scheme %q in %s", parsed.Scheme, rawURL)
	}

	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return "", err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(file, hash), resp.Body); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// extract copies each regular file of the archive named by a key of wanted to the value of that
// key plus ".part"; every wanted file must be in the archive once. Every entry name of the
// archive must be safe (see safeName), including the entries that are not extracted.
func extract(archive *os.File, kind string, wanted map[string]string) error {
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return err
	}

	found := make(map[string]bool, len(wanted))

	var err error
	if kind == kindZip {
		err = extractZip(archive, wanted, found)
	} else {
		err = extractTgz(archive, wanted, found)
	}

	if err != nil {
		return err
	}

	names := make([]string, 0, len(wanted))
	for name := range wanted {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		if !found[name] {
			return fmt.Errorf("%s is not in the archive", name)
		}
	}

	return nil
}

func extractTgz(archive io.Reader, wanted map[string]string, found map[string]bool) error {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return err
		}

		name, ok := safeName(header.Name)
		if !ok {
			return fmt.Errorf("archive entry %q: unsafe path", header.Name)
		}

		dst, ok := wanted[name]
		if !ok {
			continue
		}

		if header.Typeflag != tar.TypeReg || found[name] {
			return fmt.Errorf("archive entry %q: not a regular file", header.Name)
		}

		if err := copyLimited(dst+".part", tr); err != nil {
			return err
		}

		found[name] = true
	}

	return nil
}

func extractZip(archive *os.File, wanted map[string]string, found map[string]bool) error {
	info, err := archive.Stat()
	if err != nil {
		return err
	}

	zr, err := zip.NewReader(archive, info.Size())
	if err != nil {
		return err
	}

	matches := make(map[string]*zip.File, len(wanted))

	for _, file := range zr.File {
		name, ok := safeName(file.Name)
		if !ok {
			return fmt.Errorf("archive entry %q: unsafe path", file.Name)
		}

		if _, ok := wanted[name]; !ok {
			continue
		}

		if !file.Mode().IsRegular() || matches[name] != nil {
			return fmt.Errorf("archive entry %q: not a regular file", file.Name)
		}

		matches[name] = file
	}

	for name, file := range matches {
		if err := copyZipFile(file, wanted[name]+".part"); err != nil {
			return err
		}

		found[name] = true
	}

	return nil
}

func copyZipFile(file *zip.File, dst string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()

	return copyLimited(dst, src)
}

// copyLimited writes src to a new file dst, failing when src exceeds maxLibrary.
func copyLimited(dst string, src io.Reader) error {
	file, err := os.Create(dst)
	if err != nil {
		return err
	}

	n, err := io.Copy(file, io.LimitReader(src, maxLibrary+1))

	if closeErr := file.Close(); err == nil {
		err = closeErr
	}

	if err == nil && n > maxLibrary {
		err = fmt.Errorf("library exceeds %d bytes", maxLibrary)
	}

	return err
}

func fileSHA256(name string) (string, error) {
	file, err := os.Open(name)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}
