package yadisk

import (
	"cmp"
	"context"
	"crypto/md5" //nolint:gosec // MD5 is the Disk API's checksum of a file, not a security hash
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

const (
	// maxObject is the largest object read or written; it must equal the
	// container file limit of blobremote (64 MiB).
	maxObject = 64 << 20

	appRoot   = "app:/"
	tempInfix = ".tmp-"

	resourcesPath = "/v1/disk/resources"
	downloadPath  = "/v1/disk/resources/download"
	uploadPath    = "/v1/disk/resources/upload"
	movePath      = "/v1/disk/resources/move"

	fenceTimeout = 5 * time.Second // each step of the fence (spec §5.5)
	pollFirst    = 500 * time.Millisecond
	pollMax      = 2 * time.Second
	tempMaxAge   = time.Hour // older temporary files are left by failed Backups
	listPageSize = 100
	listMaxPages = 10
	maxCleanups  = 20 // deletions of old temporary files per Write

	opSuccess = "success"
	opFailed  = "failed"

	// Query parameters.
	qPath      = "path"
	qFields    = "fields"
	qOverwrite = "overwrite"
)

var (
	errNoPendingStore  = errors.New("yandex disk store without a pending store")
	errBadPendingName  = errors.New("pending temporary file name is not valid")
	errChecksum        = errors.New("uploaded file differs from the data")
	errOperationFailed = errors.New("disk operation failed")
)

// PendingStore keeps the name of the temporary file whose move was sent
// until the outcome is definite, or until a fence settles it (spec §5.5).
// local.db implements it.
type PendingStore interface {
	LoadPendingTemp(ctx context.Context) (name string, found bool, err error)
	SavePendingTemp(ctx context.Context, name string) error
	ClearPendingTemp(ctx context.Context) error
}

// StoreOptions are the seams of NewStore; a zero field takes its default.
type StoreOptions struct {
	// HTTP sends the API and OAuth requests, Href the uploads and downloads
	// by link. Default: system roots, TLS 1.2+, proxy from the environment.
	// The store replaces their redirect policy.
	HTTP, Href *http.Client
	Now        func() time.Time                           // default time.Now
	Sleep      func(context.Context, time.Duration) error // default waits on a timer or ctx
	NewID      func() uuid.UUID                           // names temporary files; default uuid.New
	Log        *slog.Logger                               // failures of cleanup and of the pending record; default discards
	// Progress receives the bytes of each upload and download, at most 10
	// reports a second by Now and always the last one; nil reports nothing.
	Progress usecase.ProgressFunc

	// allowLinkHost replaces the check of the hosts of upload and download
	// links (yandex.ru, yandex.net and their subdomains); only tests set it,
	// to reach the emulator on 127.0.0.1.
	allowLinkHost func(hostname string) bool
}

// Store is the blobremote.ObjectStore over the app folder of Yandex Disk
// (spec §5.4): an object is the file app:/<name>, its version is the file's
// MD5. A Store serves one operation and is not safe for concurrent use.
type Store struct {
	api      apiClient
	href     hrefClient
	tokens   usecase.CloudTokenSource
	pending  PendingStore
	now      func() time.Time
	sleep    func(context.Context, time.Duration) error
	newID    func() uuid.UUID
	log      *slog.Logger
	progress usecase.ProgressFunc

	memory *pendingMemory // temporary files to fence that pending could not record

	settled bool // no temporary file of an earlier Write waits for its fence
	reauth  bool // the token was refused for good; no more requests
}

// NewStore returns a store that signs API requests with the token of tokens
// and keeps the pending record in pending, which must not be nil.
func NewStore(e Endpoints, tokens usecase.CloudTokenSource, pending PendingStore, opts StoreOptions) *Store {
	s := &Store{
		api:      newAPIClient(opts.HTTP, e.API),
		href:     newHrefClient(opts.Href, opts.allowLinkHost),
		tokens:   tokens,
		pending:  pending,
		now:      opts.Now,
		sleep:    opts.Sleep,
		newID:    opts.NewID,
		log:      cmp.Or(opts.Log, slog.New(slog.DiscardHandler)),
		progress: opts.Progress,
		memory:   &pendingMemory{},
	}

	if s.now == nil {
		s.now = time.Now
	}

	if s.sleep == nil {
		s.sleep = sleepCtx
	}

	if s.newID == nil {
		s.newID = uuid.New
	}

	return s
}

// FactoryOption changes the stores a factory builds.
type FactoryOption func(*StoreOptions)

// WithLog makes the stores write the failures of their cleanup to log.
func WithLog(log *slog.Logger) FactoryOption {
	return func(o *StoreOptions) { o.Log = log }
}

// WithProgress makes the stores report the bytes of their uploads and
// downloads to f.
func WithProgress(f usecase.ProgressFunc) FactoryOption {
	return func(o *StoreOptions) { o.Progress = f }
}

// NewStoreFactory builds a Store per operation over the target's token.
func NewStoreFactory(e Endpoints, pending PendingStore, options ...FactoryOption) blobremote.StoreFactory {
	var opts StoreOptions
	for _, o := range options {
		o(&opts)
	}

	return newStoreFactory(e, pending, opts)
}

// newStoreFactory is NewStoreFactory with the seams of the stores. Its
// stores share one pendingMemory.
func newStoreFactory(e Endpoints, pending PendingStore, opts StoreOptions) blobremote.StoreFactory {
	memory := &pendingMemory{}

	return func(t usecase.RemoteTarget) (blobremote.ObjectStore, error) {
		if t.Token == nil {
			return nil, domain.ErrReauthRequired
		}

		if pending == nil {
			return nil, errNoPendingStore
		}

		s := NewStore(e, t.Token, pending, opts)
		s.memory = memory

		return s, nil
	}
}

// pendingMemory keeps, for the life of the process, the temporary files
// whose pending record could not be saved, so that a later store fences them
// when both the record and the DELETE failed (spec §5.5).
type pendingMemory struct {
	mu    sync.Mutex
	names []string
}

func (m *pendingMemory) add(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !slices.Contains(m.names, name) {
		m.names = append(m.names, name)
	}
}

func (m *pendingMemory) remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.names = slices.DeleteFunc(m.names, func(n string) bool { return n == name })
}

func (m *pendingMemory) list() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.names)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type resource struct {
	Type string `json:"type"`
	Size int64  `json:"size"`
	MD5  string `json:"md5"`
}

type link struct {
	Href string `json:"href"`
}

// Read returns the file app:/<name> and its MD5 (spec §5.4).
func (s *Store) Read(ctx context.Context, name string) (data []byte, version string, found bool, err error) {
	if err := s.settle(ctx); err != nil {
		return nil, "", false, err
	}

	path := appRoot + name

	// Bytes that do not match the md5 are a file replaced during the
	// download: one repeat.
	for range 2 {
		var sum string

		data, sum, found, err = s.download(ctx, path)
		if err != nil || !found {
			return nil, "", false, err
		}

		if sum != "" && strings.EqualFold(sum, md5Hex(data)) {
			return data, strings.ToLower(sum), true, nil
		}
	}

	return nil, "", false, fmt.Errorf("read %s: %w", name, blobremote.ErrVersionMismatch)
}

// download reads the metadata of path and then its bytes.
func (s *Store) download(ctx context.Context, path string) (data []byte, sum string, found bool, err error) {
	r, found, err := s.meta(ctx, stepStat, path, "md5,size,type")
	if err != nil || !found {
		return nil, "", false, err
	}

	if r.Type == "dir" || r.Size > maxObject {
		return nil, "", false, fmt.Errorf("remote object of %d bytes: %w", r.Size, domain.ErrRemoteDamaged)
	}

	var l link
	if err := s.get(ctx, stepDownloadLink, downloadPath, url.Values{qPath: {path}}, &l); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, "", false, nil
		}

		return nil, "", false, fmt.Errorf("download link: %w", err)
	}

	data, err = s.href.get(ctx, l.Href, maxObject, s.meter(usecase.DirectionDownload, r.Size))
	if errors.Is(err, errTooLarge) {
		return nil, "", false, fmt.Errorf("download: %w", domain.ErrRemoteDamaged)
	}

	if err != nil {
		return nil, "", false, fmt.Errorf("download: %w", err)
	}

	return data, r.MD5, true, nil
}

// meta returns the metadata fields of path, the request of step; found is
// false on 404.
func (s *Store) meta(ctx context.Context, step, path, fields string) (r resource, found bool, err error) {
	err = s.get(ctx, step, resourcesPath, url.Values{qPath: {path}, qFields: {fields}}, &r)
	if isStatus(err, http.StatusNotFound) {
		return resource{}, false, nil
	}

	if err != nil {
		return resource{}, false, fmt.Errorf("metadata: %w", err)
	}

	return r, true, nil
}

// Write replaces app:/<name> with data through a temporary file if its MD5
// is expect (spec §5.4).
func (s *Store) Write(ctx context.Context, name string, data []byte, expect string) error {
	if err := s.settle(ctx); err != nil {
		return err
	}

	path := appRoot + name
	tmpName := name + tempInfix + s.newID().String()
	sum := md5Hex(data)

	if err := s.stage(ctx, appRoot+tmpName, path, data, sum, expect); err != nil {
		// The move was not sent, so nobody else takes the temporary file.
		s.removeTemp(ctx, appRoot+tmpName)
		return err
	}

	if err := s.replace(ctx, tmpName, path, sum, expect == ""); err != nil {
		return err
	}

	s.cleanup(ctx, name)
	// No trash cleanup: the app-folder permission cannot see the trash (spec §14).

	return nil
}

// stage uploads data to tmp and checks the version of path: steps 1–4.
func (s *Store) stage(ctx context.Context, tmp, path string, data []byte, sum, expect string) error {
	if err := s.upload(ctx, tmp, data); err != nil {
		return err
	}

	if err := s.waitMD5(ctx, tmp, sum); err != nil {
		return err
	}

	return s.checkVersion(ctx, path, expect)
}

// upload puts data to tmp. A PUT that failed with a network error, such as
// a connection the upload node lost or did not accept, gets one more upload
// with a new link, which usually leads to another node (spec §5.4, §9). The
// failed PUT may have stored the file all the same, so the retry may
// overwrite tmp: it is the Write's own file with the same data.
func (s *Store) upload(ctx context.Context, tmp string, data []byte) error {
	err := s.uploadTo(ctx, tmp, data, false)
	if !lostUpload(ctx, err) {
		return err
	}

	s.log.Warn("yandex disk: upload retried with a new link", applog.Err(err))

	return s.uploadTo(ctx, tmp, data, true)
}

// lostUpload reports whether err is a network error of the PUT to the
// upload link while the operation still runs.
func lostUpload(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}

	ce, ok := errors.AsType[CloudError](err)

	return ok && ce.Code == codeNetwork && ce.Step == stepUpload
}

// uploadTo gets an upload link of tmp and puts data to it.
func (s *Store) uploadTo(ctx context.Context, tmp string, data []byte, overwrite bool) error {
	var l link

	q := url.Values{qPath: {tmp}, qOverwrite: {strconv.FormatBool(overwrite)}}
	if err := s.get(ctx, stepUploadLink, uploadPath, q, &l); err != nil {
		return fmt.Errorf("upload link: %w", err)
	}

	if err := s.href.put(ctx, l.Href, data, s.meter(usecase.DirectionUpload, int64(len(data)))); err != nil {
		return fmt.Errorf("upload: %w", err)
	}

	return nil
}

// waitMD5 waits for the Disk to report the md5 of tmp, which an upload
// answered with 202 gets later, and checks it against sum.
func (s *Store) waitMD5(ctx context.Context, tmp, sum string) error {
	for delay := pollFirst; ; delay = min(2*delay, pollMax) {
		r, found, err := s.meta(ctx, stepWaitMD5, tmp, "md5")
		if err != nil {
			return fmt.Errorf("uploaded file: %w", err)
		}

		if found && r.MD5 != "" {
			if !strings.EqualFold(r.MD5, sum) {
				return CloudError{Code: codeOther, Step: stepWaitMD5, err: errChecksum}
			}

			return nil
		}

		if err := s.sleep(ctx, delay); err != nil {
			return fmt.Errorf("wait for the uploaded file: %w", err)
		}
	}
}

// checkVersion returns ErrVersionMismatch unless path has the md5 expect, or
// is missing when expect is "".
func (s *Store) checkVersion(ctx context.Context, path, expect string) error {
	r, found, err := s.meta(ctx, stepStat, path, "md5")
	if err != nil {
		return err
	}

	if found != (expect != "") || found && !strings.EqualFold(r.MD5, expect) {
		return fmt.Errorf("check version: %w", blobremote.ErrVersionMismatch)
	}

	return nil
}

// moveResult is what a move request says about the temporary file.
type moveResult int

const (
	moveDone     moveResult = iota // tmp replaced the object
	moveRejected                   // nothing moved: tmp stays ours
	moveUnknown                    // the move may still be applied
)

// replace moves tmp over path (step 5) and settles an unknown outcome with
// the fence. The temporary file is recorded before the move is sent, so
// that an app killed during the move fences it on the next start, and the
// record is cleared once the outcome is definite.
func (s *Store) replace(ctx context.Context, tmpName, path, sum string, create bool) error {
	// The operation's context may end at any moment; the local steps get
	// their own deadline.
	base := context.WithoutCancel(ctx)
	s.recordPending(base, tmpName)

	res, err := s.move(ctx, appRoot+tmpName, path, create)

	switch res {
	case moveDone:
		s.clearPending(base, tmpName)
		return nil
	case moveRejected:
		// A temporary file left on the Disk keeps its record: the next fence
		// deletes it.
		if s.deleteTemp(base, appRoot+tmpName) == nil {
			s.clearPending(base, tmpName)
		}

		if create && isStatus(err, http.StatusConflict) {
			return fmt.Errorf("move: %w", blobremote.ErrVersionMismatch)
		}

		return fmt.Errorf("move: %w", err)
	default:
		return s.fence(base, tmpName, path, sum, err)
	}
}

// recordPending saves tmpName in the pending store within fenceTimeout.
// Without the record, the stores of this factory remember the file instead.
func (s *Store) recordPending(ctx context.Context, tmpName string) {
	err := s.withDeadline(ctx, func(ctx context.Context) error { return s.pending.SavePendingTemp(ctx, tmpName) })
	if err != nil {
		s.memory.add(tmpName)
		s.log.Error("yandex disk: pending temporary file not recorded", applog.Err(err))
	}
}

// clearPending forgets tmpName once no move can take it any more. A stale
// record only makes the next fence delete a missing file.
func (s *Store) clearPending(ctx context.Context, tmpName string) {
	s.memory.remove(tmpName)

	if err := s.withDeadline(ctx, s.pending.ClearPendingTemp); err != nil {
		s.log.Error("yandex disk: pending temporary file not cleared", applog.Err(err))
	}
}

func (s *Store) move(ctx context.Context, tmp, path string, create bool) (moveResult, error) {
	u, err := s.api.url(movePath, url.Values{"from": {tmp}, "path": {path}, qOverwrite: {strconv.FormatBool(!create)}})
	if err != nil {
		return moveRejected, err
	}

	var l link

	status, err := s.call(ctx, stepMove, http.MethodPost, u, &l)
	if err != nil {
		// The context may end while the request is on the way: the move may
		// still be applied, so this is an unknown outcome too.
		if mayBeApplied(err) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return moveUnknown, err
		}

		return moveRejected, err
	}

	if status != http.StatusAccepted {
		return moveDone, nil
	}

	done, err := s.waitOperation(ctx, stepMove, l.Href)

	switch {
	case err != nil:
		return moveUnknown, err
	case !done:
		return moveRejected, CloudError{Code: codeOther, Step: stepMove, err: errOperationFailed}
	}

	return moveDone, nil
}

// mayBeApplied reports whether a failed request may still take effect: it
// got no answer, the server failed while handling it, or it was accepted
// with a body that could not be read. Other answers and failures before
// sending (token, 401) are known to change nothing.
func mayBeApplied(err error) bool {
	ce, ok := errors.AsType[CloudError](err)
	accepted := ce.Status >= http.StatusOK && ce.Status < http.StatusMultipleChoices

	return ok && (ce.Status == 0 || accepted || ce.Status >= http.StatusInternalServerError)
}

// waitOperation polls the operation link of a 202 answer to the request of
// step until the operation ends; done reports its success. An error means
// the poll failed.
func (s *Store) waitOperation(ctx context.Context, step, href string) (done bool, err error) {
	u, err := s.api.link(href)
	if err != nil {
		return false, err
	}

	for delay := pollFirst; ; delay = min(2*delay, pollMax) {
		if err := s.sleep(ctx, delay); err != nil {
			return false, err
		}

		var op struct {
			Status string `json:"status"`
		}

		if _, err := s.call(ctx, step, http.MethodGet, u, &op); err != nil {
			return false, fmt.Errorf("operation status: %w", err)
		}

		switch op.Status {
		case opSuccess:
			return true, nil
		case opFailed:
			return false, nil
		}
	}
}

// fence settles a move with an unknown outcome (spec §5.5): with tmp
// deleted the move cannot be applied later, and the md5 of path tells
// whether it was applied. cause is the failure that left the outcome
// unknown. replace recorded tmpName already; base is not cancelled, and
// each step gets its own deadline.
func (s *Store) fence(base context.Context, tmpName, path, sum string, cause error) error {
	if err := s.fenceTemp(base, tmpName); err != nil {
		s.settled = false // the record stays: the next Read or Write fences again
		return err
	}

	s.clearPending(base, tmpName)

	var (
		r     resource
		found bool
	)

	err := s.withDeadline(base, func(ctx context.Context) (err error) {
		r, found, err = s.meta(ctx, stepFence, path, "md5")
		return err
	})
	if err != nil {
		return fmt.Errorf("check the fenced move: %w", blobremote.ErrOutcomeUnknown)
	}

	if found && strings.EqualFold(r.MD5, sum) {
		return nil
	}

	// An end of the operation's context stays as it is, so that the interface
	// shows the operation's timeout or cancellation.
	_, isCloud := errors.AsType[CloudError](cause)
	isCtx := errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, context.Canceled)

	if !isCloud && !isCtx {
		cause = withStep(networkError(cause), stepMove)
	}

	return fmt.Errorf("move: %w", cause)
}

// settle fences the temporary file of an earlier Write that the pending
// store kept, before the first access of this Store (spec §5.5).
func (s *Store) settle(ctx context.Context) error {
	if s.settled {
		return nil
	}

	name, found, err := s.pending.LoadPendingTemp(ctx)
	if err != nil {
		return fmt.Errorf("load pending temporary file: %w", usecase.LocalError{Err: err})
	}

	if found {
		if !validTempName(name) {
			return fmt.Errorf("load pending temporary file: %w", usecase.LocalError{Err: errBadPendingName})
		}

		if err := s.fenceTemp(ctx, name); err != nil {
			return err
		}

		if err := s.pending.ClearPendingTemp(ctx); err != nil {
			return fmt.Errorf("clear pending temporary file: %w", usecase.LocalError{Err: err})
		}
	}

	for _, name := range s.memory.list() {
		if err := s.fenceTemp(ctx, name); err != nil {
			return err
		}

		s.memory.remove(name)
	}

	s.settled = true

	return nil
}

// fenceTemp deletes the temporary file name so that no move can take it. A
// failure leaves the outcome unknown, except a refused token, which asks for
// a new sign-in; the caller keeps the record either way.
func (s *Store) fenceTemp(ctx context.Context, name string) error {
	err := s.deleteTempAt(ctx, stepFence, appRoot+name)

	switch {
	case err == nil:
		return nil
	case errors.Is(err, domain.ErrReauthRequired):
		return fmt.Errorf("fence the temporary file: %w", err)
	default:
		return fmt.Errorf("fence the temporary file: %w", blobremote.ErrOutcomeUnknown)
	}
}

// validTempName reports whether name is a temporary file name of Write:
// <object>.tmp-<uuid>, with no path separator.
func validTempName(name string) bool {
	i := strings.LastIndex(name, tempInfix)
	if i <= 0 || strings.ContainsAny(name, `/\`) {
		return false
	}

	_, err := uuid.Parse(name[i+len(tempInfix):])

	return err == nil
}

// deleteTemp deletes the Write's own temporary file path: deleteTempAt of
// stepDelete.
func (s *Store) deleteTemp(ctx context.Context, path string) error {
	return s.deleteTempAt(ctx, stepDelete, path)
}

// deleteTempAt deletes path permanently within fenceTimeout, as the request
// of step. nil means path is gone: deleted, or missing because a move took it.
func (s *Store) deleteTempAt(ctx context.Context, step, path string) error {
	err := s.withDeadline(ctx, func(ctx context.Context) error {
		u, err := s.api.url(resourcesPath, url.Values{qPath: {path}, "permanently": {"true"}})
		if err != nil {
			return err
		}

		var l link

		status, err := s.call(ctx, step, http.MethodDelete, u, &l)

		switch {
		case isStatus(err, http.StatusNotFound):
			return nil
		case err != nil:
			return err
		case status == http.StatusAccepted:
			done, err := s.waitOperation(ctx, step, l.Href)
			if err == nil && !done {
				err = CloudError{Code: codeOther, Step: step, err: errOperationFailed}
			}

			return err
		default:
			return nil
		}
	})
	if err != nil {
		s.log.Error("yandex disk: temporary file not deleted", applog.Err(err))
	}

	return err
}

// removeTemp deletes a temporary file that no move can take, ignoring a
// failure: cleanup removes it later.
func (s *Store) removeTemp(ctx context.Context, path string) {
	_ = s.deleteTemp(context.WithoutCancel(ctx), path) // logged; cleanup retries
}

func (s *Store) withDeadline(ctx context.Context, f func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, fenceTimeout)
	defer cancel()

	return f(ctx)
}

// cleanup deletes the temporary files of name older than tempMaxAge that
// failed Writes left (step 7). Every Write lists the folder from the start,
// within listMaxPages pages and maxCleanups deletions; failures are only
// logged.
func (s *Store) cleanup(ctx context.Context, name string) {
	deleted := 0

	for page := range listMaxPages {
		var l struct {
			Embedded struct {
				Items []struct {
					Name    string `json:"name"`
					Created string `json:"created"`
				} `json:"items"`
				Total int `json:"total"`
			} `json:"_embedded"`
		}

		offset := page * listPageSize

		err := s.get(ctx, stepCleanup, resourcesPath, url.Values{
			qPath:    {appRoot},
			qFields:  {"_embedded.items.name,_embedded.items.created,_embedded.total"},
			"limit":  {strconv.Itoa(listPageSize)},
			"offset": {strconv.Itoa(offset)},
		}, &l)
		if err != nil {
			s.log.Error("yandex disk: list the app folder", applog.Err(err))
			return
		}

		for _, it := range l.Embedded.Items {
			if deleted == maxCleanups {
				return
			}

			if !s.oldTemp(name, it.Name, it.Created) {
				continue
			}

			deleted++

			_ = s.deleteTempAt(ctx, stepCleanup, appRoot+it.Name) // logged; the next Write retries
		}

		if len(l.Embedded.Items) < listPageSize || offset+len(l.Embedded.Items) >= l.Embedded.Total {
			return
		}
	}
}

// oldTemp reports whether file is a temporary file of name created more than
// tempMaxAge ago.
func (s *Store) oldTemp(name, file, created string) bool {
	if !strings.HasPrefix(file, name+tempInfix) || !validTempName(file) {
		return false
	}

	t, err := time.Parse(time.RFC3339, created)

	return err == nil && s.now().Sub(t) > tempMaxAge
}

// get sends a GET request of step to the API path with query q.
func (s *Store) get(ctx context.Context, step, path string, q url.Values, out any) error {
	u, err := s.api.url(path, q)
	if err != nil {
		return err
	}

	_, err = s.call(ctx, step, http.MethodGet, u, out)

	return err
}

// call sends the API request of step with the token. A 401 answer is
// domain.ErrReauthRequired: the app has no way to refresh the token
// (spec §14).
func (s *Store) call(ctx context.Context, step, method string, u *url.URL, out any) (int, error) {
	if s.reauth {
		return 0, domain.ErrReauthRequired
	}

	tok, err := s.token(ctx)
	if err != nil {
		return 0, s.noteReauth(err)
	}

	status, err := s.api.send(ctx, step, method, u, tok.Access, out)
	if status == http.StatusUnauthorized {
		return 0, s.noteReauth(domain.ErrReauthRequired)
	}

	return status, err
}

// noteReauth stops further requests once err is domain.ErrReauthRequired.
func (s *Store) noteReauth(err error) error {
	if errors.Is(err, domain.ErrReauthRequired) {
		s.reauth = true
	}

	return err
}

// token returns the token for the next request. One whose expiry has passed
// is not sent: it asks for a new sign-in.
func (s *Store) token(ctx context.Context) (usecase.CloudToken, error) {
	t, err := s.tokens.Token(ctx)
	if err != nil {
		return usecase.CloudToken{}, fmt.Errorf("cloud token: %w", err)
	}

	if !t.Expiry.IsZero() && !s.now().Before(t.Expiry) {
		return usecase.CloudToken{}, domain.ErrReauthRequired
	}

	return t, nil
}

// meter starts the count of the bytes of one transfer of total bytes in
// direction; nil when nobody receives the progress. A download whose body
// is longer than its metadata says counts up to its own length; an upload
// always has the length of its data as Total.
func (s *Store) meter(direction string, total int64) *meter {
	if s.progress == nil {
		return nil
	}

	report := usecase.ThrottleProgress(s.progress, s.now)

	return startMeter(func(done int64) {
		if direction == usecase.DirectionDownload {
			total = max(total, done)
		}

		report(usecase.TransferProgress{
			Storage: domain.KindYandexDisk, Direction: direction, Unit: usecase.UnitBytes,
			Done: done, Total: total,
		})
	})
}

// Close closes the idle connections of the store's clients. The transport
// is shared by every store, so a connection kept after the operation would
// wait for the next one, which may come much later over a changed network.
// It implements io.Closer for blobremote.Session.
func (s *Store) Close() error {
	s.api.http.CloseIdleConnections()
	s.href.http.CloseIdleConnections()

	return nil
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data) //nolint:gosec // MD5 is the Disk API's checksum of a file, not a security hash
	return hex.EncodeToString(sum[:])
}
