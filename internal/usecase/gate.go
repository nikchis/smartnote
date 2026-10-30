package usecase

import (
	"sync"

	"github.com/nikchis/smartnote/internal/domain"
)

// Gate lets one operation with the server run at a time in the whole app,
// Sync and Connection.Test alike (spec §6.6). The zero value is ready to use.
type Gate struct {
	mu sync.Mutex
}

// Enter takes the gate, or returns domain.ErrBusy when another operation holds
// it. leave releases it; calling leave again does nothing.
func (g *Gate) Enter() (leave func(), err error) {
	if !g.mu.TryLock() {
		return nil, domain.ErrBusy
	}

	return sync.OnceFunc(g.mu.Unlock), nil
}
