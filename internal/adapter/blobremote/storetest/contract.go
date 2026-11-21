// Package storetest is the contract every blobremote.ObjectStore keeps:
// the tests of each store adapter run it over the adapter's emulator.
package storetest

import (
	"bytes"
	"errors"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
)

const name = blobremote.ContainerName

// Run checks the contract of blobremote.ObjectStore (spec §3.2) with the
// stores newStore returns; each case gets a new store over an empty
// storage.
func Run(t *testing.T, newStore func(t *testing.T) blobremote.ObjectStore) {
	t.Helper()

	t.Run("missing object is not found", func(t *testing.T) {
		data, version, found := read(t, newStore(t))
		if found || data != nil || version != "" {
			t.Errorf("Read = %q, %q, %v; want nothing", data, version, found)
		}
	})

	t.Run("create then read gives the bytes and the version", func(t *testing.T) {
		s := newStore(t)
		write(t, s, []byte("first"), "")

		data, version, found := read(t, s)
		if !found || string(data) != "first" || version == "" {
			t.Fatalf("Read = %q, %q, %v; want the created object with a version", data, version, found)
		}

		_, again, _ := read(t, s)
		if again != version {
			t.Errorf("second Read version = %q, want %q", again, version)
		}
	})

	t.Run("replace with the current version", func(t *testing.T) {
		s := newStore(t)
		write(t, s, []byte("first"), "")
		_, v1, _ := read(t, s)

		write(t, s, []byte("second"), v1)

		data, v2, found := read(t, s)
		if !found || string(data) != "second" || v2 == "" || v2 == v1 {
			t.Errorf("Read = %q, %q, %v; want the replacement with a new version (old %q)", data, v2, found, v1)
		}
	})

	t.Run("replace with a wrong version is a mismatch", func(t *testing.T) {
		s := newStore(t)
		write(t, s, []byte("first"), "")
		_, v1, _ := read(t, s)

		err := s.Write(t.Context(), name, []byte("second"), v1+"-stale")
		if !errors.Is(err, blobremote.ErrVersionMismatch) {
			t.Fatalf("Write: err = %v, want ErrVersionMismatch", err)
		}

		wantObject(t, s, []byte("first"), v1)
	})

	t.Run("create over an existing object is a mismatch", func(t *testing.T) {
		s := newStore(t)
		write(t, s, []byte("first"), "")
		_, v1, _ := read(t, s)

		err := s.Write(t.Context(), name, []byte("second"), "")
		if !errors.Is(err, blobremote.ErrVersionMismatch) {
			t.Fatalf("Write: err = %v, want ErrVersionMismatch", err)
		}

		wantObject(t, s, []byte("first"), v1)
	})
}

func read(t *testing.T, s blobremote.ObjectStore) (data []byte, version string, found bool) {
	t.Helper()

	data, version, found, err := s.Read(t.Context(), name)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	return data, version, found
}

func write(t *testing.T, s blobremote.ObjectStore, data []byte, expect string) {
	t.Helper()

	if err := s.Write(t.Context(), name, data, expect); err != nil {
		t.Fatalf("Write(expect %q): %v", expect, err)
	}
}

// wantObject checks that the object still has data and version.
func wantObject(t *testing.T, s blobremote.ObjectStore, data []byte, version string) {
	t.Helper()

	got, v, found := read(t, s)
	if !found || !bytes.Equal(got, data) || v != version {
		t.Errorf("Read = %q, %q, %v; want %q, %q", got, v, found, data, version)
	}
}
