package usecase

import (
	"context"

	"github.com/nikchis/smartnote/internal/domain"
)

// SettingsStore keeps the connection settings in the local storage.
type SettingsStore interface {
	LoadSettings(ctx context.Context) (domain.ConnectionSettings, error)
	SaveSettings(ctx context.Context, s domain.ConnectionSettings) error
}
