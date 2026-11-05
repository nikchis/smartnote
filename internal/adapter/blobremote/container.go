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
	"math"
	"strconv"
	"strings"
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
	if len(file) > maxFile {
		return nil, m, nil, damaged("container file too large")
	}

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

	id, ok := canonicalUUID(h.ServerUUID)
	if !ok {
		return nil, m, nil, damaged("container server_uuid")
	}

	if again, err := json.Marshal(h); err != nil || !bytes.Equal(again, header) {
		return nil, m, nil, damaged("container header is not canonical")
	}

	if err := h.KDF.Validate(); err != nil {
		return nil, m, nil, fmt.Errorf("container kdf_params: %w: %w", domain.ErrUnsupportedFormat, err)
	}

	m = remotecrypto.Meta{ServerID: id, FormatVersion: h.FormatVersion, KDF: h.KDF, Salt: h.KDFSalt, KeyCheck: h.KeyCheck}

	return header, m, body, nil
}

// checkFormatVersion reads format_version alone (spec §4.2 step 4): a newer
// header may carry fields this version does not know. Any number above 1,
// whatever its form, is newer; a number below 1, null, a missing field or a
// non-number is damage.
func checkFormatVersion(header []byte) error {
	var probe struct {
		FormatVersion json.RawMessage `json:"format_version"`
	}

	if err := json.Unmarshal(header, &probe); err != nil {
		return damaged("container header")
	}

	switch cmp, ok := compareToOne(probe.FormatVersion); {
	case !ok || cmp < 0:
		return damaged("container format_version")
	case cmp > 0:
		return fmt.Errorf("container format_version: %w", domain.ErrUnsupportedFormat)
	}

	return nil
}

// compareToOne compares a JSON number literal with 1,
// returning -1, 0 or 1. ok is false when lit is not a number. It works on the
// decimal digits, so no form of the number can overflow.
func compareToOne(lit json.RawMessage) (cmp int, ok bool) {
	if len(lit) == 0 || !json.Valid(lit) || (lit[0] != '-' && (lit[0] < '0' || lit[0] > '9')) {
		return 0, false
	}

	s := string(lit)
	if s[0] == '-' {
		return -1, true
	}

	mant, expText, _ := strings.Cut(strings.ToLower(s), "e")
	intPart, frac, _ := strings.Cut(mant, ".")

	exp := 0

	if expText != "" {
		neg := expText[0] == '-'
		digits := strings.TrimLeft(expText, "+-")

		if len(digits) > 9 {
			digits = "999999999"
		}

		exp, _ = strconv.Atoi(digits)
		if neg {
			exp = -exp
		}
	}

	// value = 0.<digits> * 10^pos
	all := intPart + frac
	pos := len(intPart) + exp
	trimmed := strings.TrimLeft(all, "0")
	pos -= len(all) - len(trimmed)
	trimmed = strings.TrimRight(trimmed, "0")

	switch {
	case trimmed == "":
		return -1, true
	case pos > 1:
		return 1, true
	case pos < 1:
		return -1, true
	case trimmed == "1":
		return 0, true
	default:
		return 1, true
	}
}

// canonicalUUID parses the lowercase hyphenated form of a non-nil UUID.
func canonicalUUID(s string) (uuid.UUID, bool) {
	id, err := uuid.Parse(s)
	if err != nil || id == uuid.Nil || id.String() != s {
		return uuid.Nil, false
	}

	return id, true
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

	jsonLen, err := lengthU32(len(js))
	if err != nil {
		return nil, err
	}

	size := (4 + len(js) + padBlock - 1) / padBlock * padBlock

	plain := make([]byte, size)
	defer sealbox.Wipe(plain)

	binary.BigEndian.PutUint32(plain, jsonLen)
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

	if b.Notes == nil {
		return 0, nil, damaged("container notes")
	}

	if again, err := json.Marshal(b); err != nil || !bytes.Equal(again, plain[4:end]) {
		return 0, nil, damaged("container body is not canonical")
	}

	if b.Revision < 0 {
		return 0, nil, damaged("container revision")
	}

	if id, ok := canonicalUUID(b.ServerUUID); !ok || id != serverID {
		return 0, nil, damaged("container body server_uuid")
	}

	notes = make([]domain.Note, len(b.Notes))

	for i, n := range b.Notes {
		id, ok := canonicalUUID(n.ID)
		if !ok {
			return 0, nil, damaged("container note id")
		}

		notes[i] = domain.Note{ID: id, Name: n.Name, Info: n.Info, Updated: time.UnixMilli(n.Updated).UTC()}
	}

	return b.Revision, notes, nil
}

// lengthU32 converts a length to the uint32 of the layout.
func lengthU32(n int) (uint32, error) {
	if n < 0 || n > math.MaxUint32 {
		return 0, errors.New("container length does not fit uint32")
	}

	return uint32(n), nil
}

// assemble builds the container file. The result must not exceed maxFile.
func assemble(header, body []byte) ([]byte, error) {
	if len(header) < 1 || len(header) > maxHeader {
		return nil, fmt.Errorf("container header of %d bytes", len(header))
	}

	headerLen, err := lengthU32(len(header))
	if err != nil {
		return nil, err
	}

	if int64(fixedPart)+int64(len(header))+int64(len(body)) > maxFile {
		return nil, fmt.Errorf("container exceeds %d bytes: %w", maxFile, domain.ErrRemoteTooLarge)
	}

	file := make([]byte, 0, fixedPart+len(header)+len(body))
	file = append(file, prefix...)
	file = append(file, layoutVersion)
	file = binary.BigEndian.AppendUint32(file, headerLen)
	file = append(file, header...)
	file = append(file, body...)

	return file, nil
}
