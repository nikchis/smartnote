package gdrive

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/domain"
)

// versionFields is the field mask of the version check of a replacement.
const versionFields = "id,md5Checksum,trashed"

// Write replaces the effective file of name with data if its version is
// expect, or creates it if expect is "" and there is none (spec §5.3).
// Drive does not check If-Match (П5), so the window between the check and
// the commit is accepted (spec §11). An unfinished upload of an earlier
// Write is resolved first (spec §5.4). A final PUT refused with 401 leads
// to one repeat of the operation from its check, with a refreshed token.
func (s *Store) Write(ctx context.Context, name string, data []byte, expect string) error {
	if err := s.settle(ctx); err != nil {
		return err
	}

	err := s.write(ctx, name, data, expect)

	refused, ok := errors.AsType[refusedError](err)
	if !ok {
		return err
	}

	if _, err := s.renew(ctx, refused.access); err != nil {
		return err
	}

	err = s.write(ctx, name, data, expect)
	if _, ok := errors.AsType[refusedError](err); ok {
		return fmt.Errorf("upload refused after a refresh: %w", domain.ReauthRequired(domain.KindGoogleDrive))
	}

	return err
}

func (s *Store) write(ctx context.Context, name string, data []byte, expect string) error {
	if expect == "" {
		return s.create(ctx, name, data)
	}

	return s.replace(ctx, name, data, expect)
}

// create uploads a new file of name when there is none (spec §5.3,
// creation). When another device created one meanwhile and its file is the
// effective one, the own file is deleted and it is
// blobremote.ErrVersionMismatch.
func (s *Store) create(ctx context.Context, name string, data []byte) error {
	files, err := s.list(ctx, name)
	if err != nil {
		return err
	}

	if len(files) > 0 {
		return fmt.Errorf("create %s: %w", name, blobremote.ErrVersionMismatch)
	}

	own, err := s.upload(ctx, uploadTarget{op: opCreate, name: name}, data)
	if err != nil {
		return err
	}

	files, err = s.list(ctx, name)
	if err != nil {
		// The file is created, but whether it is the effective one is not known.
		s.log.Error("google drive: created container not listed", applog.Err(err))
		return fmt.Errorf("list the created container: %w", blobremote.ErrOutcomeUnknown)
	}

	if len(files) == 0 || files[0].ID != own.ID {
		// Its duplicate, if not deleted now, goes with a later write.
		if err := s.deleteFile(ctx, own.ID); err != nil {
			s.log.Error("google drive: own duplicate of the container not deleted", applog.Err(err))
		}

		return fmt.Errorf("create %s: created by another device: %w", name, blobremote.ErrVersionMismatch)
	}

	s.removeDuplicates(ctx, files)

	return nil
}

// replace uploads new content of the file of expect,
// "<fileId>:<md5Checksum>", if it still has this version (spec §5.3,
// replacement).
func (s *Store) replace(ctx context.Context, name string, data []byte, expect string) error {
	id, sum, ok := strings.Cut(expect, ":")
	if !ok || !validFileID.MatchString(id) || sum == "" {
		return fmt.Errorf("replace %s: %w", name, blobremote.ErrVersionMismatch)
	}

	if err := s.checkVersion(ctx, id, sum); err != nil {
		return fmt.Errorf("replace %s: %w", name, err)
	}

	if _, err := s.upload(ctx, uploadTarget{op: opReplace, fileID: id, name: name}, data); err != nil {
		return err
	}

	s.cleanupAfterReplace(ctx, name)

	return nil
}

// checkVersion returns blobremote.ErrVersionMismatch unless the file id
// exists, is not trashed and has the md5Checksum sum.
func (s *Store) checkVersion(ctx context.Context, id, sum string) error {
	u, err := s.api.url("/files/"+id, url.Values{qFields: {versionFields}})
	if err != nil {
		return err
	}

	var f struct {
		ID      string `json:"id"`
		MD5     string `json:"md5Checksum"`
		Trashed bool   `json:"trashed"`
	}

	err = s.authorized(ctx, func(access string) error { return s.api.getJSON(ctx, stepVersion, u, access, &f) })

	switch {
	case cloudhttp.IsStatus(err, http.StatusNotFound):
		return fmt.Errorf("check version: %w", blobremote.ErrVersionMismatch)
	case err != nil:
		return fmt.Errorf("check version: %w", err)
	case f.Trashed || f.ID != id || f.MD5 == "" || !strings.EqualFold(f.MD5, sum):
		return fmt.Errorf("check version: %w", blobremote.ErrVersionMismatch)
	}

	return nil
}
