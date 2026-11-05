package sqlite

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
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
const formatVersion = 2

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
	meta_id INTEGER PRIMARY KEY,
	key     TEXT NOT NULL UNIQUE,
	value   BLOB NOT NULL
);
CREATE TABLE note (
	note_uuid TEXT PRIMARY KEY,
	updated   INTEGER NOT NULL,
	name      BLOB NOT NULL,
	info      BLOB NOT NULL
);
CREATE TABLE note_version (
	note_version_uuid TEXT PRIMARY KEY,
	note_uuid         TEXT NOT NULL REFERENCES note (note_uuid) ON DELETE CASCADE,
	created           INTEGER NOT NULL,
	info              BLOB NOT NULL
);
CREATE INDEX note_version_note_uuid ON note_version (note_uuid);
CREATE TABLE connection (
	connection_id      INTEGER PRIMARY KEY CHECK (connection_id = 1),
	settings           BLOB,
	server_uuid        TEXT,
	last_sync_revision INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE ssh_key (
	ssh_key_id  INTEGER PRIMARY KEY CHECK (ssh_key_id = 1),
	private_key BLOB NOT NULL,
	public_key  TEXT NOT NULL
);
` + embeddingDDL

// embeddingDDL is the table added by format 2 (search spec §4.1); Unlock runs
// it to migrate a format 1 database.
const embeddingDDL = `CREATE TABLE note_embedding (
	note_uuid TEXT PRIMARY KEY REFERENCES note (note_uuid) ON DELETE CASCADE,
	data      BLOB NOT NULL
);`

// cloudAuthDDL and cloudPendingDDL are the tables of the cloud storage
// (yandex spec §7.2). They are created on every Create and Unlock and are not
// required, so a database of version 2.1.0 stays valid.
const (
	cloudAuthDDL = `CREATE TABLE IF NOT EXISTS cloud_auth (
	cloud_auth_id INTEGER PRIMARY KEY CHECK (cloud_auth_id = 1),
	data          BLOB NOT NULL
);`
	cloudPendingDDL = `CREATE TABLE IF NOT EXISTS cloud_pending (
	cloud_pending_id INTEGER PRIMARY KEY CHECK (cloud_pending_id = 1),
	temp_name        TEXT NOT NULL
);`
)

// requiredTablesV1 and requiredTablesV2 are checked on Unlock, so a damaged
// schema reads as a corrupt storage rather than as a later "no such table".
var (
	requiredTablesV1 = []string{tableMeta, tableNote, tableNoteVersion, tableConnection, tableSSHKey}
	requiredTablesV2 = append(slices.Clone(requiredTablesV1), tableNoteEmbedding)
)

// Table names, also the Table part of the AADs.
const (
	tableMeta          = "meta"
	tableNote          = "note"
	tableNoteVersion   = "note_version"
	tableConnection    = "connection"
	tableSSHKey        = "ssh_key"
	tableNoteEmbedding = "note_embedding"
	tableCloudAuth     = "cloud_auth"
)

// AADs of the sealed fields (spec §4).
func aadWrappedDEK() sealbox.AAD {
	return sealbox.AAD{Table: tableMeta, Column: metaWrappedDEK, RowID: "1"}
}
func aadSettings() sealbox.AAD {
	return sealbox.AAD{Table: tableConnection, Column: "settings", RowID: "1"}
}
func aadPrivateKey() sealbox.AAD {
	return sealbox.AAD{Table: tableSSHKey, Column: "private_key", RowID: "1"}
}

func aadEmbedding(id string) sealbox.AAD {
	return sealbox.AAD{Table: tableNoteEmbedding, Column: "data", RowID: id}
}

func aadNote(column, id string) sealbox.AAD {
	return sealbox.AAD{Table: tableNote, Column: column, RowID: id}
}

func aadVersion(versionID, noteID string) sealbox.AAD {
	return sealbox.AAD{Table: tableNoteVersion, Column: "info", RowID: versionID, OwnerID: noteID}
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
