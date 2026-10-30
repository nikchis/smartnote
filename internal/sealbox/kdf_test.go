package sealbox

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"
)

// testKDF is the lowest allowed cost, used to keep tests fast.
var testKDF = KDFParams{Time: 1, MemoryKiB: 19456, Threads: 1}

func TestKDFParams_Validate(t *testing.T) {
	tests := []struct {
		name    string
		params  KDFParams
		wantErr bool
	}{
		{name: "default", params: DefaultKDF()},
		{name: "time 0", params: KDFParams{Time: 0, MemoryKiB: 65536, Threads: 4}, wantErr: true},
		{name: "time 1", params: KDFParams{Time: 1, MemoryKiB: 65536, Threads: 4}},
		{name: "time 10", params: KDFParams{Time: 10, MemoryKiB: 65536, Threads: 4}},
		{name: "time 11", params: KDFParams{Time: 11, MemoryKiB: 65536, Threads: 4}, wantErr: true},
		{name: "memory 19455", params: KDFParams{Time: 3, MemoryKiB: 19455, Threads: 4}, wantErr: true},
		{name: "memory 19456", params: KDFParams{Time: 3, MemoryKiB: 19456, Threads: 4}},
		{name: "memory 262144", params: KDFParams{Time: 3, MemoryKiB: 262144, Threads: 4}},
		{name: "memory 262145", params: KDFParams{Time: 3, MemoryKiB: 262145, Threads: 4}, wantErr: true},
		{name: "threads 0", params: KDFParams{Time: 3, MemoryKiB: 65536, Threads: 0}, wantErr: true},
		{name: "threads 1", params: KDFParams{Time: 3, MemoryKiB: 65536, Threads: 1}},
		{name: "threads 8", params: KDFParams{Time: 3, MemoryKiB: 65536, Threads: 8}},
		{name: "threads 9", params: KDFParams{Time: 3, MemoryKiB: 65536, Threads: 9}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.params.Validate()
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidParams) {
					t.Errorf("Validate() = %v, want ErrInvalidParams", err)
				}

				return
			}

			if err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestNewSalt(t *testing.T) {
	a, err := NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}

	b, err := NewSalt()
	if err != nil {
		t.Fatalf("NewSalt: %v", err)
	}

	if len(a) != SaltSize {
		t.Errorf("len = %d, want %d", len(a), SaltSize)
	}

	if bytes.Equal(a, b) {
		t.Error("two salts are equal")
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	salt := bytes.Repeat([]byte{1}, SaltSize)
	otherSalt := bytes.Repeat([]byte{2}, SaltSize)

	k1, err := DeriveKey([]byte("secret"), salt, testKDF)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}

	k2, err := DeriveKey([]byte("secret"), salt, testKDF)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}

	k3, err := DeriveKey([]byte("secret"), otherSalt, testKDF)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}

	k4, err := DeriveKey([]byte("other secret"), salt, testKDF)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}

	if len(k1) != KeySize {
		t.Errorf("len = %d, want %d", len(k1), KeySize)
	}

	if !bytes.Equal(k1, k2) {
		t.Error("same input gave different keys")
	}

	if bytes.Equal(k1, k3) {
		t.Error("different salt gave the same key")
	}

	if bytes.Equal(k1, k4) {
		t.Error("different secret gave the same key")
	}
}

func TestDefaultKDFIsACopy(t *testing.T) {
	p := DefaultKDF()

	p.Time = 1
	if got := DefaultKDF().Time; got != 3 {
		t.Errorf("DefaultKDF().Time = %d after changing a copy, want 3", got)
	}
}

func TestDeriveKeyRejectsBadInput(t *testing.T) {
	tests := []struct {
		name   string
		secret []byte
		salt   []byte
		params KDFParams
	}{
		{name: "salt of 15 bytes", secret: []byte("secret"), salt: make([]byte, SaltSize-1), params: testKDF},
		{name: "salt of 17 bytes", secret: []byte("secret"), salt: make([]byte, SaltSize+1), params: testKDF},
		{name: "invalid params", secret: []byte("secret"), salt: make([]byte, SaltSize), params: KDFParams{Time: 1, MemoryKiB: 1 << 30, Threads: 1}},
		{name: "nil secret", secret: nil, salt: make([]byte, SaltSize), params: testKDF},
		{name: "empty secret", secret: []byte{}, salt: make([]byte, SaltSize), params: testKDF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := DeriveKey(tt.secret, tt.salt, tt.params); !errors.Is(err, ErrInvalidParams) {
				t.Errorf("DeriveKey = %v, want ErrInvalidParams", err)
			}
		})
	}
}

func TestSubKeyRFC5869(t *testing.T) {
	// RFC 5869, Test Case 3: SHA-256 with an empty salt and empty info.
	ikm := bytes.Repeat([]byte{0x0b}, 22)

	want, err := hex.DecodeString("8da4e775a563c18f715f802a063c5a31b8a11f5c5ee1879ec3454e5f3c738d2d9d201395faa4b61a96c8")
	if err != nil {
		t.Fatal(err)
	}

	okm, err := subKeyLen(ikm, "", len(want))
	if err != nil {
		t.Fatalf("subKeyLen: %v", err)
	}

	if !bytes.Equal(okm, want) {
		t.Errorf("subKeyLen = %x, want %x", okm, want)
	}

	// SubKey only adds the master length check; the 22-byte IKM of the vector is below it.
	master := bytes.Repeat([]byte{0x0b}, KeySize)

	key, err := SubKey(master, "smartnote/v2/enc")
	if err != nil {
		t.Fatalf("SubKey: %v", err)
	}

	same, err := subKeyLen(master, "smartnote/v2/enc", KeySize)
	if err != nil {
		t.Fatalf("subKeyLen: %v", err)
	}

	if !bytes.Equal(key, same) {
		t.Errorf("SubKey = %x, want %x", key, same)
	}
}

func TestSubKeyRejectsShortMaster(t *testing.T) {
	tests := []struct {
		name   string
		master []byte
	}{
		{name: "nil", master: nil},
		{name: "31 bytes", master: make([]byte, KeySize-1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := SubKey(tt.master, "smartnote/v2/enc"); !errors.Is(err, ErrInvalidParams) {
				t.Errorf("SubKey = %v, want ErrInvalidParams", err)
			}
		})
	}
}

func TestSubKeyInfoSeparatesKeys(t *testing.T) {
	master := bytes.Repeat([]byte{7}, KeySize)

	a, err := SubKey(master, "smartnote/v2/enc")
	if err != nil {
		t.Fatalf("SubKey: %v", err)
	}

	b, err := SubKey(master, "smartnote/v2/other")
	if err != nil {
		t.Fatalf("SubKey: %v", err)
	}

	if bytes.Equal(a, b) {
		t.Error("different info gave the same key")
	}
}
