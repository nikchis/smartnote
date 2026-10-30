package usecase

import (
	"errors"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
)

func TestGate(t *testing.T) {
	var g Gate

	leave, err := g.Enter()
	if err != nil {
		t.Fatalf("first Enter: %v", err)
	}

	if _, err := g.Enter(); !errors.Is(err, domain.ErrBusy) {
		t.Errorf("second Enter = %v, want ErrBusy", err)
	}

	leave()
	leave() // a second call must not unlock a later holder

	leave2, err := g.Enter()
	if err != nil {
		t.Fatalf("Enter after leave: %v", err)
	}

	if _, err := g.Enter(); !errors.Is(err, domain.ErrBusy) {
		t.Errorf("Enter while held again = %v, want ErrBusy", err)
	}

	leave2()
}
