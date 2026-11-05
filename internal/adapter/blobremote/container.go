// Package blobremote is the remote storage on an object store: one encrypted
// container file (spec §4, §5).
package blobremote

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/adapter/remotecrypto"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// Container limits and layout (spec §4.2).
const (
	maxFile   = 64 << 20 // the largest container file
	maxHeader = 4096     // the largest header
	padBlock  = 65536    // the plaintext of the body is a multiple of this

	prefix        = "SNREMOT"
	layoutVersion = '1'
	fixedPart     = len(prefix) + 1 + 4 // prefix, layout, header_len
)

// container is a parsed remote storage: the header bytes exactly as stored,
// its meta, and the decrypted body.
type container struct {
	header   []byte
	meta     remotecrypto.Meta
	revision int64
	notes    []domain.Note
}

// headerJSON is the header; the field order is the order newHeader writes.
type headerJSON struct {
	FormatVersion int               `json:"format_version"`
	ServerUUID    string            `json:"server_uuid"`
	KDF           sealbox.KDFParams `json:"kdf_params"`
	KDFSalt       []byte            `json:"kdf_salt"`
	KeyCheck      []byte            `json:"key_check"`
}

// bodyJSON and noteJSON are the plaintext of the body.
type bodyJSON struct {
	Revision   int64      `json:"revision"`
	ServerUUID string     `json:"server_uuid"`
	Notes      []noteJSON `json:"notes"`
}

type noteJSON struct {
	ID      string `json:"id"`
	Updated int64  `json:"updated"`
	Name    string `json:"name"`
	Info    string `json:"info"`
}

// damaged returns an ErrRemoteDamaged with a reason that never holds content.
func damaged(reason string) error {
	return fmt.Errorf("%s: %w", reason, domain.ErrRemoteDamaged)
}

// newHeader serializes m into the header of a new container.
func newHeader(m remotecrypto.Meta) ([]byte, error) {
	h, err := json.Marshal(headerJSON{
		FormatVersion: m.FormatVersion,
		ServerUUID:    m.ServerID.String(),
		KDF:           m.KDF,
		KDFSalt:       m.Salt,
		KeyCheck:      m.KeyCheck,
	})
	if err != nil {
		return nil, fmt.Errorf("encode container header: %w", err)
	}

	if len(h) > maxHeader {
		return nil, fmt.Errorf("container header of %d bytes exceeds %d", len(h), maxHeader)
	}

	return h, nil
}

// parseHeader does steps 1-6 of spec §4.2. The returned header and body alias
// file. The salt size and the key check are left to Meta.Unlock.
func parseHeader(file []byte) (header []byte, m remotecrypto.Meta, body []byte, err error) {
	if len(file) < fixedPart || string(file[:len(prefix)]) != prefix {
		return nil, m, nil, damaged("container prefix")
	}

	switch layout := file[len(prefix)]; {
	case layout > layoutVersion:
		return nil, m, nil, fmt.Errorf("container layout %q: %w", layout, domain.ErrUnsupportedFormat)
	case layout < layoutVersion:
		return nil, m, nil, damaged("container layout")
	}

	n := binary.BigEndian.Uint32(file[len(prefix)+1:])
	if n < 1 || n > maxHeader || int64(n) > int64(len(file)-fixedPart) {
		return nil, m, nil, damaged("container header length")
	}

	header = file[fixedPart : fixedPart+int(n)]
	body = file[fixedPart+int(n):]

	if err := checkFormatVersion(header); err != nil {
		return nil, m, nil, err
	}

	var h headerJSON
	if err := decodeStrict(header, &h); err != nil {
		return nil, m, nil, damaged("container header")
	}

	id, err := uuid.Parse(h.ServerUUID)
	if err != nil {
		return nil, m, nil, damaged("container server_uuid")
	}

	if err := h.KDF.Validate(); err != nil {
		return nil, m, nil, fmt.Errorf("container kdf_params: %w: %w", domain.ErrUnsupportedFormat, err)
	}

	m = remotecrypto.Meta{ServerID: id, FormatVersion: h.FormatVersion, KDF: h.KDF, Salt: h.KDFSalt, KeyCheck: h.KeyCheck}

	return header, m, body, nil
}

// checkFormatVersion reads format_version alone (spec §4.2 step 4): a newer
// header may carry fields this version does not know.
func checkFormatVersion(header []byte) error {
	var probe struct {
		FormatVersion json.RawMessage `json:"format_version"`
	}

	if err := json.Unmarshal(header, &probe); err != nil {
		return damaged("container header")
	}

	var v int64
	if len(probe.FormatVersion) == 0 || json.Unmarshal(probe.FormatVersion, &v) != nil {
		return damaged("container format_version")
	}

	switch {
	case v > remotecrypto.FormatVersion:
		return fmt.Errorf("container format_version %d: %w", v, domain.ErrUnsupportedFormat)
	case v < remotecrypto.FormatVersion:
		return damaged("container format_version")
	}

	return nil
}

// decodeStrict decodes one JSON value into v, refusing unknown fields and
// anything after the value. Its error carries no content.
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		return errors.New("invalid JSON")
	}

	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("data after JSON")
	}

	return nil
}

// bodyAAD binds the body to the exact header bytes and the server.
func bodyAAD(header []byte, serverID uuid.UUID) sealbox.AAD {
	sum := sha256.Sum256(header)

	return sealbox.AAD{Table: "container", Column: "body", RowID: serverID.String(), OwnerID: hex.EncodeToString(sum[:])}
}

// marshalBody returns the JSON of the body.
func marshalBody(serverID uuid.UUID, revision int64, notes []domain.Note) ([]byte, error) {
	if revision < 0 {
		return nil, errors.New("negative revision")
	}

	b := bodyJSON{Revision: revision, ServerUUID: serverID.String(), Notes: make([]noteJSON, len(notes))}
	for i, n := range notes {
		b.Notes[i] = noteJSON{ID: n.ID.String(), Updated: n.Updated.UnixMilli(), Name: n.Name, Info: n.Info}
	}

	js, err := json.Marshal(b)
	if err != nil {
		return nil, errors.New("encode container body")
	}

	return js, nil
}

// sealBody encrypts the notes with the revision into a body: json_len, JSON and
// zero padding up to a multiple of padBlock.
func sealBody(key, header []byte, serverID uuid.UUID, revision int64, notes []domain.Note) ([]byte, error) {
	js, err := marshalBody(serverID, revision, notes)
	if err != nil {
		return nil, err
	}

	defer sealbox.Wipe(js)

	if int64(len(js)) > int64(^uint32(0)) {
		return nil, errors.New("container body too large")
	}

	size := (4 + len(js) + padBlock - 1) / padBlock * padBlock
	plain := make([]byte, size)
	defer sealbox.Wipe(plain)

	binary.BigEndian.PutUint32(plain, uint32(len(js)))
	copy(plain[4:], js)

	return sealbox.Seal(key, plain, bodyAAD(header, serverID))
}

// openBody decrypts and checks a body. Every failure is ErrRemoteDamaged: the
// key already opened the key check.
func openBody(key, header []byte, serverID uuid.UUID, body []byte) (revision int64, notes []domain.Note, err error) {
	plain, err := sealbox.Open(key, body, bodyAAD(header, serverID))
	if err != nil {
		return 0, nil, damaged("container body does not open")
	}

	defer sealbox.Wipe(plain)

	if len(plain) == 0 || len(plain)%padBlock != 0 {
		return 0, nil, damaged("container body alignment")
	}

	n := binary.BigEndian.Uint32(plain)
	if int64(n) > int64(len(plain)-4) {
		return 0, nil, damaged("container json_len")
	}

	end := 4 + int(n)
	if bytes.Count(plain[end:], []byte{0}) != len(plain)-end {
		return 0, nil, damaged("container padding")
	}

	var b bodyJSON
	if err := decodeStrict(plain[4:end], &b); err != nil {
		return 0, nil, damaged("container body JSON")
	}

	if b.Revision < 0 {
		return 0, nil, damaged("container revision")
	}

	if id, err := uuid.Parse(b.ServerUUID); err != nil || id != serverID {
		return 0, nil, damaged("container body server_uuid")
	}

	notes = make([]domain.Note, len(b.Notes))

	for i, n := range b.Notes {
		id, err := uuid.Parse(n.ID)
		if err != nil {
			return 0, nil, damaged("container note id")
		}

		notes[i] = domain.Note{ID: id, Name: n.Name, Info: n.Info, Updated: time.UnixMilli(n.Updated).UTC()}
	}

	return b.Revision, notes, nil
}

// assemble builds the container file. The result must not exceed maxFile.
func assemble(header, body []byte) ([]byte, error) {
	if len(header) < 1 || len(header) > maxHeader {
		return nil, fmt.Errorf("container header of %d bytes", len(header))
	}

	if int64(fixedPart)+int64(len(header))+int64(len(body)) > maxFile {
		return nil, fmt.Errorf("container exceeds %d bytes: %w", maxFile, domain.ErrRemoteTooLarge)
	}

	file := make([]byte, 0, fixedPart+len(header)+len(body))
	file = append(file, prefix...)
	file = append(file, layoutVersion)
	file = binary.BigEndian.AppendUint32(file, uint32(len(header)))
	file = append(file, header...)
	file = append(file, body...)

	return file, nil
}
