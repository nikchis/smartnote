// Package usecase implements the scenarios of the app on top of ports that the
// adapters implement. It imports only domain.
package usecase

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Clock returns the current time; tests pass a fixed one.
type Clock func() time.Time

// IDGenerator returns a new note or version ID; tests pass a sequence.
type IDGenerator func() uuid.UUID

// errNilDependency is returned by constructors given a nil dependency.
var errNilDependency = errors.New("nil dependency")

// nilDependency reports the dependency name that constructor ctor got as nil.
func nilDependency(ctor, name string) error {
	return fmt.Errorf("usecase: %s: %w: %s", ctor, errNilDependency, name)
}
