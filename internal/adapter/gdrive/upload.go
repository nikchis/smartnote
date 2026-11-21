package gdrive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// UploadStore keeps the record of the unfinished upload of a provider until
// it is proven that its session cannot commit any more (spec §5.4, §7.2).
// The record is opaque bytes; local.db implements the store and seals them
// with the DEK, because the session URI alone lets anyone finish the upload.
// One record per provider is saved and removed without a condition: that
// relies on usecase.Gate, which runs one cloud operation at a time in the
// app, so no two uploads of a device overlap.
type UploadStore interface {
	LoadCloudUpload(ctx context.Context, p domain.StorageKind) (data []byte, found bool, err error)
	SaveCloudUpload(ctx context.Context, p domain.StorageKind, data []byte) error
	ClearCloudUpload(ctx context.Context, p domain.StorageKind) error
}

// Kinds of an upload, pendingUpload.Op; only the log uses them.
const (
	opCreate  = "create"
	opReplace = "replace"
)

const (
	// uploadFields is the field mask of the answers that give the uploaded
	// file (spec §5.1).
	uploadFields = "id,size,md5Checksum,sha256Checksum,headRevisionId"
	contentOctet = "application/octet-stream"

	// localWriteTimeout bounds each write of the record in local.db, which
	// does not end with the operation's context.
	localWriteTimeout = 5 * time.Second
)

var (
	errChecksum      = errors.New("committed upload is not the written data")
	errNoSession     = errors.New("opening answer without a valid session URI")
	errNoUploadStore = errors.New("no store of the upload record")
	errBadRecord     = errors.New("upload record cannot be used")
)

// pendingUpload is the record of an unfinished upload (spec §7.2): the
// session URI, the size of the body for the status request and the kind of
// the operation for the log.
type pendingUpload struct {
	SessionURI string `json:"session_uri"`
	Op         string `json:"op"`
	Size       int64  `json:"size"`
}

// uploadTarget is what an upload writes: a new file name in appDataFolder,
// or new content of the file fileID.
type uploadTarget struct {
	op     string
	fileID string // the replaced file
	name   string // the created file
}

// refusedError is a final PUT refused with 401 whose session is proven not
// to commit: Write refreshes the token and repeats the operation once with
// its precondition (spec §5.4). access is the refused token.
type refusedError struct{ access string }

func (refusedError) Error() string { return "final upload request refused" }

// upload writes data with one resumable session (spec §5.4) and returns the
// committed file: its md5Checksum is the MD5 of data, so it is our commit.
// The record of the session is saved before the final PUT and removed once
// the session cannot commit any more (unconditionally: usecase.Gate keeps
// one operation at a time, see UploadStore). The final PUT is never
// repeated on its URI.
func (s *Store) upload(ctx context.Context, t uploadTarget, data []byte) (driveFile, error) {
	sess, err := s.openSession(ctx, t, int64(len(data)))
	if err != nil {
		return driveFile{}, err
	}

	// The operation's context may end at any moment; the local steps and the
	// resolution get their own deadlines.
	base := context.WithoutCancel(ctx)

	access, err := s.accessToken(ctx)
	if err != nil {
		s.cancelUnrecorded(base, sess)
		return driveFile{}, err
	}

	rec := pendingUpload{SessionURI: sess.String(), Op: t.op, Size: int64(len(data))}
	if err := s.saveRecord(base, rec); err != nil {
		s.cancelUnrecorded(base, sess)
		return driveFile{}, fmt.Errorf("record the upload: %w", usecase.LocalError{Err: err})
	}

	f, err := s.putBody(ctx, sess, access, data)

	switch {
	case err == nil:
		s.clearRecord(base)
		return f, checkCommit(f, data, t.fileID)
	case cloudhttp.IsStatus(err, http.StatusPreconditionFailed):
		// A refused precondition is never committed.
		s.clearRecord(base)
		return driveFile{}, fmt.Errorf("upload: %w", blobremote.ErrVersionMismatch)
	case cloudhttp.IsStatus(err, http.StatusUnauthorized):
		return s.afterRefusal(base, rec, access, data, t.fileID)
	default:
		return s.resolveWrite(base, rec, data, t.fileID, err)
	}
}

// openSession opens the resumable session of t for a body of size bytes and
// returns its URI, checked to be on the session host. A refused
// precondition is blobremote.ErrVersionMismatch; there is no session then.
func (s *Store) openSession(ctx context.Context, t uploadTarget, size int64) (*url.URL, error) {
	method, path, meta := http.MethodPost, "/files", []byte("{}")

	if t.op == opReplace {
		method, path = http.MethodPatch, "/files/"+t.fileID
	} else {
		var err error

		meta, err = json.Marshal(map[string]any{"name": t.name, "parents": []string{appDataFolder}, "mimeType": contentOctet})
		if err != nil {
			return nil, err
		}
	}

	u, err := s.api.uploadURL(path, url.Values{"uploadType": {"resumable"}, qFields: {uploadFields}})
	if err != nil {
		return nil, err
	}

	var location string

	err = s.authorized(ctx, func(access string) error {
		req, err := s.api.request(ctx, stepUploadOpen, method, u, access, meta)
		if err != nil {
			return err
		}

		req.Header.Set("Content-Type", "application/json; charset=UTF-8")
		req.Header.Set("X-Upload-Content-Type", contentOctet)
		req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(size, 10))

		resp, err := s.api.exchange(ctx, stepUploadOpen, req)
		if err != nil {
			return err
		}

		if !success(resp.StatusCode) {
			return statusError(resp, stepUploadOpen)
		}

		location = resp.Header.Get("Location")

		return resp.Body.Close()
	})

	switch {
	case cloudhttp.IsStatus(err, http.StatusPreconditionFailed):
		return nil, fmt.Errorf("open upload: %w", blobremote.ErrVersionMismatch)
	case err != nil:
		return nil, fmt.Errorf("open upload: %w", err)
	}

	sess, err := s.api.sessionURL(location)
	if err != nil {
		return nil, otherError(0, stepUploadOpen, errNoSession)
	}

	return sess, nil
}

// putBody sends the whole body in one PUT to the session with the access
// token and returns the file of a 2xx answer. Any other answer is a
// CloudError; so is a 2xx answer that cannot be read.
func (s *Store) putBody(ctx context.Context, sess *url.URL, access string, data []byte) (driveFile, error) {
	m := s.meter(usecase.DirectionUpload, int64(len(data)))
	defer m.Stop()

	req, err := s.api.request(ctx, stepUpload, http.MethodPut, sess, access, nil)
	if err != nil {
		return driveFile{}, err
	}

	if len(data) > 0 {
		req.Body = io.NopCloser(m.Wrap(bytes.NewReader(data)))
		req.ContentLength = int64(len(data))
	}

	req.Header.Set("Content-Type", contentOctet)

	resp, err := s.api.exchange(ctx, stepUpload, req)
	if err != nil {
		return driveFile{}, err
	}

	if !success(resp.StatusCode) {
		return driveFile{}, statusError(resp, stepUpload)
	}

	var f driveFile
	if err := decodeJSON(ctx, resp, stepUpload, &f); err != nil {
		return driveFile{}, err
	}

	return f, nil
}

// checkCommit returns a cloud error unless the committed file f is the
// file wantID (any for a creation) with the MD5 of data (spec §5.1).
func checkCommit(f driveFile, data []byte, wantID string) error {
	if !validFileID.MatchString(f.ID) || wantID != "" && f.ID != wantID || !strings.EqualFold(f.MD5, md5Hex(data)) {
		return otherError(0, stepUpload, errChecksum)
	}

	return nil
}

// resolveWrite settles the session of rec after a final PUT that failed
// with cause, inside the Write that has the data (spec §5.4): our commit is
// a success, a commit of other bytes a cloud error; a cancelled session
// gives cause; a session that cannot commit any more gives cause when the
// PUT was refused with a 4xx, else an unknown outcome. Without an answer
// the record stays, and the outcome is unknown.
func (s *Store) resolveWrite(base context.Context, rec pendingUpload, data []byte, wantID string, cause error) (driveFile, error) {
	res, f := s.resolve(base, rec)

	switch res {
	case outcomeCommitted:
		s.clearRecord(base)
		return f, checkCommit(f, data, wantID)
	case outcomeCancelled:
		s.clearRecord(base)
		return driveFile{}, fmt.Errorf("upload: %w", cause)
	case outcomeGone:
		s.clearRecord(base)

		if ce, ok := errors.AsType[cloudhttp.CloudError](cause); ok && ce.Status >= 400 && ce.Status < 500 {
			return driveFile{}, fmt.Errorf("upload: %w", cause)
		}

		s.log.Error("google drive: upload outcome unknown, the session cannot commit any more", applog.Err(cause))

		return driveFile{}, fmt.Errorf("upload: %w", blobremote.ErrOutcomeUnknown)
	default:
		s.unsettle()
		s.log.Error("google drive: upload outcome unknown, the record stays", applog.Err(cause))

		return driveFile{}, fmt.Errorf("upload: %w", blobremote.ErrOutcomeUnknown)
	}
}

// afterRefusal settles the session of rec after a final PUT refused with
// 401: our commit is a success; a session that cannot commit gives
// refusedError, so that Write repeats the operation with a new token.
func (s *Store) afterRefusal(base context.Context, rec pendingUpload, access string, data []byte, wantID string) (driveFile, error) {
	res, f := s.resolve(base, rec)

	switch res {
	case outcomeCommitted:
		s.clearRecord(base)
		return f, checkCommit(f, data, wantID)
	case outcomeUnknown:
		s.unsettle()
		return driveFile{}, fmt.Errorf("upload refused: %w", blobremote.ErrOutcomeUnknown)
	default:
		s.clearRecord(base)
		return driveFile{}, refusedError{access: access}
	}
}

// saveRecord saves rec within localWriteTimeout.
func (s *Store) saveRecord(base context.Context, rec pendingUpload) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(base, localWriteTimeout)
	defer cancel()

	return s.uploads.SaveCloudUpload(ctx, domain.KindGoogleDrive, data)
}

// clearRecord removes the record once its session cannot commit. A record
// that stays only makes the next open resolve the session again.
func (s *Store) clearRecord(base context.Context) {
	ctx, cancel := context.WithTimeout(base, localWriteTimeout)
	defer cancel()

	if err := s.uploads.ClearCloudUpload(ctx, domain.KindGoogleDrive); err != nil {
		s.log.Error("google drive: upload record not cleared", applog.Err(err))
	}
}

// cancelUnrecorded cancels a session whose final PUT is not sent; without a
// PUT and a record it cannot commit, so a failure is only logged.
func (s *Store) cancelUnrecorded(base context.Context, sess *url.URL) {
	ctx, cancel := context.WithTimeout(base, resolveTimeout)
	defer cancel()

	if status, _ := s.sessionRequest(ctx, stepUploadCancel, http.MethodDelete, sess, 0); status != statusClientClosed &&
		status != http.StatusNoContent {
		s.log.Error("google drive: unused upload session not cancelled", "status", status)
	}
}
