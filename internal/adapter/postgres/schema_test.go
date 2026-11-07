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
