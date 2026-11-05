package blobremote

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/adapter/remotecrypto"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// testKDF is the lowest allowed cost, to keep tests fast.
var testKDF = sealbox.KDFParams{Time: 1, MemoryKiB: 19456, Threads: 1}

const testPhrase = "sixteen chars ok"

func testMeta(t *testing.T) (meta remotecrypto.Meta, key []byte) {
	t.Helper()

	m, key, err := remotecrypto.NewMeta(testPhrase, testKDF)
	if err != nil {
		t.Fatalf("NewMeta: %v", err)
	}

	return m, key
}

// rawFile builds a container file field by field, without any check.
func rawFile(prefix string, layout byte, hlen uint32, header, body []byte) []byte {
	var b bytes.Buffer

	b.WriteString(prefix)
	b.WriteByte(layout)
	_ = binary.Write(&b, binary.BigEndian, hlen)
	b.Write(header)
	b.Write(body)

	return b.Bytes()
}

func goodFile(header, body []byte) []byte {
	return rawFile("SNREMOT", '1', uint32(len(header)), header, body)
}

func TestParseHeaderOrder(t *testing.T) {
	m, _ := testMeta(t)

	header, err := newHeader(m)
	if err != nil {
		t.Fatalf("newHeader: %v", err)
	}

	id := m.ServerID.String()
	salt := base64Of(m.Salt)
	check := base64Of(m.KeyCheck)
	hdr := func(s string) []byte { return []byte(s) }
	full := func(fv, extra, kdf string) []byte {
		if kdf == "" {
			kdf = `{"t":3,"m":65536,"p":4}`
		}

		return hdr(fmt.Sprintf(`{%s"server_uuid":%q,"kdf_params":%s,"kdf_salt":%q,"key_check":%q%s}`,
			fv, id, kdf, salt, check, extra))
	}

	tests := []struct {
		name string
		file []byte
		want error // nil: parsed
	}{
		{"valid", goodFile(header, []byte("body")), nil},
		{"no prefix", rawFile("SNREMOX", '1', uint32(len(header)), header, nil), domain.ErrRemoteDamaged},
		{"short file", []byte("SNREMOT1\x00\x00"), domain.ErrRemoteDamaged},
		{"empty file", nil, domain.ErrRemoteDamaged},
		{"layout 2", rawFile("SNREMOT", '2', uint32(len(header)), header, nil), domain.ErrUnsupportedFormat},
		{"layout 0", rawFile("SNREMOT", '0', uint32(len(header)), header, nil), domain.ErrRemoteDamaged},
		{"header_len 0", rawFile("SNREMOT", '1', 0, header, nil), domain.ErrRemoteDamaged},
		{"header_len 4097", rawFile("SNREMOT", '1', 4097, bytes.Repeat([]byte(" "), 5000), nil), domain.ErrRemoteDamaged},
		{"header_len beyond file", rawFile("SNREMOT", '1', uint32(len(header)+1), header, nil), domain.ErrRemoteDamaged},
		{"version 2 unknown field", goodFile(full(`"format_version":2,`, `,"new":1`, ""), nil), domain.ErrUnsupportedFormat},
		{"version 2 not a header", goodFile(hdr(`{"format_version":2}`), nil), domain.ErrUnsupportedFormat},
		{"version 0", goodFile(full(`"format_version":0,`, "", ""), nil), domain.ErrRemoteDamaged},
		{"version string", goodFile(full(`"format_version":"1",`, "", ""), nil), domain.ErrRemoteDamaged},
		{"version 1.5", goodFile(full(`"format_version":1.5,`, "", ""), nil), domain.ErrUnsupportedFormat},
		{"version 1.0 not canonical", goodFile(full(`"format_version":1.0,`, "", ""), nil), domain.ErrRemoteDamaged},
		{"version 2e0", goodFile(full(`"format_version":2e0,`, "", ""), nil), domain.ErrUnsupportedFormat},
		{"version 0.5", goodFile(full(`"format_version":0.5,`, "", ""), nil), domain.ErrRemoteDamaged},
		{"version -3", goodFile(full(`"format_version":-3,`, "", ""), nil), domain.ErrRemoteDamaged},
		{"version 1.0000000001", goodFile(full(`"format_version":1.0000000001,`, "", ""), nil), domain.ErrUnsupportedFormat},
		{"version beyond int64", goodFile(full(`"format_version":99999999999999999999999,`, "", ""), nil), domain.ErrUnsupportedFormat},
		{"version huge exponent", goodFile(full(`"format_version":1e999999999999,`, "", ""), nil), domain.ErrUnsupportedFormat},
		{"version tiny exponent", goodFile(full(`"format_version":1e-999999999999,`, "", ""), nil), domain.ErrRemoteDamaged},
		{"version null", goodFile(full(`"format_version":null,`, "", ""), nil), domain.ErrRemoteDamaged},
		{"version true", goodFile(full(`"format_version":true,`, "", ""), nil), domain.ErrRemoteDamaged},
		{"case-variant key", goodFile(hdr(strings.Replace(string(full(`"format_version":1,`, "", "")), "server_uuid", "Server_UUID", 1)), nil), domain.ErrRemoteDamaged},
		{"duplicate key", goodFile(full(`"format_version":1,"format_version":1,`, "", ""), nil), domain.ErrRemoteDamaged},
		{"whitespace", goodFile(hdr(strings.Replace(string(full(`"format_version":1,`, "", "")), ",", ", ", 1)), nil), domain.ErrRemoteDamaged},
		{"missing kdf_params", goodFile(hdr(fmt.Sprintf(`{"format_version":1,"server_uuid":%q,"kdf_salt":%q,"key_check":%q}`, id, salt, check)), nil), domain.ErrRemoteDamaged},
		{"uppercase uuid", goodFile(hdr(strings.Replace(string(full(`"format_version":1,`, "", "")), id, strings.ToUpper(id), 1)), nil), domain.ErrRemoteDamaged},
		{"nil uuid", goodFile(hdr(strings.Replace(string(full(`"format_version":1,`, "", "")), id, uuid.Nil.String(), 1)), nil), domain.ErrRemoteDamaged},
		{"braced uuid", goodFile(hdr(strings.Replace(string(full(`"format_version":1,`, "", "")), id, "{"+id+"}", 1)), nil), domain.ErrRemoteDamaged},
		{"oversized file", append(goodFile(header, nil), make([]byte, maxFile)...), domain.ErrRemoteDamaged},
		{"version missing", goodFile(full("", "", ""), nil), domain.ErrRemoteDamaged},
		{"header not JSON", goodFile(hdr(`not json`), nil), domain.ErrRemoteDamaged},
		{"unknown field v1", goodFile(full(`"format_version":1,`, `,"new":1`, ""), nil), domain.ErrRemoteDamaged},
		{"trailing data", goodFile(append(full(`"format_version":1,`, "", ""), []byte(` {}`)...), nil), domain.ErrRemoteDamaged},
		{"bad uuid", goodFile(hdr(strings.Replace(string(full(`"format_version":1,`, "", "")), id, "nope", 1)), nil), domain.ErrRemoteDamaged},
		{"kdf m too small", goodFile(full(`"format_version":1,`, "", `{"t":3,"m":1,"p":4}`), nil), domain.ErrUnsupportedFormat},
		{"kdf m too large", goodFile(full(`"format_version":1,`, "", `{"t":3,"m":4294967295,"p":4}`), nil), domain.ErrUnsupportedFormat},
		{"valid by hand", goodFile(full(`"format_version":1,`, "", ""), nil), nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, meta, body, err := parseHeader(tc.file)

			if tc.want == nil {
				if err != nil {
					t.Fatalf("parseHeader: %v", err)
				}

				if len(h) == 0 || meta.ServerID == uuid.Nil || meta.FormatVersion != 1 || body == nil && len(tc.file) > 12+len(h) {
					t.Errorf("h=%d meta=%+v body=%d", len(h), meta, len(body))
				}

				return
			}

			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}

			other := domain.ErrUnsupportedFormat
			if errors.Is(tc.want, domain.ErrUnsupportedFormat) {
				other = domain.ErrRemoteDamaged
			}

			if errors.Is(err, other) {
				t.Errorf("err %v matches both classes", err)
			}
		})
	}
}

func base64Of(b []byte) string {
	out, _ := json.Marshal(b)
	return strings.Trim(string(out), `"`)
}

func TestHeaderRoundTripAndOrder(t *testing.T) {
	m, _ := testMeta(t)

	header, err := newHeader(m)
	if err != nil {
		t.Fatalf("newHeader: %v", err)
	}

	order := []string{"format_version", "server_uuid", "kdf_params", "kdf_salt", "key_check"}
	last := -1

	for _, k := range order {
		i := bytes.Index(header, []byte(`"`+k+`"`))
		if i <= last {
			t.Fatalf("key %s out of order in %s", k, header)
		}

		last = i
	}

	h, got, body, err := parseHeader(goodFile(header, []byte("xyz")))
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}

	if !bytes.Equal(h, header) || string(body) != "xyz" {
		t.Error("header or body bytes changed")
	}

	if got.ServerID != m.ServerID || got.KDF != m.KDF || !bytes.Equal(got.Salt, m.Salt) || !bytes.Equal(got.KeyCheck, m.KeyCheck) {
		t.Errorf("meta = %+v, want %+v", got, m)
	}
}

func note(i int, info string) domain.Note {
	return domain.Note{
		ID:      uuid.MustParse(fmt.Sprintf("00000000-0000-4000-8000-%012d", i)),
		Name:    fmt.Sprintf("name %d", i),
		Info:    info,
		Updated: time.UnixMilli(1_700_000_000_123 + int64(i)).UTC(),
	}
}

func TestBodyRoundTrip(t *testing.T) {
	m, key := testMeta(t)

	header, err := newHeader(m)
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range []int{0, 1, 300} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			notes := make([]domain.Note, n)
			for i := range notes {
				notes[i] = note(i, "text é \"q\" \n"+strings.Repeat("x", i))
			}

			body, err := sealBody(key, header, m.ServerID, 42, notes)
			if err != nil {
				t.Fatalf("sealBody: %v", err)
			}

			rev, got, err := openBody(key, header, m.ServerID, body)
			if err != nil {
				t.Fatalf("openBody: %v", err)
			}

			if rev != 42 || len(got) != n {
				t.Fatalf("rev=%d notes=%d", rev, len(got))
			}

			for i := range notes {
				if got[i] != notes[i] || got[i].Updated.Location() != time.UTC {
					t.Fatalf("note %d = %+v, want %+v", i, got[i], notes[i])
				}
			}
		})
	}
}

// sealPlain seals arbitrary plaintext exactly like sealBody does.
func sealPlain(t *testing.T, key, header []byte, id uuid.UUID, plain []byte) []byte {
	t.Helper()

	out, err := sealbox.Seal(key, plain, bodyAAD(header, id))
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func plainOf(jsonText string, pad int) []byte {
	p := make([]byte, 4, padBlock)
	binary.BigEndian.PutUint32(p, uint32(len(jsonText)))
	p = append(p, jsonText...)

	for len(p) < pad*padBlock {
		p = append(p, 0)
	}

	return p
}

func TestBodyPadding(t *testing.T) {
	m, key := testMeta(t)
	header, _ := newHeader(m)
	js := fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[]}`, m.ServerID)

	body, err := sealBody(key, header, m.ServerID, 1, []domain.Note{note(1, "a")})
	if err != nil {
		t.Fatal(err)
	}

	if (len(body)-41)%padBlock != 0 || len(body)-41 == 0 {
		t.Errorf("plaintext length %d is not a positive multiple of %d", len(body)-41, padBlock)
	}

	tests := []struct {
		name  string
		plain []byte
		ok    bool
	}{
		{"zero padding", plainOf(js, 1), true},
		{"nonzero padding", func() []byte { p := plainOf(js, 1); p[len(p)-1] = 1; return p }(), false},
		{"first padding byte", func() []byte { p := plainOf(js, 1); p[4+len(js)] = 7; return p }(), false},
		{"not a multiple", plainOf(js, 1)[:padBlock-1], false},
		{"too short", []byte{0, 0}, false},
		{"empty", []byte{}, false},
		{"json_len too big", func() []byte { p := plainOf(js, 1); binary.BigEndian.PutUint32(p, padBlock); return p }(), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := openBody(key, header, m.ServerID, sealPlain(t, key, header, m.ServerID, tc.plain))
			if tc.ok && err != nil {
				t.Fatalf("openBody: %v", err)
			}

			if !tc.ok && !errors.Is(err, domain.ErrRemoteDamaged) {
				t.Fatalf("err = %v, want ErrRemoteDamaged", err)
			}
		})
	}
}

func TestBodyAADBinding(t *testing.T) {
	m, key := testMeta(t)
	header, _ := newHeader(m)

	body, err := sealBody(key, header, m.ServerID, 3, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The same values in another order.
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(header, &fields); err != nil {
		t.Fatal(err)
	}

	reordered := []byte(fmt.Sprintf(`{"server_uuid":%s,"format_version":%s,"kdf_params":%s,"kdf_salt":%s,"key_check":%s}`,
		fields["server_uuid"], fields["format_version"], fields["kdf_params"], fields["kdf_salt"], fields["key_check"]))

	tests := []struct {
		name   string
		header []byte
		id     uuid.UUID
		body   []byte
	}{
		{"other server_uuid", header, uuid.New(), body},
		{"reordered header", reordered, m.ServerID, body},
		{"spaced header", append(append([]byte{}, header...), ' '), m.ServerID, body},
		{"flipped body bit", header, m.ServerID, func() []byte { b := bytes.Clone(body); b[len(b)/2] ^= 1; return b }()},
		{"truncated body", header, m.ServerID, body[:len(body)-1]},
		{"empty body", header, m.ServerID, nil},
		{"other key", header, m.ServerID, nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := key
			if tc.name == "other key" {
				k = bytes.Repeat([]byte{9}, 32)
				tc.body = body
			}

			_, _, err := openBody(k, tc.header, tc.id, tc.body)
			if !errors.Is(err, domain.ErrRemoteDamaged) {
				t.Fatalf("err = %v, want ErrRemoteDamaged", err)
			}
		})
	}

	// The AAD is the documented one.
	sum := sealbox.AAD{Table: "container", Column: "body", RowID: m.ServerID.String()}
	if got := bodyAAD(header, m.ServerID); got.Table != sum.Table || got.Column != sum.Column || got.RowID != sum.RowID ||
		len(got.OwnerID) != 64 || got.OwnerID != strings.ToLower(got.OwnerID) {
		t.Errorf("bodyAAD = %+v", got)
	}

	if _, err := hex.DecodeString(bodyAAD(header, m.ServerID).OwnerID); err != nil {
		t.Error(err)
	}
}

func TestBodyStrictJSON(t *testing.T) {
	m, key := testMeta(t)
	header, _ := newHeader(m)
	id := m.ServerID.String()
	nid := uuid.New().String()

	tests := []struct {
		name string
		js   string
		ok   bool
	}{
		{"valid", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[{"id":%q,"updated":5,"name":"n","info":"i"}]}`, id, nid), true},
		{"unknown field", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[],"x":1}`, id), false},
		{"unknown note field", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[{"id":%q,"updated":5,"name":"n","info":"i","x":1}]}`, id, nid), false},
		{"trailing data", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[]} {}`, id), false},
		{"trailing garbage", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[]}x`, id), false},
		{"revision -1", fmt.Sprintf(`{"revision":-1,"server_uuid":%q,"notes":[]}`, id), false},
		{"revision missing", fmt.Sprintf(`{"server_uuid":%q,"notes":[]}`, id), false},
		{"notes missing", fmt.Sprintf(`{"revision":1,"server_uuid":%q}`, id), false},
		{"notes null", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":null}`, id), false},
		{"note field missing", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[{"id":%q,"updated":5,"name":"n"}]}`, id, nid), false},
		{"case-variant key", fmt.Sprintf(`{"Revision":1,"server_uuid":%q,"notes":[]}`, id), false},
		{"duplicate key", fmt.Sprintf(`{"revision":1,"revision":1,"server_uuid":%q,"notes":[]}`, id), false},
		{"whitespace", fmt.Sprintf(`{"revision":1, "server_uuid":%q,"notes":[]}`, id), false},
		{"field order", fmt.Sprintf(`{"server_uuid":%q,"revision":1,"notes":[]}`, id), false},
		{"uppercase server_uuid", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[]}`, strings.ToUpper(id)), false},
		{"uppercase note id", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[{"id":%q,"updated":5,"name":"n","info":"i"}]}`, id, strings.ToUpper(nid)), false},
		{"nil note id", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[{"id":%q,"updated":5,"name":"n","info":"i"}]}`, id, uuid.Nil), false},
		{"other server_uuid", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[]}`, uuid.New()), false},
		{"bad server_uuid", `{"revision":1,"server_uuid":"x","notes":[]}`, false},
		{"bad note id", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[{"id":"x","updated":5,"name":"n","info":"i"}]}`, id), false},
		{"updated string", fmt.Sprintf(`{"revision":1,"server_uuid":%q,"notes":[{"id":%q,"updated":"5","name":"n","info":"i"}]}`, id, nid), false},
		{"not JSON", `nope`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := openBody(key, header, m.ServerID, sealPlain(t, key, header, m.ServerID, plainOf(tc.js, 1)))
			if tc.ok && err != nil {
				t.Fatalf("openBody: %v", err)
			}

			if !tc.ok && !errors.Is(err, domain.ErrRemoteDamaged) {
				t.Fatalf("err = %v, want ErrRemoteDamaged", err)
			}

			if err != nil && strings.Contains(err.Error(), "nope") {
				t.Errorf("error leaks content: %v", err)
			}
		})
	}
}

func TestSizeLimitSynthetic(t *testing.T) {
	header := bytes.Repeat([]byte("h"), 100)

	ok, err := assemble(header, make([]byte, maxFile-12-len(header)))
	if err != nil {
		t.Fatalf("exactly maxFile: %v", err)
	}

	if len(ok) != maxFile || string(ok[:8]) != "SNREMOT1" || binary.BigEndian.Uint32(ok[8:12]) != 100 {
		t.Errorf("assembled len=%d prefix=%q", len(ok), ok[:12])
	}

	if _, err := assemble(header, make([]byte, maxFile-12-len(header)+1)); !errors.Is(err, domain.ErrRemoteTooLarge) {
		t.Fatalf("maxFile+1: %v, want ErrRemoteTooLarge", err)
	}
}

func TestLargestValidContainer(t *testing.T) {
	m, key := testMeta(t)
	header, _ := newHeader(m)

	const rev = 7

	blocks := (maxFile - 12 - len(header) - 41) / padBlock

	jsonLen := func(info int) int {
		js, err := marshalBody(m.ServerID, rev, []domain.Note{note(1, strings.Repeat("a", info))})
		if err != nil {
			t.Fatal(err)
		}

		return len(js)
	}

	base := jsonLen(0)
	// 4+json_len == blocks*padBlock fills the last block exactly.
	fit := blocks*padBlock - 4 - base
	over := fit + 1

	if got := 4 + jsonLen(fit); got != blocks*padBlock {
		t.Fatalf("fit plaintext = %d, want %d", got, blocks*padBlock)
	}

	body, err := sealBody(key, header, m.ServerID, rev, []domain.Note{note(1, strings.Repeat("a", fit))})
	if err != nil {
		t.Fatal(err)
	}

	file, err := assemble(header, body)
	if err != nil {
		t.Fatalf("assemble largest: %v", err)
	}

	if want := 12 + len(header) + 41 + padBlock*blocks; len(file) != want || len(file) > maxFile || maxFile-len(file) >= padBlock {
		t.Fatalf("len = %d, want %d (max %d)", len(file), want, maxFile)
	}

	h, meta, b, err := parseHeader(file)
	if err != nil {
		t.Fatal(err)
	}

	gotRev, notes, err := openBody(key, h, meta.ServerID, b)
	if err != nil || gotRev != rev || len(notes) != 1 || len(notes[0].Info) != fit {
		t.Fatalf("round trip: rev=%d notes=%d err=%v", gotRev, len(notes), err)
	}

	big, err := sealBody(key, header, m.ServerID, rev, []domain.Note{note(1, strings.Repeat("a", over))})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := assemble(header, big); !errors.Is(err, domain.ErrRemoteTooLarge) {
		t.Fatalf("N+1 blocks: %v, want ErrRemoteTooLarge", err)
	}
}
