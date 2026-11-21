// Package gdrive is the Google Drive adapter: the object store over the
// hidden app folder of the Drive v3 API (spec §4, §5) and the parameters of
// the Google sign-in (spec §6).
package gdrive

import (
	"cmp"
	"context"
	"crypto/md5" //nolint:gosec // MD5 is the Drive checksum of a file (spec §5.1), not a security hash
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// maxObject is the largest object read; it must equal the container file
// limit of blobremote (64 MiB).
const maxObject = 64 << 20

// errFileGone is a file of the list that is gone at the download.
var errFileGone = errors.New("listed file is gone")

// Refresher exchanges a refresh token for a new access token, as
// cloudauth.Authorizer does: the port of the refresh of the Google token
// (spec §5.6).
type Refresher interface {
	Refresh(ctx context.Context, c usecase.CloudClient, refreshToken string) (usecase.CloudToken, error)
}

// storeOptions are the seams of a Store; a zero field takes its default.
type storeOptions struct {
	// http sends the requests. Default: system roots, TLS 1.2+, proxy from
	// the environment. The store replaces its redirect policy.
	http     *http.Client
	log      *slog.Logger         // duplicates of the container; default discards
	progress usecase.ProgressFunc // bytes of each transfer; nil reports nothing
	now      func() time.Time     // default time.Now

	// allowHost replaces the check of the hosts that get the token
	// (googleHost); only tests set it, to reach the emulator on 127.0.0.1.
	allowHost func(hostname string) bool
}

// Store is the object store over appDataFolder of Google Drive (spec §5):
// an object is the effective file of its name, its version is
// "<fileId>:<md5Checksum>" (spec §5.1). A Store serves one operation and is
// not safe for concurrent use.
type Store struct {
	api      apiClient
	tokens   usecase.CloudTokenSource
	log      *slog.Logger
	progress usecase.ProgressFunc
	now      func() time.Time
}

// newStore returns a store over the API of e that signs the requests with
// the token of tokens.
func newStore(e Endpoints, tokens usecase.CloudTokenSource, opts storeOptions) *Store {
	s := &Store{
		api:      newAPIClient(opts.http, e.API, opts.allowHost),
		tokens:   tokens,
		log:      cmp.Or(opts.log, slog.New(slog.DiscardHandler)),
		progress: opts.progress,
		now:      opts.now,
	}

	if s.now == nil {
		s.now = time.Now
	}

	return s
}

// Read returns the effective file of name and its version (spec §5.5). The
// version belongs to the bytes: when their MD5 is not the md5Checksum of the
// list, or the listed file is gone, the list and the download are repeated
// once, then it is blobremote.ErrVersionMismatch.
func (s *Store) Read(ctx context.Context, name string) (data []byte, version string, found bool, err error) {
	for range 2 {
		var f driveFile

		data, f, found, err = s.download(ctx, name)

		switch {
		case errors.Is(err, errFileGone):
			continue
		case err != nil || !found:
			return nil, "", false, err
		case f.MD5 != "" && strings.EqualFold(f.MD5, md5Hex(data)):
			return data, f.ID + ":" + strings.ToLower(f.MD5), true, nil
		}
	}

	return nil, "", false, fmt.Errorf("read %s: %w", name, blobremote.ErrVersionMismatch)
}

// download finds the effective file of name and reads its bytes.
func (s *Store) download(ctx context.Context, name string) (data []byte, f driveFile, found bool, err error) {
	files, err := s.list(ctx, name)
	if err != nil || len(files) == 0 {
		return nil, driveFile{}, false, err
	}

	f = files[0]
	if f.Size > maxObject {
		return nil, driveFile{}, false, fmt.Errorf("remote object of %d bytes: %w", f.Size, domain.ErrRemoteDamaged)
	}

	tok, err := s.token(ctx)
	if err != nil {
		return nil, driveFile{}, false, err
	}

	u, err := s.api.url("/files/"+f.ID, url.Values{"alt": {"media"}})
	if err != nil {
		return nil, driveFile{}, false, err
	}

	data, err = s.api.getMedia(ctx, stepDownload, u, tok.Access, maxObject, s.meter(f.Size))

	switch {
	case errors.Is(err, errTooLarge):
		return nil, driveFile{}, false, fmt.Errorf("download: %w", domain.ErrRemoteDamaged)
	case cloudhttp.IsStatus(err, http.StatusNotFound):
		return nil, driveFile{}, false, fmt.Errorf("download: %w", errFileGone)
	case err != nil:
		return nil, driveFile{}, false, fmt.Errorf("download: %w", err)
	}

	return data, f, true, nil
}

// token returns the token for the next request.
func (s *Store) token(ctx context.Context) (usecase.CloudToken, error) {
	t, err := s.tokens.Token(ctx)
	if err != nil {
		return usecase.CloudToken{}, fmt.Errorf("cloud token: %w", err)
	}

	return t, nil
}

// meter starts the count of the bytes of a download of total bytes; nil
// when nobody receives the progress. A body longer than the list says
// counts up to its own length.
func (s *Store) meter(total int64) *cloudhttp.Meter {
	if s.progress == nil {
		return nil
	}

	report := usecase.ThrottleProgress(s.progress, s.now)

	return cloudhttp.StartMeter(func(done int64) {
		total = max(total, done)

		report(usecase.TransferProgress{
			Storage: domain.KindGoogleDrive, Direction: usecase.DirectionDownload, Unit: usecase.UnitBytes,
			Done: done, Total: total,
		})
	})
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data) //nolint:gosec // MD5 is the Drive checksum of a file (spec §5.1), not a security hash
	return hex.EncodeToString(sum[:])
}
