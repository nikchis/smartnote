package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
	"github.com/nikchis/smartnote/internal/usecase"
)

func googleToken() usecase.CloudToken {
	return usecase.CloudToken{
		Provider: domain.KindGoogleDrive, ClientID: "gcid", Access: "G-ACCESS-SECRET", Refresh: "G-REFRESH-SECRET",
		Expiry: time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC),
	}
}

func setUpdate(t usecase.CloudToken) usecase.TokenUpdate {
	return usecase.TokenUpdate{Provider: t.Provider, Change: usecase.TokenSet, Token: t}
}

func deleteUpdate(p domain.StorageKind) usecase.TokenUpdate {
	return usecase.TokenUpdate{Provider: p, Change: usecase.TokenDelete}
}

// rawRow returns the single blob query selects, or nil if there is no row.
func rawRow(t *testing.T, dir, query string, args ...any) []byte {
	t.Helper()

	db, err := sql.Open("sqlite", dsn(filepath.Join(dir, dbFile)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var data []byte

	err = db.QueryRowContext(context.Background(), query, args...).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}

	if err != nil {
		t.Fatal(err)
	}

	return data
}

// sameToken compares the fields of two tokens; the redacted String hides them.
func sameToken(a, b usecase.CloudToken) bool {
	return a.Provider == b.Provider && a.ClientID == b.ClientID && a.Access == b.Access &&
		a.Refresh == b.Refresh && a.Account == b.Account && a.Expiry.Equal(b.Expiry)
}

// assertToken fails unless the stored token of p is want, or is missing when
// want is the zero token.
func assertToken(t *testing.T, s *Store, p domain.StorageKind, want usecase.CloudToken) {
	t.Helper()

	got, ok, err := s.LoadCloudToken(context.Background(), p)
	if err != nil {
		t.Fatalf("LoadCloudToken(%s): %v", p, err)
	}

	if wantOK := want != (usecase.CloudToken{}); ok != wantOK || (ok && !sameToken(got, want)) {
		t.Errorf("LoadCloudToken(%s) = %s %s, %v; want %s %s, %v", p, got.ClientID, got.Access, ok, want.ClientID, want.Access, wantOK)
	}
}

func TestCloudTokenPerProvider(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)
	yandex, google := testToken(), googleToken()

	if err := s.SaveCloudToken(ctx, yandex); err != nil {
		t.Fatal(err)
	}

	yandexRow := rawRow(t, dir, "SELECT data FROM cloud_auth WHERE cloud_auth_id = 1")

	if err := s.SaveCloudToken(ctx, google); err != nil {
		t.Fatal(err)
	}

	if got := rawRow(t, dir, "SELECT data FROM cloud_auth WHERE cloud_auth_id = 1"); !bytes.Equal(got, yandexRow) {
		t.Error("saving the Google token changed cloud_auth")
	}

	assertToken(t, s, domain.KindYandexDisk, yandex)
	assertToken(t, s, domain.KindGoogleDrive, google)

	settings := domain.ConnectionSettings{StorageKind: domain.KindYandexDisk}

	if err := s.SaveConnection(ctx, settings, []usecase.TokenUpdate{deleteUpdate(domain.KindGoogleDrive)}); err != nil {
		t.Fatal(err)
	}

	assertToken(t, s, domain.KindYandexDisk, yandex)
	assertToken(t, s, domain.KindGoogleDrive, usecase.CloudToken{})

	if err := s.SaveConnection(ctx, settings, []usecase.TokenUpdate{setUpdate(google), deleteUpdate(domain.KindYandexDisk)}); err != nil {
		t.Fatal(err)
	}

	assertToken(t, s, domain.KindYandexDisk, usecase.CloudToken{})
	assertToken(t, s, domain.KindGoogleDrive, google)
}

func TestCloudTokenGoogleSealed(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)

	if err := s.SaveCloudToken(ctx, googleToken()); err != nil {
		t.Fatal(err)
	}

	data := rawRow(t, dir, "SELECT data FROM cloud_token WHERE provider = ?", "google_drive")
	for _, secret := range []string{"G-ACCESS-SECRET", "G-REFRESH-SECRET"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("data contains %q in plain", secret)
		}
	}

	// A token sealed for another provider must not open as Google's.
	wrong, err := sealbox.Seal(s.dek, []byte(`{"Access":"x"}`), sealbox.AAD{Table: "cloud_token", Column: "data", RowID: "yandex_disk"})
	if err != nil {
		t.Fatal(err)
	}

	rawExec(t, dir, "UPDATE cloud_token SET data = ? WHERE provider = 'google_drive'", wrong)

	if _, _, err := s.LoadCloudToken(ctx, domain.KindGoogleDrive); !errors.Is(err, domain.ErrCorruptStorage) {
		t.Errorf("LoadCloudToken with a foreign AAD = %v, want ErrCorruptStorage", err)
	}
}

func TestCloudTokenUnknownProvider(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)

	if _, _, err := s.LoadCloudToken(ctx, domain.KindSSHPostgres); err == nil {
		t.Error("LoadCloudToken of ssh_postgres: nil error")
	}

	if err := s.SaveCloudToken(ctx, usecase.CloudToken{Provider: ""}); err == nil {
		t.Error("SaveCloudToken without a provider: nil error")
	}
}

func TestSaveConnectionRollsBackAllUpdates(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name   string
		damage func(t *testing.T, dir string)
		second usecase.TokenUpdate
	}{
		{name: "table of the second is missing", damage: func(t *testing.T, dir string) { rawExec(t, dir, "DROP TABLE cloud_token") },
			second: setUpdate(googleToken())},
		{name: "invalid change", second: usecase.TokenUpdate{Provider: domain.KindGoogleDrive}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, dir := createTest(t)
			before := domain.ConnectionSettings{SSHHost: "before.example"}

			if err := s.SaveSettings(ctx, before); err != nil {
				t.Fatal(err)
			}

			if tc.damage != nil {
				tc.damage(t, dir)
			}

			after := domain.ConnectionSettings{StorageKind: domain.KindGoogleDrive}
			updates := []usecase.TokenUpdate{setUpdate(testToken()), tc.second}

			if err := s.SaveConnection(ctx, after, updates); err == nil {
				t.Fatal("SaveConnection succeeded with a failing second update")
			}

			if got, err := s.LoadSettings(ctx); err != nil || got != before {
				t.Errorf("settings after a failed SaveConnection = %v, %v; want unchanged", got, err)
			}

			assertToken(t, s, domain.KindYandexDisk, usecase.CloudToken{})
		})
	}
}

func TestNewTablesKeepFormat2(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)

	tables, err := tableNames(ctx, s.db)
	if err != nil {
		t.Fatal(err)
	}

	newTables := []string{"cloud_token", "cloud_upload"}

	for _, name := range newTables {
		if !tables[name] {
			t.Errorf("%s is missing in a new storage", name)
		}

		// 2.2.2 opens the storage: the table is not required by its check.
		if slices.Contains(requiredTablesV2, name) {
			t.Errorf("%s is a required table", name)
		}
	}

	if err := checkTables(tables, 2); err != nil {
		t.Errorf("checkTables: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	for _, name := range newTables {
		rawExec(t, dir, "DROP TABLE "+name)
	}

	s2 := openTest(t, dir)
	if err := s2.Unlock(ctx, "secret1"); err != nil {
		t.Fatalf("Unlock without the new tables: %v", err)
	}

	for _, name := range newTables {
		if !hasTable(t, dir, name) {
			t.Errorf("%s was not created on unlock", name)
		}
	}

	if v := formatOf(t, dir); v != "2" {
		t.Errorf("format_version = %q, want 2", v)
	}
}
