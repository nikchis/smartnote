package gdrive

import (
	"errors"
	"testing"

	"github.com/nikchis/smartnote/internal/adapter/blobremote"
	"github.com/nikchis/smartnote/internal/adapter/blobremote/storetest"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// withEmulator makes the stores of a factory reach the emulator d.
func withEmulator(d *fakeDrive) FactoryOption {
	return func(o *storeOptions) {
		t := testOptions(d)
		o.http, o.now, o.allowHost = t.http, t.now, t.allowHost
	}
}

// testFactory is the public factory over the emulator for a device whose
// upload record is up.
func testFactory(t *testing.T, d *fakeDrive, up UploadStore) blobremote.StoreFactory {
	t.Helper()

	return NewStoreFactory(d.endpoints(), up, testRefresher(t, d), fakeClient, withEmulator(d))
}

func googleTarget(tokens usecase.CloudTokenSource) usecase.RemoteTarget {
	return usecase.RemoteTarget{Settings: domain.ConnectionSettings{StorageKind: domain.KindGoogleDrive}, Token: tokens}
}

func TestStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) blobremote.ObjectStore {
		d := newFakeDrive(t)

		s, err := testFactory(t, d, &memUploads{})(googleTarget(testTokens()))
		if err != nil {
			t.Fatal(err)
		}

		return s
	})
}

func TestFactoryWithoutTokenIsReauth(t *testing.T) {
	d := newFakeDrive(t)

	_, err := testFactory(t, d, &memUploads{})(googleTarget(nil))
	if re, ok := errors.AsType[*domain.ReauthRequiredError](err); !ok || re.Kind != domain.KindGoogleDrive {
		t.Errorf("factory: err = %v, want ReauthRequiredError{google_drive}", err)
	}
}

// TestFactoryRefreshesWithEffectiveClient: the refresh goes with the own
// client of the target's settings, else with the built-in one.
func TestFactoryRefreshesWithEffectiveClient(t *testing.T) {
	tests := []struct {
		name     string
		settings domain.ConnectionSettings
		want     string
	}{
		{name: "built-in", settings: domain.ConnectionSettings{StorageKind: domain.KindGoogleDrive}, want: fakeClientID},
		{name: "own", settings: domain.ConnectionSettings{
			StorageKind: domain.KindGoogleDrive, GoogleClientID: "own-client", GoogleClientSecret: "own-secret",
		}, want: "own-client"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newFakeDrive(t)
			d.put(blobremote.ContainerName, []byte("x"), testNow)

			s, err := testFactory(t, d, &memUploads{})(usecase.RemoteTarget{Settings: tc.settings, Token: signedIn("stale", 3600e9)})
			if err != nil {
				t.Fatal(err)
			}

			_, _, _, _ = s.Read(t.Context(), blobremote.ContainerName) // only the refresh matters

			forms := d.forms()
			if len(forms) == 0 || forms[0].Get("client_id") != tc.want {
				t.Errorf("refreshes = %d, want the first with client_id %q", len(forms), tc.want)
			}
		})
	}
}

// TestFactoryWithoutUploadStoreIsLocal: a store without the upload record
// store is a local error, as settle reports it.
func TestFactoryWithoutUploadStoreIsLocal(t *testing.T) {
	d := newFakeDrive(t)

	_, err := NewStoreFactory(d.endpoints(), nil, testRefresher(t, d), fakeClient, withEmulator(d))(googleTarget(testTokens()))
	if _, ok := errors.AsType[usecase.LocalError](err); !ok {
		t.Errorf("factory: err = %v, want usecase.LocalError", err)
	}
}
