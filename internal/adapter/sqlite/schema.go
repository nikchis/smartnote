package sqlite

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	msqlite "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// File names in the data directory.
const (
	dbFile  = "local.db"
	tmpFile = "local.db.tmp"
)

// formatVersion is the storage format this version reads and writes.
const formatVersion = 1

// Keys of the meta table.
const (
	metaFormatVersion = "format_version"
	metaKDFParams     = "kdf_params"
	metaKDFSalt       = "kdf_salt"
	metaWrappedDEK    = "wrapped_dek"
)

// schema is the DDL of spec §5.1.
const schema = `
CREATE TABLE meta (
	key   TEXT PRIMARY KEY,
	value BLOB NOT NULL
);
CREATE TABLE notes (
	note_id TEXT PRIMARY KEY,
	updated INTEGER NOT NULL,
	name    BLOB NOT NULL,
	info    BLOB NOT NULL
);
CREATE TABLE note_versions (
	version_id TEXT PRIMARY KEY,
	note_id    TEXT NOT NULL REFERENCES notes (note_id) ON DELETE CASCADE,
	created    INTEGER NOT NULL,
	info       BLOB NOT NULL
);
CREATE INDEX note_versions_note_id ON note_versions (note_id);
CREATE TABLE connection (
	id                 INTEGER PRIMARY KEY CHECK (id = 1),
	settings           BLOB,
	server_id          TEXT,
	last_sync_revision INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE ssh_key (
	id          INTEGER PRIMARY KEY CHECK (id = 1),
	private_key BLOB NOT NULL,
	public_key  TEXT NOT NULL
);`

// requiredTables are checked on Unlock, so a damaged schema reads as a
// corrupt storage rather than as a later "no such table".
var requiredTables = []string{"meta", "notes", "note_versions", "connection", "ssh_key"}

// AADs of the sealed fields (spec §4).
func aadWrappedDEK() sealbox.AAD {
	return sealbox.AAD{Table: "meta", Column: metaWrappedDEK, RowID: "1"}
}
func aadSettings() sealbox.AAD {
	return sealbox.AAD{Table: "connection", Column: "settings", RowID: "1"}
}
func aadPrivateKey() sealbox.AAD {
	return sealbox.AAD{Table: "ssh_key", Column: "private_key", RowID: "1"}
}

func aadNote(column, id string) sealbox.AAD {
	return sealbox.AAD{Table: "notes", Column: column, RowID: id}
}

func aadVersion(versionID, noteID string) sealbox.AAD {
	return sealbox.AAD{Table: "note_versions", Column: "info", RowID: versionID, OwnerID: noteID}
}

// dsn returns the URI for path: the file must exist (mode=rw), foreign keys
// are on and a locked database is retried for 5 s. The path is URI-encoded,
// so spaces, "#" or "?" in it cannot change the parameters.
func dsn(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // a Windows drive path
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=rw&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}
	return u.String()
}

// mapOpenError turns the SQLite errors of a file that is not a usable
// database into domain.ErrCorruptStorage.
func mapOpenError(err error) error {
	var se *msqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & 0xff {
		case sqlite3.SQLITE_NOTADB, sqlite3.SQLITE_CORRUPT:
			return fmt.Errorf("%w: %w", domain.ErrCorruptStorage, err)
		}
	}
	return err
}

// corrupt marks err as a sign of damaged or tampered local data.
func corrupt(format string, args ...any) error {
	return fmt.Errorf("%w: %s", domain.ErrCorruptStorage, fmt.Sprintf(format, args...))
}
