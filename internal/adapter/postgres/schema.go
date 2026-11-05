// Package postgres is the remote storage: the smartnote schema in a
// PostgreSQL database reached through SSH (spec §5.2, §5.3).
package postgres

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// schemaLockKey names the advisory lock that serializes schema creation.
const schemaLockKey = "smartnote.schema"

// storageTables are the tables of the remote storage.
var storageTables = []string{"smartnote.meta", "smartnote.note", "smartnote.sync_state"}

// ddl creates the tables of spec §5.2 in an existing smartnote schema.
const ddl = `
CREATE TABLE IF NOT EXISTS smartnote.meta (
  meta_id        smallint PRIMARY KEY CHECK (meta_id = 1),
  server_uuid    uuid     NOT NULL,
  format_version integer  NOT NULL,
  kdf_params     jsonb    NOT NULL,
  kdf_salt       bytea    NOT NULL,
  key_check      bytea    NOT NULL
);
CREATE TABLE IF NOT EXISTS smartnote.note (
  note_uuid uuid        PRIMARY KEY,
  updated   timestamptz NOT NULL,
  name      bytea       NOT NULL,
  info      bytea       NOT NULL
);
CREATE TABLE IF NOT EXISTS smartnote.sync_state (
  sync_state_id smallint    PRIMARY KEY CHECK (sync_state_id = 1),
  revision      bigint      NOT NULL,
  updated_at    timestamptz NOT NULL
);
INSERT INTO smartnote.sync_state (sync_state_id, revision, updated_at) VALUES (1, 0, now()) ON CONFLICT DO NOTHING;`

func aadNote(column, id string) sealbox.AAD {
	return sealbox.AAD{Table: "note", Column: column, RowID: id}
}

// parseKDF reads meta.kdf_params; anything unreadable is a format this
// version does not know.
func parseKDF(raw []byte) (sealbox.KDFParams, error) {
	var p sealbox.KDFParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return sealbox.KDFParams{}, fmt.Errorf("%w: kdf parameters: %w", domain.ErrUnsupportedFormat, err)
	}

	return p, nil
}

func sealNote(key []byte, n domain.Note) (name, info []byte, err error) {
	id := n.ID.String()
	if name, err = sealbox.Seal(key, []byte(n.Name), aadNote("name", id)); err != nil {
		return nil, nil, err
	}

	if info, err = sealbox.Seal(key, []byte(n.Info), aadNote("info", id)); err != nil {
		return nil, nil, err
	}

	return name, info, nil
}

// errNoteDoesNotOpen reports a server row that does not open with the key
// that opened the key check: forged, moved or damaged.
var errNoteDoesNotOpen = errors.New("remote note does not open")

func openNote(key []byte, id string, updated time.Time, name, info []byte) (domain.Note, error) {
	noteID, err := uuid.Parse(id)
	if err != nil {
		return domain.Note{}, fmt.Errorf("%w: note id %q", errNoteDoesNotOpen, id)
	}

	n := domain.Note{ID: noteID, Updated: updated.UTC()}

	nameBytes, err := sealbox.Open(key, name, aadNote("name", id))
	if err != nil {
		return domain.Note{}, fmt.Errorf("%w: %s: %w", errNoteDoesNotOpen, id, err)
	}

	infoBytes, err := sealbox.Open(key, info, aadNote("info", id))
	if err != nil {
		return domain.Note{}, fmt.Errorf("%w: %s: %w", errNoteDoesNotOpen, id, err)
	}

	n.Name, n.Info = string(nameBytes), string(infoBytes)

	return n, nil
}
