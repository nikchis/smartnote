package blobremote

import (
	"context"
	"errors"

	"github.com/nikchis/smartnote/internal/usecase"
)

// ContainerName is the name of the container object in the store.
const ContainerName = "smartnote.v1.bin"

// ErrVersionMismatch and ErrOutcomeUnknown are the errors of ObjectStore.Write.
var (
	ErrVersionMismatch = errors.New("remote object changed")
	ErrOutcomeUnknown  = errors.New("remote replace outcome unknown")
)

// ObjectStore is an object storage with a version check (spec §3.2).
type ObjectStore interface {
	// Read returns the content and the version of the object; found is false
	// when there is no such object. The version belongs to the returned bytes.
	Read(ctx context.Context, name string) (data []byte, version string, found bool, err error)
	// Write replaces the object name with data if its current version equals
	// expect ("" means there is no object); otherwise ErrVersionMismatch. The
	// replacement is atomic for readers. ErrOutcomeUnknown means the
	// replacement was accepted but its result is not known.
	Write(ctx context.Context, name string, data []byte, expect string) error
}

// StoreFactory builds the ObjectStore for one operation.
type StoreFactory func(t usecase.RemoteTarget) (ObjectStore, error)
