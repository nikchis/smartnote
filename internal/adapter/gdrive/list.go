package gdrive

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
)

const (
	listPageSize = 100
	listMaxPages = 10 // more pages is a damaged store (spec §5.2)

	// listFields is the field mask of the list (spec §5.1).
	listFields = "files(id,size,createdTime,md5Checksum,sha256Checksum,headRevisionId),nextPageToken"

	appDataFolder = "appDataFolder"
)

// validFileID matches the IDs of Drive files; an ID goes into the path of
// a request as it is.
var validFileID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

var errBadFileID = errors.New("file ID of the answer is not valid")

// driveFile is a file of the list, with the fields the store uses.
type driveFile struct {
	ID          string    `json:"id"`
	Size        int64     `json:"size,string"`
	CreatedTime time.Time `json:"createdTime"`
	MD5         string    `json:"md5Checksum"`
}

// list returns the files of name in appDataFolder that are not in the
// trash, the effective one first (spec §5.2): the earliest by createdTime,
// on equal createdTime the smaller ID, chosen among the files of all pages.
// The rest are duplicates nobody reads; their number is logged, without
// IDs. More than listMaxPages pages is domain.ErrRemoteDamaged.
func (s *Store) list(ctx context.Context, name string) ([]driveFile, error) {
	tok, err := s.token(ctx)
	if err != nil {
		return nil, err
	}

	q := url.Values{
		"spaces":   {appDataFolder},
		"q":        {"name='" + escapeQuery(name) + "' and trashed=false"},
		"orderBy":  {"createdTime"},
		"pageSize": {strconv.Itoa(listPageSize)},
		"fields":   {listFields},
	}

	var files []driveFile

	for range listMaxPages {
		u, err := s.api.url("/files", q)
		if err != nil {
			return nil, err
		}

		var page struct {
			Files         []driveFile `json:"files"`
			NextPageToken string      `json:"nextPageToken"`
		}

		if err := s.api.getJSON(ctx, stepList, u, tok.Access, &page); err != nil {
			return nil, fmt.Errorf("list %s: %w", name, err)
		}

		for _, f := range page.Files {
			if !validFileID.MatchString(f.ID) {
				return nil, fmt.Errorf("list %s: %w", name, otherError(0, stepList, errBadFileID))
			}
		}

		files = append(files, page.Files...)

		if page.NextPageToken == "" {
			s.sortCandidates(files)
			return files, nil
		}

		q.Set("pageToken", page.NextPageToken)
	}

	return nil, fmt.Errorf("list %s: more than %d pages: %w", name, listMaxPages, domain.ErrRemoteDamaged)
}

// sortCandidates puts the effective file first and logs the number of
// duplicates.
func (s *Store) sortCandidates(files []driveFile) {
	slices.SortFunc(files, func(a, b driveFile) int {
		if c := a.CreatedTime.Compare(b.CreatedTime); c != 0 {
			return c
		}

		return strings.Compare(a.ID, b.ID)
	})

	if len(files) > 1 {
		s.log.Warn("google drive: duplicate files of the container", "count", len(files)-1)
	}
}

// escapeQuery escapes name for a string of the Drive query language.
func escapeQuery(name string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(name)
}
