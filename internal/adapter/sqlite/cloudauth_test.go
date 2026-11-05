package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
	"github.com/nikchis/smartnote/internal/usecase"
)

var (
	_ usecase.CloudTokenStore = (*Store)(nil)
	_ usecase.ConnectionStore = (*Store)(nil)
)

func testToken() usecase.CloudToken {
	return usecase.CloudToken{
		Provider: "yandex", ClientID: "cid", Access: "ACCESS-SECRET-1", Refresh: "REFRESH-SECRET-2",
		Account: "user", Expiry: time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC),
	}
}

func hasTable(t *testing.T, dir, name string) bool {
	t.Helper()

	db, err := sql.Open("sqlite", dsn(filepath.Join(dir, dbFile)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&n); err != nil {
		t.Fatal(err)
	}

	return n == 1
}

func formatOf(t *testing.T, dir string) string {
	t.Helper()

	db, err := sql.Open("sqlite", dsn(filepath.Join(dir, dbFile)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var v []byte
	if err := db.QueryRowContext(context.Background(), "SELECT value FROM meta WHERE key = 'format_version'").Scan(&v); err != nil {
		t.Fatal(err)
	}

	return string(v)
}

func TestCloudAuthCreatedNewStore(t *testing.T) {
	_, dir := createTest(t)

	for _, tbl := range []string{"cloud_auth", "cloud_pending"} {
		if !hasTable(t, dir, tbl) {
			t.Errorf("table %s is missing in a new storage", tbl)
		}
	}

	if v := formatOf(t, dir); v != "2" {
		t.Errorf("format_version = %q, want 2", v)
	}
}

func TestCloudAuthCreatedOnUnlockV2(t *testing.T) {
	s, dir := createTest(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	rawExec(t, dir, "DROP TABLE cloud_auth")
	rawExec(t, dir, "DROP TABLE cloud_pending")

	s2 := openTest(t, dir)
	if err := s2.Unlock(context.Background(), "secret1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	for _, tbl := range []string{"cloud_auth", "cloud_pending"} {
		if !hasTable(t, dir, tbl) {
			t.Errorf("table %s was not created on unlock", tbl)
		}
	}

	if v := formatOf(t, dir); v != "2" {
		t.Errorf("format_version = %q, want 2", v)
	}
}

func TestCloudAuthCreatedOnUnlockV1(t *testing.T) {
	s, dir := createTest(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	downgradeToV1(t, dir)
	rawExec(t, dir, "DROP TABLE cloud_auth")
	rawExec(t, dir, "DROP TABLE cloud_pending")

	s2 := openTest(t, dir)
	if err := s2.Unlock(context.Background(), "secret1"); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	if !hasTable(t, dir, "cloud_auth") || !hasTable(t, dir, "cloud_pending") {
		t.Error("cloud tables were not created when unlocking a format 1 storage")
	}
}

func TestCloudAuthNotRequired(t *testing.T) {
	tables := map[string]bool{}
	for _, n := range requiredTablesV2 {
		tables[n] = true
	}

	if err := checkTables(tables, 2); err != nil {
		t.Errorf("checkTables without cloud tables: %v", err)
	}
}

func TestCloudTokenRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)

	if _, ok, err := s.LoadCloudToken(ctx); err != nil || ok {
		t.Fatalf("LoadCloudToken on empty = %v, %v", ok, err)
	}

	want := testToken()
	if err := s.SaveCloudToken(ctx, want); err != nil {
		t.Fatal(err)
	}

	next := want
	next.Access = "ACCESS-NEW"

	if err := s.SaveCloudToken(ctx, next); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openTest(t, dir)
	if err := s2.Unlock(ctx, "secret1"); err != nil {
		t.Fatal(err)
	}

	got, ok, err := s2.LoadCloudToken(ctx)
	if err != nil || !ok || !got.Expiry.Equal(next.Expiry) || got.Access != "ACCESS-NEW" || got.Refresh != want.Refresh ||
		got.Account != want.Account || got.ClientID != want.ClientID || got.Provider != want.Provider {
		t.Errorf("LoadCloudToken = %+v, %v, %v", got, ok, err)
	}
}

func TestCloudTokenSealed(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)

	if err := s.SaveCloudToken(ctx, testToken()); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", dsn(filepath.Join(dir, dbFile)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var data []byte
	if err := db.QueryRowContext(context.Background(), "SELECT data FROM cloud_auth WHERE cloud_auth_id = 1").Scan(&data); err != nil {
		t.Fatal(err)
	}

	for _, secret := range []string{"ACCESS-SECRET-1", "REFRESH-SECRET-2"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Errorf("data contains %q in plain", secret)
		}
	}

	// The same plaintext sealed under another AAD must not open.
	wrong, err := sealbox.Seal(s.dek, []byte(`{"Access":"x"}`), sealbox.AAD{Table: "cloud_auth", Column: "data", RowID: "2"})
	if err != nil {
		t.Fatal(err)
	}

	rawExec(t, dir, "UPDATE cloud_auth SET data = ? WHERE cloud_auth_id = 1", wrong)

	if _, _, err := s.LoadCloudToken(ctx); !errors.Is(err, domain.ErrCorruptStorage) {
		t.Errorf("LoadCloudToken with a foreign AAD = %v, want ErrCorruptStorage", err)
	}
}

func TestSaveConnectionTokenChanges(t *testing.T) {
	ctx := context.Background()
	old := testToken()
	fresh := usecase.CloudToken{Provider: "yandex", ClientID: "cid2", Access: "A2", Refresh: "R2", Account: "other"}

	tests := []struct {
		name   string
		change usecase.TokenChange
		want   usecase.CloudToken
		wantOK bool
	}{
		{"keep", usecase.TokenKeep, old, true},
		{"set", usecase.TokenSet, fresh, true},
		{"delete", usecase.TokenDelete, usecase.CloudToken{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := createTest(t)

			if err := s.SaveCloudToken(ctx, old); err != nil {
				t.Fatal(err)
			}

			settings := domain.ConnectionSettings{StorageKind: domain.KindYandexDisk, YandexClientID: "cid2"}
			if err := s.SaveConnection(ctx, settings, tt.change, fresh); err != nil {
				t.Fatalf("SaveConnection: %v", err)
			}

			if got, err := s.LoadSettings(ctx); err != nil || got != settings {
				t.Errorf("LoadSettings = %v, %v", got, err)
			}

			got, ok, err := s.LoadCloudToken(ctx)
			if err != nil || ok != tt.wantOK || !got.Expiry.Equal(tt.want.Expiry) || got.Access != tt.want.Access || got.ClientID != tt.want.ClientID {
				t.Errorf("LoadCloudToken = %+v, %v, %v; want %+v, %v", got, ok, err, tt.want, tt.wantOK)
			}
		})
	}
}

func TestSaveConnectionAtomic(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)

	before := domain.ConnectionSettings{SSHHost: "before.example"}
	if err := s.SaveSettings(ctx, before); err != nil {
		t.Fatal(err)
	}

	rawExec(t, dir, "DROP TABLE cloud_auth")

	after := domain.ConnectionSettings{StorageKind: domain.KindYandexDisk, YandexClientID: "cid"}
	if err := s.SaveConnection(ctx, after, usecase.TokenSet, testToken()); err == nil {
		t.Fatal("SaveConnection succeeded without the token table")
	}

	if got, err := s.LoadSettings(ctx); err != nil || got != before {
		t.Errorf("settings after a failed SaveConnection = %v, %v; want unchanged", got, err)
	}
}

func TestLoadSettingsWithKind(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)

	want := domain.ConnectionSettings{StorageKind: domain.KindYandexDisk, YandexClientID: "543e36190e6549c8883212efb9582bc5", SyncPhrase: "sixteen chars ok"}
	if err := s.SaveSettings(ctx, want); err != nil {
		t.Fatal(err)
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openTest(t, dir)
	if err := s2.Unlock(ctx, "secret1"); err != nil {
		t.Fatal(err)
	}

	if got, err := s2.LoadSettings(ctx); err != nil || got != want {
		t.Errorf("LoadSettings = %v, %v; want %v", got, err, want)
	}
}

func TestPendingTempRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)

	if name, ok, err := s.LoadPendingTemp(ctx); err != nil || ok || name != "" {
		t.Fatalf("LoadPendingTemp on empty = %q, %v, %v", name, ok, err)
	}

	for _, n := range []string{"smartnote.v1.bin.tmp-1", "smartnote.v1.bin.tmp-2"} {
		if err := s.SavePendingTemp(ctx, n); err != nil {
			t.Fatal(err)
		}

		if name, ok, err := s.LoadPendingTemp(ctx); err != nil || !ok || name != n {
			t.Fatalf("LoadPendingTemp = %q, %v, %v; want %q", name, ok, err, n)
		}
	}

	if err := s.ClearPendingTemp(ctx); err != nil {
		t.Fatal(err)
	}

	if err := s.ClearPendingTemp(ctx); err != nil {
		t.Errorf("second ClearPendingTemp: %v", err)
	}

	if _, ok, err := s.LoadPendingTemp(ctx); err != nil || ok {
		t.Errorf("LoadPendingTemp after clear = %v, %v", ok, err)
	}

	// TokenDelete clears the pending name, TokenKeep does not.
	if err := s.SavePendingTemp(ctx, "tmp-3"); err != nil {
		t.Fatal(err)
	}

	if err := s.SaveConnection(ctx, domain.ConnectionSettings{}, usecase.TokenKeep, usecase.CloudToken{}); err != nil {
		t.Fatal(err)
	}

	if _, ok, _ := s.LoadPendingTemp(ctx); !ok {
		t.Error("TokenKeep cleared the pending name")
	}

	if err := s.SaveConnection(ctx, domain.ConnectionSettings{}, usecase.TokenDelete, usecase.CloudToken{}); err != nil {
		t.Fatal(err)
	}

	if _, ok, _ := s.LoadPendingTemp(ctx); ok {
		t.Error("TokenDelete left the pending name")
	}

	// A format 2 storage without the table gets it on unlock.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	rawExec(t, dir, "DROP TABLE cloud_pending")

	s2 := openTest(t, dir)
	if err := s2.Unlock(ctx, "secret1"); err != nil {
		t.Fatal(err)
	}

	if err := s2.SavePendingTemp(ctx, "tmp-4"); err != nil {
		t.Errorf("SavePendingTemp after re-creation: %v", err)
	}
}
