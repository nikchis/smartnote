package gdrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

const (
	// resolveTimeout bounds the resolution of a session (spec §5.4).
	resolveTimeout = 10 * time.Second

	// statusClientClosed is the answer of Drive to DELETE of an incomplete
	// session and to the status of a cancelled one (П6 г).
	statusClientClosed = 499
)

// outcome is what the resolution proved about an upload session.
type outcome int

const (
	outcomeUnknown   outcome = iota // no answer, or one §5.4 does not name: the session may still commit
	outcomeCommitted                // the upload is committed and will not change (step 2)
	outcomeCancelled                // our cancellation is confirmed (step 3)
	outcomeGone                     // unknown, cancelled or expired before (step 4)
)

func (o outcome) String() string {
	return [...]string{"unknown", "committed", "cancelled", "gone"}[o]
}

// settle resolves the record of an unfinished upload before the first
// access of this Store, and again after a Write left one (spec §5.4, 3).
// While the session may still commit the store gives
// domain.ErrRemoteOutcomeUnknown and touches no file. The resolution does
// not compare the check value: the open only needs to know that the session
// cannot change the storage any more.
func (s *Store) settle(ctx context.Context) error {
	s.settleMu.Lock()
	defer s.settleMu.Unlock()

	if s.settled {
		return nil
	}

	if s.uploads == nil {
		return usecase.LocalError{Err: errNoUploadStore}
	}

	raw, found, err := s.uploads.LoadCloudUpload(ctx, domain.KindGoogleDrive)
	if err != nil {
		return fmt.Errorf("load upload record: %w", usecase.LocalError{Err: err})
	}

	if found {
		if err := s.resolveRecord(ctx, raw); err != nil {
			return err
		}
	}

	s.settled = true

	return nil
}

// unsettle makes the next access resolve the record a Write left.
func (s *Store) unsettle() {
	s.settleMu.Lock()
	defer s.settleMu.Unlock()

	s.settled = false
}

// resolveRecord resolves the record raw at the open and removes it once
// the session cannot commit.
func (s *Store) resolveRecord(ctx context.Context, raw []byte) error {
	var rec pendingUpload

	err := json.Unmarshal(raw, &rec)
	clear(raw)

	if err == nil && rec.Size >= 0 {
		_, err = s.api.sessionURL(rec.SessionURI)
	}

	if err != nil || rec.Size < 0 {
		return fmt.Errorf("load upload record: %w", usecase.LocalError{Err: errBadRecord})
	}

	if res, _ := s.resolve(ctx, rec); res == outcomeUnknown {
		return fmt.Errorf("unfinished upload: %w", domain.ErrRemoteOutcomeUnknown)
	}

	if err := s.uploads.ClearCloudUpload(ctx, domain.KindGoogleDrive); err != nil {
		return fmt.Errorf("clear upload record: %w", usecase.LocalError{Err: err})
	}

	return nil
}

// resolve finds out whether the session of rec can still commit, within
// resolveTimeout of ctx (spec §5.4). It sends requests only to the session
// URI and without Authorization (П6 е), so it does not depend on the
// account or the client of the current sign-in, and reads no file. The
// file is that of a committed session, as its answer gives it. The caller
// removes the record unless the outcome is unknown.
func (s *Store) resolve(ctx context.Context, rec pendingUpload) (outcome, driveFile) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()

	res, f := outcomeUnknown, driveFile{}

	if sess, err := s.api.sessionURL(rec.SessionURI); err == nil {
		res, f = s.resolveSession(ctx, sess, rec.Size)
	}

	s.log.Info("google drive: unfinished upload resolved", "op", rec.Op, "outcome", res.String())

	return res, f
}

// resolveSession runs the steps of §5.4 (rev. 7, by П6): the status (step
// 1); 200/201 is committed (2); 308 is cancelled with DELETE, whose 204/499
// confirms the cancellation and 200/201 is a commit that came first (3);
// 404/410/499 cannot commit any more (4). A lost or unnamed answer to
// DELETE is followed by the status once more, which confirms the
// cancellation with 404/410/499. Anything else is unknown (5).
func (s *Store) resolveSession(ctx context.Context, sess *url.URL, size int64) (outcome, driveFile) {
	cancelSent := false

	for range 2 {
		status, f := s.sessionRequest(ctx, stepUploadStatus, http.MethodPut, sess, size)

		switch status {
		case http.StatusOK, http.StatusCreated:
			return outcomeCommitted, f
		case http.StatusNotFound, http.StatusGone, statusClientClosed:
			if cancelSent {
				return outcomeCancelled, driveFile{}
			}

			return outcomeGone, driveFile{}
		case http.StatusPermanentRedirect:
		default:
			return outcomeUnknown, driveFile{}
		}

		status, f = s.sessionRequest(ctx, stepUploadCancel, http.MethodDelete, sess, 0)

		switch status {
		case http.StatusOK, http.StatusCreated:
			return outcomeCommitted, f
		case http.StatusNoContent, statusClientClosed:
			return outcomeCancelled, driveFile{}
		}

		cancelSent = true
	}

	return outcomeUnknown, driveFile{}
}

// sessionRequest sends a request of step to the session without
// Authorization: a PUT is the status request for a body of size bytes,
// "Content-Range: bytes */<size>" with an empty body. It returns the status
// of the answer, 0 without one, and the file of a 2xx answer, zero when it
// cannot be read.
func (s *Store) sessionRequest(ctx context.Context, step, method string, sess *url.URL, size int64) (int, driveFile) {
	req, err := s.api.request(ctx, step, method, sess, "", nil)
	if err != nil {
		return 0, driveFile{}
	}

	if method == http.MethodPut {
		req.Header.Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
	}

	resp, err := s.api.exchange(ctx, step, req)
	if err != nil {
		return 0, driveFile{}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))
	if err != nil {
		// The answer broke off: whatever its status said is not proven.
		return 0, driveFile{}
	}

	var f driveFile
	if success(resp.StatusCode) {
		_ = json.Unmarshal(body, &f) // a commit without a readable file fails the check of Write
	}

	return resp.StatusCode, f
}
