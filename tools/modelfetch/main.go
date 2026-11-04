// Command modelfetch downloads the search model files listed in model.lock.json into a
// directory and checks their SHA-256. A file already present with the expected digest is
// kept; a file that does not match is replaced, and a download that does not match is
// removed, so the directory never holds a file that looks valid but is not.
//
// Usage: go run ./tools/modelfetch [-lock model.lock.json] [-out build/model]
//
// MODEL_BASE_URL overrides base_url of the lock file (for local work, e.g. file:///dir/).
package main

import (
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
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

// downloadTimeout bounds one file download; the model is about 140 MB.
const downloadTimeout = 30 * time.Minute

type lockFile struct {
	Release string            `json:"release"`
	BaseURL string            `json:"base_url"`
	Files   map[string]string `json:"files"`
}

func main() {
	lock := flag.String("lock", "model.lock.json", "lock file with the release URL and SHA-256 of the files")
	out := flag.String("out", filepath.Join("build", "model"), "directory to download the files to")

	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, *lock, *out, os.Getenv("MODEL_BASE_URL"))

	stop()

	if err != nil {
		fmt.Fprintln(os.Stderr, "modelfetch:", err)
		os.Exit(1)
	}
}

// run downloads every file of the lock file from baseOverride, or from its base_url when
// baseOverride is empty, into outDir.
func run(ctx context.Context, lockPath, outDir, baseOverride string) error {
	lock, err := readLock(lockPath)
	if err != nil {
		return err
	}

	base := lock.BaseURL
	if baseOverride != "" {
		base = baseOverride
	}

	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return err
	}

	names := make([]string, 0, len(lock.Files))
	for name := range lock.Files {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		if err := fetchFile(ctx, base+name, filepath.Join(outDir, name), lock.Files[name]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	return nil
}

func readLock(path string) (lockFile, error) {
	var lock lockFile

	data, err := os.ReadFile(path)
	if err != nil {
		return lock, err
	}

	if err := json.Unmarshal(data, &lock); err != nil {
		return lock, fmt.Errorf("%s: %w", path, err)
	}

	if lock.BaseURL == "" || len(lock.Files) == 0 {
		return lock, fmt.Errorf("%s: base_url and files are required", path)
	}

	for name := range lock.Files {
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\:`) {
			return lock, fmt.Errorf("%s: file name %q must not contain a path", path, name)
		}
	}

	return lock, nil
}

// fetchFile makes path hold the content with SHA-256 want, downloading it from rawURL
// unless path already has it.
func fetchFile(ctx context.Context, rawURL, path, want string) error {
	if got, err := fileSHA256(path); err == nil && got == want {
		fmt.Println("ok", path)

		return nil
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	fmt.Println("downloading", rawURL)

	partial := path + ".part"

	got, err := download(ctx, rawURL, partial)
	if err == nil && got != want {
		err = fmt.Errorf("SHA-256 %s, expected %s", got, want)
	}

	if err != nil {
		_ = os.Remove(partial)

		return err
	}

	return os.Rename(partial, path)
}

// download writes the content at rawURL to path and returns its SHA-256.
func download(ctx context.Context, rawURL, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	body, err := open(ctx, rawURL)
	if err != nil {
		return "", err
	}
	defer body.Close()

	file, err := os.Create(path)
	if err != nil {
		return "", err
	}

	hash := sha256.New()
	_, err = io.Copy(io.MultiWriter(file, hash), body)

	if closeErr := file.Close(); err == nil {
		err = closeErr
	}

	if err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// open returns the content at an http(s) or file URL.
func open(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	switch parsed.Scheme {
	case "file":
		return os.Open(localPath(parsed)) //nolint:gosec // the URL comes from the repository lock file or MODEL_BASE_URL
	case "http", "https":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody) //nolint:gosec // the URL comes from the repository lock file or MODEL_BASE_URL
		if err != nil {
			return nil, err
		}

		resp, err := http.DefaultClient.Do(req) //nolint:gosec // the URL comes from the repository's lock file or MODEL_BASE_URL
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()

			return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
		}

		return resp.Body, nil
	default:
		return nil, fmt.Errorf("unsupported URL scheme %q in %s", parsed.Scheme, rawURL)
	}
}

// localPath turns a file URL path into an OS path: /tmp/x stays, /C:/x becomes C:\x on Windows.
func localPath(fileURL *url.URL) string {
	path := fileURL.Path
	if runtime.GOOS == "windows" && len(path) > 2 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}

	return filepath.FromSlash(path)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
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
