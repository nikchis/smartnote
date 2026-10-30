package sealbox

import (
	"bytes"
	"errors"
	"testing"
)

func testKey(b byte) []byte { return bytes.Repeat([]byte{b}, KeySize) }

var noteAAD = AAD{Table: "note", Column: "info", RowID: "u1"}

func TestAAD_bytes(t *testing.T) {
	tests := []struct {
		name string
		aad  AAD
		want string
	}{
		{name: "without owner", aad: AAD{Table: "note", Column: "info", RowID: "u1"}, want: "smartnote|1|note|info|u1"},
		{name: "with owner", aad: AAD{Table: "note_version", Column: "info", RowID: "u1", OwnerID: "n1"}, want: "smartnote|1|note_version|info|u1|n1"},
		{name: "meta row", aad: AAD{Table: "meta", Column: "wrapped_dek", RowID: "1"}, want: "smartnote|1|meta|wrapped_dek|1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.aad.bytes()
			if err != nil {
				t.Fatalf("bytes(): %v", err)
			}

			if string(got) != tt.want {
				t.Errorf("bytes() = %q, want %q", got, tt.want)
			}
		})
	}
}

// ambiguousAADs could share an encoding with another AAD, or bind a field to nothing.
var ambiguousAADs = []struct {
	name string
	aad  AAD
}{
	{name: "zero", aad: AAD{}},
	{name: "no table", aad: AAD{Column: "info", RowID: "u1"}},
	{name: "no column", aad: AAD{Table: "note", RowID: "u1"}},
	{name: "no row id", aad: AAD{Table: "note", Column: "info"}},
	{name: "separator in table", aad: AAD{Table: "notes|info", Column: "a", RowID: "b"}},
	{name: "separator in column", aad: AAD{Table: "note", Column: "info|a", RowID: "b"}},
	{name: "separator in row id", aad: AAD{Table: "note", Column: "info", RowID: "a|b"}},
	{name: "separator in owner id", aad: AAD{Table: "note", Column: "info", RowID: "a", OwnerID: "b|c"}},
}

func TestSealRejectsAmbiguousAAD(t *testing.T) {
	for _, tt := range ambiguousAADs {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Seal(testKey(1), []byte("x"), tt.aad); !errors.Is(err, ErrInvalidParams) {
				t.Errorf("Seal = %v, want ErrInvalidParams", err)
			}
		})
	}
}

func TestOpenRejectsAmbiguousAAD(t *testing.T) {
	sealed, err := Seal(testKey(1), []byte("x"), AAD{Table: "note", Column: "info", RowID: "a", OwnerID: "b"})
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	for _, tt := range ambiguousAADs {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Open(testKey(1), sealed, tt.aad)
			if !errors.Is(err, ErrOpen) {
				t.Errorf("Open = %v, want ErrOpen", err)
			}

			if got != nil {
				t.Errorf("Open returned plaintext %q", got)
			}
		})
	}
}

func TestSealOpenRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext []byte
	}{
		{name: "text", plaintext: []byte("Привет, 😀")},
		{name: "empty", plaintext: []byte{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sealed, err := Seal(testKey(1), tt.plaintext, noteAAD)
			if err != nil {
				t.Fatalf("Seal: %v", err)
			}

			got, err := Open(testKey(1), sealed, noteAAD)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}

			if !bytes.Equal(got, tt.plaintext) {
				t.Errorf("Open = %q, want %q", got, tt.plaintext)
			}

			if got == nil {
				t.Error("Open returned nil plaintext with a nil error")
			}
		})
	}
}

func TestSealFormat(t *testing.T) {
	pt := []byte("hello")

	sealed, err := Seal(testKey(1), pt, noteAAD)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if sealed[0] != FormatVersion {
		t.Errorf("version byte = %d, want %d", sealed[0], FormatVersion)
	}

	if want := 1 + 24 + len(pt) + 16; len(sealed) != want {
		t.Errorf("len = %d, want %d", len(sealed), want)
	}

	again, err := Seal(testKey(1), pt, noteAAD)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if bytes.Equal(sealed[1:25], again[1:25]) {
		t.Error("two seals use the same nonce")
	}
}

func TestSealRejectsBadKey(t *testing.T) {
	if _, err := Seal(make([]byte, KeySize-1), []byte("x"), noteAAD); !errors.Is(err, ErrInvalidParams) {
		t.Errorf("Seal with short key = %v, want ErrInvalidParams", err)
	}
}

func TestOpenRejects(t *testing.T) {
	sealed, err := Seal(testKey(1), []byte("secret note"), AAD{Table: "note_version", Column: "info", RowID: "v1", OwnerID: "n1"})
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	aad := AAD{Table: "note_version", Column: "info", RowID: "v1", OwnerID: "n1"}

	tampered := bytes.Clone(sealed)
	tampered[len(tampered)-20] ^= 0x01
	version2 := bytes.Clone(sealed)
	version2[0] = 0x02

	tests := []struct {
		name   string
		key    []byte
		sealed []byte
		aad    AAD
	}{
		{name: "changed ciphertext byte", key: testKey(1), sealed: tampered, aad: aad},
		{name: "other row id", key: testKey(1), sealed: sealed, aad: AAD{Table: "note_version", Column: "info", RowID: "v2", OwnerID: "n1"}},
		{name: "other owner id", key: testKey(1), sealed: sealed, aad: AAD{Table: "note_version", Column: "info", RowID: "v1", OwnerID: "n2"}},
		{name: "no owner id", key: testKey(1), sealed: sealed, aad: AAD{Table: "note_version", Column: "info", RowID: "v1"}},
		{name: "other column", key: testKey(1), sealed: sealed, aad: AAD{Table: "note_version", Column: "name", RowID: "v1", OwnerID: "n1"}},
		{name: "other key", key: testKey(2), sealed: sealed, aad: aad},
		{name: "short key", key: testKey(1)[:16], sealed: sealed, aad: aad},
		{name: "empty input", key: testKey(1), sealed: nil, aad: aad},
		{name: "truncated input", key: testKey(1), sealed: sealed[:1+24+15], aad: aad},
		{name: "version 2", key: testKey(1), sealed: version2, aad: aad},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Open(tt.key, tt.sealed, tt.aad)
			if !errors.Is(err, ErrOpen) {
				t.Errorf("Open = %v, want ErrOpen", err)
			}

			if got != nil {
				t.Errorf("Open returned plaintext %q", got)
			}
		})
	}
}

// FuzzOpen checks that Open never panics, that every failure is ErrOpen
// without plaintext, and that anything it opens is the sealed text. Fuzz
// workers re-run this setup with a new nonce, so the check is on the plaintext.
func FuzzOpen(f *testing.F) {
	key := testKey(1)
	plaintext := []byte("secret note")

	authentic, err := Seal(key, plaintext, noteAAD)
	if err != nil {
		f.Fatalf("Seal: %v", err)
	}

	f.Add(authentic)
	f.Add([]byte{})
	f.Add([]byte{FormatVersion})
	f.Add(authentic[:len(authentic)-1])

	f.Fuzz(func(t *testing.T, sealed []byte) {
		got, err := Open(key, sealed, noteAAD)
		if err == nil {
			if !bytes.Equal(got, plaintext) {
				t.Fatalf("Open(%x) = %q, want %q", sealed, got, plaintext)
			}

			return
		}

		if !errors.Is(err, ErrOpen) {
			t.Errorf("Open = %v, want ErrOpen", err)
		}

		if got != nil {
			t.Errorf("Open returned plaintext %q with an error", got)
		}
	})
}

func TestWipe(t *testing.T) {
	b := []byte{1, 2, 3}
	Wipe(b)

	if !bytes.Equal(b, []byte{0, 0, 0}) {
		t.Errorf("Wipe left %v", b)
	}

	Wipe(nil)
}
