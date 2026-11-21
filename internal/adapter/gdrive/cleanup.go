package gdrive

import (
	"context"
	"net/http"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/applog"
)

// maxCleanups is the largest number of duplicates one write deletes
// (spec §5.2).
const maxCleanups = 10

// removeDuplicates deletes the files of a list just read that have an
// earlier one: all but the first, at most maxCleanups (spec §5.2). Nobody
// reads them, so deleting them is safe for any device; the earliest file is
// never deleted, even when the list repeats its ID. Failures are only
// logged: a later write retries.
func (s *Store) removeDuplicates(ctx context.Context, files []driveFile) {
	if len(files) < 2 {
		return
	}

	effective := files[0].ID

	for _, f := range files[1:min(len(files), maxCleanups+1)] {
		if f.ID == effective {
			continue
		}

		if err := s.deleteFile(ctx, f.ID); err != nil {
			s.log.Error("google drive: duplicate of the container not deleted", applog.Err(err))
		}
	}
}

// cleanupAfterReplace lists name after a replacement and removes its
// duplicates; a failed list is only logged.
func (s *Store) cleanupAfterReplace(ctx context.Context, name string) {
	files, err := s.list(ctx, name)
	if err != nil {
		s.log.Error("google drive: duplicates of the container not listed", applog.Err(err))
		return
	}

	s.removeDuplicates(ctx, files)
}

// deleteFile deletes the file id for good (files.delete); a missing file is
// deleted already.
func (s *Store) deleteFile(ctx context.Context, id string) error {
	u, err := s.api.url("/files/"+id, nil)
	if err != nil {
		return err
	}

	err = s.authorized(ctx, func(access string) error { return s.api.delete(ctx, stepDelete, u, access) })
	if cloudhttp.IsStatus(err, http.StatusNotFound) {
		return nil
	}

	return err
}
