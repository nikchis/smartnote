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
	"sync"
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
// "<fileId>:<md5Checksum>" (spec §5.1). A Store serves one operation; if its
// requests run concurrently they share the token and refresh it once
// (spec §5.6).
type Store struct {
	api       apiClient
	tokens    usecase.CloudTokenSource
	client    usecase.CloudClient // sent with the refresh
	refresher Refresher
	uploads   UploadStore

	// refreshMu serializes the refresh, so requests that share a token do
	// not refresh it twice.
	refreshMu sync.Mutex

	// settleMu guards settled: no record of an unfinished upload waits for
	// its resolution (spec §5.4, 3).
	settleMu sync.Mutex
	settled  bool

	log      *slog.Logger
	progress usecase.ProgressFunc
	now      func() time.Time
}

// newStore returns a store over the API of e that signs the requests with
// the token of tokens and renews it with refresher as the OAuth client
// client (usecase.EffectiveClient of the target). uploads keeps the record
// of the unfinished upload of the device and must not be nil.
func newStore(e Endpoints, tokens usecase.CloudTokenSource, client usecase.CloudClient, refresher Refresher, uploads UploadStore, opts storeOptions) *Store {
	api := newAPIClient(opts.http, e.API, opts.allowHost)
	api.upload = strings.TrimSuffix(e.Upload, "/")

	s := &Store{
		api:       api,
		tokens:    tokens,
		client:    client,
		refresher: refresher,
		uploads:   uploads,
		log:       cmp.Or(opts.log, slog.New(slog.DiscardHandler)),
		progress:  opts.progress,
		now:       opts.now,
	}

	if s.now == nil {
		s.now = time.Now
	}

	return s
}

// Read returns the effective file of name and its version (spec §5.5). The
// version belongs to the bytes: when their MD5 is not the md5Checksum of the
// list, or the listed file is gone, the list and the download are repeated
// once, then it is blobremote.ErrVersionMismatch. An unfinished upload of
// an earlier Write is resolved first (spec §5.4).
func (s *Store) Read(ctx context.Context, name string) (data []byte, version string, found bool, err error) {
	if err := s.settle(ctx); err != nil {
		return nil, "", false, err
	}

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

	u, err := s.api.url("/files/"+f.ID, url.Values{"alt": {"media"}})
	if err != nil {
		return nil, driveFile{}, false, err
	}

	err = s.authorized(ctx, func(access string) (err error) {
		data, err = s.api.getMedia(ctx, stepDownload, u, access, maxObject, s.meter(usecase.DirectionDownload, f.Size))
		return err
	})

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

// tokenMargin is the time before the expiry of the access token at which
// it is refreshed (spec §5.6).
const tokenMargin = time.Minute

// authorized runs call with an access token that is valid for the next
// request (spec §5.6): a token that expires within a minute is refreshed
// first; a 401 answer to call is followed by one refresh and one repeat;
// a second 401 asks for a new sign-in. call must not be the final upload
// request, which is never repeated on the same URI.
func (s *Store) authorized(ctx context.Context, call func(access string) error) error {
	access, err := s.accessToken(ctx)
	if err != nil {
		return err
	}

	err = call(access)
	if !cloudhttp.IsStatus(err, http.StatusUnauthorized) {
		return err
	}

	tok, err := s.renew(ctx, access)
	if err != nil {
		return err
	}

	err = call(tok.Access)
	if cloudhttp.IsStatus(err, http.StatusUnauthorized) {
		return fmt.Errorf("access token refused after a refresh: %w", domain.ReauthRequired(domain.KindGoogleDrive))
	}

	return err
}

// accessToken returns an access token valid for the next request: a token
// that expires within tokenMargin is refreshed first.
func (s *Store) accessToken(ctx context.Context) (string, error) {
	tok, err := s.tokens.Token(ctx)
	if err != nil {
		return "", fmt.Errorf("cloud token: %w", err)
	}

	if tok.Expiry.Sub(s.now()) < tokenMargin {
		if tok, err = s.renew(ctx, tok.Access); err != nil {
			return "", err
		}
	}

	return tok.Access, nil
}

// renew returns a new token for the one whose access token is stale and
// keeps it in the token source. When another request has already replaced
// the stale token, that one is returned and nothing is refreshed.
func (s *Store) renew(ctx context.Context, stale string) (usecase.CloudToken, error) {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()

	cur, err := s.tokens.Token(ctx)
	if err != nil {
		return usecase.CloudToken{}, fmt.Errorf("cloud token: %w", err)
	}

	if cur.Access != stale {
		return cur, nil
	}

	// Without a refresh token or an OAuth client only a new sign-in helps.
	if cur.Refresh == "" || s.client.ID == "" {
		return usecase.CloudToken{}, domain.ReauthRequired(domain.KindGoogleDrive)
	}

	next, err := s.refresher.Refresh(ctx, s.client, cur.Refresh)
	if err != nil {
		return usecase.CloudToken{}, err
	}

	// Update keeps the token as given: what the answer lacks stays as it was.
	next.Provider, next.ClientID = cur.Provider, cur.ClientID
	next.Refresh = cmp.Or(next.Refresh, cur.Refresh)
	next.Account = cmp.Or(next.Account, cur.Account)

	if err := s.tokens.Update(ctx, next); err != nil {
		if _, ok := errors.AsType[usecase.LocalError](err); !ok {
			err = usecase.LocalError{Err: err}
		}

		return usecase.CloudToken{}, fmt.Errorf("save refreshed token: %w", err)
	}

	return next, nil
}

// meter starts the count of the bytes of a transfer of total bytes in
// direction; nil when nobody receives the progress. A body longer than the
// list says counts up to its own length.
func (s *Store) meter(direction string, total int64) *cloudhttp.Meter {
	if s.progress == nil {
		return nil
	}

	report := usecase.ThrottleProgress(s.progress, s.now)

	return cloudhttp.StartMeter(func(done int64) {
		total = max(total, done)

		report(usecase.TransferProgress{
			Storage: domain.KindGoogleDrive, Direction: direction, Unit: usecase.UnitBytes,
			Done: done, Total: total,
		})
	})
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data) //nolint:gosec // MD5 is the Drive checksum of a file (spec §5.1), not a security hash
	return hex.EncodeToString(sum[:])
}
