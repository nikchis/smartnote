// Package remote picks the connector for the storage kind of the settings.
package remote

import (
	"context"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// Router is the usecase.RemoteConnector that sends each Open to the
// connector of the storage kind in the target's settings.
type Router struct {
	ssh, yandex usecase.RemoteConnector
}

// NewRouter returns a Router over the SSH + PostgreSQL and the Yandex Disk
// connectors.
func NewRouter(ssh, yandex usecase.RemoteConnector) *Router {
	return &Router{ssh: ssh, yandex: yandex}
}

// Open opens a session with the connector of t.Settings.Kind(). The kind
// none is domain.ErrNotConnected; a kind this version does not know comes from
// settings of a newer one and is domain.ErrUnsupportedFormat.
func (r *Router) Open(ctx context.Context, t usecase.RemoteTarget, mode usecase.OpenMode) (usecase.RemoteSession, error) {
	switch t.Settings.Kind() {
	case domain.KindSSHPostgres:
		return r.ssh.Open(ctx, t, mode)
	case domain.KindYandexDisk:
		return r.yandex.Open(ctx, t, mode)
	case domain.KindNone:
		return nil, domain.ErrNotConnected
	default:
		return nil, domain.ErrUnsupportedFormat
	}
}
