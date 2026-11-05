package remotecrypto

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
	m, key, err := NewMeta(testPhrase, testKDF)
	if err != nil {
		t.Fatalf("NewMeta: %v", err)
	}

	if m.ServerID == uuid.Nil || m.FormatVersion != FormatVersion || len(m.Salt) != sealbox.SaltSize {
		t.Errorf("meta = %+v", m)
	}

	got, err := m.Unlock(testPhrase)
	if err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if !bytes.Equal(got, key) {
		t.Error("the same phrase derived another key")
	}

	if _, err := m.Unlock("another phrase 16"); !errors.Is(err, domain.ErrWrongSyncPhrase) {
		t.Errorf("Unlock with another phrase = %v, want ErrWrongSyncPhrase", err)
	}
}

func TestUnlockNFCAndNFD(t *testing.T) {
	nfc, nfd := "passéword phrase", "passéword phrase"

	m, _, err := NewMeta(nfc, testKDF)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := m.Unlock(nfd); err != nil {
		t.Errorf("Unlock with the NFD form: %v", err)
	}
}

func TestMetaCheck(t *testing.T) {
	good, _, err := NewMeta(testPhrase, testKDF)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		modify func(*Meta)
	}{
		{name: "newer format", modify: func(m *Meta) { m.FormatVersion = 2 }},
		{name: "format 0", modify: func(m *Meta) { m.FormatVersion = 0 }},
		{name: "memory out of range", modify: func(m *Meta) { m.KDF.MemoryKiB = 4194304 }},
		{name: "time 0", modify: func(m *Meta) { m.KDF.Time = 0 }},
		{name: "salt of 15 bytes", modify: func(m *Meta) { m.Salt = m.Salt[:15] }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := good
			m.Salt = bytes.Clone(good.Salt)
			tt.modify(&m)

			if _, err := m.Unlock(testPhrase); !errors.Is(err, domain.ErrUnsupportedFormat) {
				t.Errorf("Unlock = %v, want ErrUnsupportedFormat", err)
			}
		})
	}
}

func TestUnlockVersionZero(t *testing.T) {
	good, _, err := NewMeta(testPhrase, testKDF)
	if err != nil {
		t.Fatal(err)
	}

	good.FormatVersion = 0

	if _, err := good.Unlock(testPhrase); !errors.Is(err, domain.ErrUnsupportedFormat) {
		t.Errorf("Unlock of format 0 = %v, want ErrUnsupportedFormat", err)
	}
}

func TestKeyCheckAAD(t *testing.T) {
	// sealbox encodes this AAD as "smartnote|1|meta|key_check|1".
	want := sealbox.AAD{Table: "meta", Column: "key_check", RowID: "1"}
	if got := KeyCheckAAD(); got != want {
		t.Errorf("KeyCheckAAD = %+v, want %+v", got, want)
	}
}

func TestDeriveEncKeyDeterministic(t *testing.T) {
	salt := bytes.Repeat([]byte{7}, sealbox.SaltSize)

	a, err := DeriveEncKey(testPhrase, salt, testKDF)
	if err != nil {
		t.Fatal(err)
	}

	b, err := DeriveEncKey(testPhrase, salt, testKDF)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(a, b) || len(a) != sealbox.KeySize {
		t.Error("DeriveEncKey is not deterministic or has a wrong size")
	}
}
