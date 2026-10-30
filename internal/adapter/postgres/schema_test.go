package postgres

import (
	"bytes"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// testKDF is the lowest allowed cost, to keep tests fast.
var testKDF = sealbox.KDFParams{Time: 1, MemoryKiB: 19456, Threads: 1}

const testPhrase = "sixteen chars ok"

func TestNewMetaOpensWithSamePhrase(t *testing.T) {
	m, key, err := newMeta(testPhrase, testKDF)
	if err != nil {
		t.Fatalf("newMeta: %v", err)
	}
	if m.ServerID == uuid.Nil || m.FormatVersion != formatVersion || len(m.Salt) != sealbox.SaltSize {
		t.Errorf("meta = %+v", m)
	}
	got, err := m.unlock(testPhrase)
	if err != nil {
		t.Fatalf("unlock: %v", err)
	}
	if !bytes.Equal(got, key) {
		t.Error("the same phrase derived another key")
	}
	if _, err := m.unlock("another phrase 16"); !errors.Is(err, domain.ErrWrongSyncPhrase) {
		t.Errorf("unlock with another phrase = %v, want ErrWrongSyncPhrase", err)
	}
}

func TestUnlockNFCAndNFD(t *testing.T) {
	nfc, nfd := "passéword phrase", "passéword phrase"
	m, _, err := newMeta(nfc, testKDF)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.unlock(nfd); err != nil {
		t.Errorf("unlock with the NFD form: %v", err)
	}
}

func TestMetaCheck(t *testing.T) {
	good, _, err := newMeta(testPhrase, testKDF)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		modify func(*meta)
	}{
		{name: "newer format", modify: func(m *meta) { m.FormatVersion = 2 }},
		{name: "format 0", modify: func(m *meta) { m.FormatVersion = 0 }},
		{name: "memory out of range", modify: func(m *meta) { m.KDF.MemoryKiB = 4194304 }},
		{name: "time 0", modify: func(m *meta) { m.KDF.Time = 0 }},
		{name: "salt of 15 bytes", modify: func(m *meta) { m.Salt = m.Salt[:15] }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := good
			m.Salt = bytes.Clone(good.Salt)
			tt.modify(&m)
			if _, err := m.unlock(testPhrase); !errors.Is(err, domain.ErrUnsupportedFormat) {
				t.Errorf("unlock = %v, want ErrUnsupportedFormat", err)
			}
		})
	}
}

func TestParseKDF(t *testing.T) {
	p, err := parseKDF([]byte(`{"t":3,"m":65536,"p":4}`))
	if err != nil || p != sealbox.DefaultKDF() {
		t.Errorf("parseKDF = %+v, %v", p, err)
	}
	if _, err := parseKDF([]byte(`{`)); !errors.Is(err, domain.ErrUnsupportedFormat) {
		t.Errorf("parseKDF of broken json = %v, want ErrUnsupportedFormat", err)
	}
}

func TestNoteFieldsBoundToNote(t *testing.T) {
	key := bytes.Repeat([]byte{1}, sealbox.KeySize)
	n := domain.Note{ID: uuid.New(), Name: "n", Info: "i"}
	name, info, err := sealNote(key, n)
	if err != nil {
		t.Fatal(err)
	}
	other := uuid.New()
	if _, err := openNote(key, other.String(), n.Updated, name, info); err == nil {
		t.Error("fields of one note opened as another note")
	}
	if _, err := openNote(key, n.ID.String(), n.Updated, info, name); err == nil {
		t.Error("name and text swapped opened")
	}
	got, err := openNote(key, n.ID.String(), n.Updated, name, info)
	if err != nil || got.Name != "n" || got.Info != "i" || got.ID != n.ID {
		t.Errorf("openNote = %+v, %v", got, err)
	}
}
