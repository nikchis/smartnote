package gdrive

import (
	"log/slog"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// FactoryOption changes the stores a factory builds.
type FactoryOption func(*storeOptions)

// WithLog makes the stores log the duplicates, the failures of their
// cleanup and the resolutions of unfinished uploads to log.
func WithLog(log *slog.Logger) FactoryOption {
	return func(o *storeOptions) { o.log = log }
}

// WithProgress makes the stores report the bytes of their uploads and
// downloads to f.
func WithProgress(f usecase.ProgressFunc) FactoryOption {
	return func(o *storeOptions) { o.progress = f }
}

// NewStoreFactory builds a Store per operation over the target's token
// (spec §3.3). uploads keeps the record of the unfinished upload, auth
// refreshes the token with the effective client of the target's settings:
// the own client, or else builtin.
func NewStoreFactory(e Endpoints, uploads UploadStore, auth Refresher, builtin usecase.CloudClient, opts ...FactoryOption) blobremote.StoreFactory {
	var o storeOptions
	for _, opt := range opts {
		opt(&o)
	}

	return func(t usecase.RemoteTarget) (blobremote.ObjectStore, error) {
		if t.Token == nil {
			return nil, domain.ReauthRequired(domain.KindGoogleDrive)
		}

		if uploads == nil {
			return nil, errNoUploadStore
		}

		// Without a client the store resolves an unfinished upload, which
		// needs no token, and then asks to sign in at the first refresh.
		client, _ := usecase.EffectiveClient(t.Settings, map[domain.StorageKind]usecase.CloudClient{domain.KindGoogleDrive: builtin})
		client.Provider = domain.KindGoogleDrive

		return newStore(e, t.Token, client, auth, uploads, o), nil
	}
}
