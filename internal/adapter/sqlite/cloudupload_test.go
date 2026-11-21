package sqlite

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// TestCloudUploadSealed: the record of an unfinished upload is sealed with
// the DEK (spec §7.2): the table holds no plain bytes, a load gives them
// back, a clear removes them and a record sealed for another provider does
// not open.
func TestCloudUploadSealed(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)
	record := []byte(`{"session_uri":"https://www.googleapis.com/upload/SESSION-SECRET","op":"create","size":5}`)

	if _, found, err := s.LoadCloudUpload(ctx, domain.KindGoogleDrive); err != nil || found {
		t.Fatalf("LoadCloudUpload of an empty table = %v, %v; want nothing", found, err)
	}

	if err := s.SaveCloudUpload(ctx, domain.KindGoogleDrive, record); err != nil {
		t.Fatal(err)
	}

	raw := rawRow(t, dir, "SELECT data FROM cloud_upload WHERE provider = ?", "google_drive")
	if raw == nil || bytes.Equal(raw, record) || bytes.Contains(raw, []byte("SESSION-SECRET")) {
		t.Errorf("cloud_upload holds %q, want the sealed record", raw)
	}

	got, found, err := s.LoadCloudUpload(ctx, domain.KindGoogleDrive)
	if err != nil || !found || !bytes.Equal(got, record) {
		t.Errorf("LoadCloudUpload = %q, %v, %v; want %q", got, found, err, record)
	}

	next := []byte(`{"op":"replace"}`)
	if err := s.SaveCloudUpload(ctx, domain.KindGoogleDrive, next); err != nil {
		t.Fatal(err)
	}

	if got, _, _ := s.LoadCloudUpload(ctx, domain.KindGoogleDrive); !bytes.Equal(got, next) {
		t.Errorf("LoadCloudUpload after a second save = %q, want %q", got, next)
	}

	wrong, err := sealbox.Seal(s.dek, record, sealbox.AAD{Table: "cloud_upload", Column: "data", RowID: "yandex_disk"})
	if err != nil {
		t.Fatal(err)
	}

	rawExec(t, dir, "UPDATE cloud_upload SET data = ? WHERE provider = 'google_drive'", wrong)

	if _, _, err := s.LoadCloudUpload(ctx, domain.KindGoogleDrive); !errors.Is(err, domain.ErrCorruptStorage) {
		t.Errorf("LoadCloudUpload with a foreign AAD = %v, want ErrCorruptStorage", err)
	}

	if err := s.ClearCloudUpload(ctx, domain.KindGoogleDrive); err != nil {
		t.Fatal(err)
	}

	if _, found, err := s.LoadCloudUpload(ctx, domain.KindGoogleDrive); err != nil || found {
		t.Errorf("LoadCloudUpload after Clear = %v, %v; want nothing", found, err)
	}

	if err := s.ClearCloudUpload(ctx, domain.KindGoogleDrive); err != nil {
		t.Errorf("ClearCloudUpload without a record: %v", err)
	}
}

func TestCloudUploadNotCloudProvider(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)

	if err := s.SaveCloudUpload(ctx, domain.KindSSHPostgres, []byte("{}")); err == nil {
		t.Error("SaveCloudUpload of ssh_postgres: nil error")
	}

	if _, _, err := s.LoadCloudUpload(ctx, domain.KindSSHPostgres); err == nil {
		t.Error("LoadCloudUpload of ssh_postgres: nil error")
	}
}
