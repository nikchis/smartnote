package sqlite

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nikchis/smartnote/internal/domain"
)

func TestSettingsAndKeyRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)

	if got, err := s.LoadSettings(ctx); err != nil || !got.IsEmpty() || got.TrustedHostKey != "" {
		t.Fatalf("LoadSettings on a new storage = %v, %v; want empty", got, err)
	}
	if _, ok, err := s.LoadKey(ctx); err != nil || ok {
		t.Fatalf("LoadKey on a new storage = %v, %v; want none", ok, err)
	}

	settings := domain.ConnectionSettings{
		SSHHost: "example.org", SSHPort: 2222, SSHUser: "u", SSHPassword: "p", DBHost: "::1", DBPort: 5432,
		DBName: "db", DBUser: "dbu", DBPassword: "q", SyncPhrase: "sixteen chars ok", TrustedHostKey: "ssh-ed25519 AAAA",
	}
	key := domain.KeyPair{PrivatePEM: []byte("-----BEGIN OPENSSH PRIVATE KEY-----\n..."), PublicAuthorized: "ssh-ed25519 AAAA me"}
	if err := s.SaveSettings(ctx, settings); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if err := s.SaveKey(ctx, key); err != nil {
		t.Fatalf("SaveKey: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2 := openTest(t, dir)
	if err := s2.Unlock(ctx, "secret1"); err != nil {
		t.Fatal(err)
	}
	if got, err := s2.LoadSettings(ctx); err != nil || got != settings {
		t.Errorf("LoadSettings = %v, %v; want the saved settings", got, err)
	}
	got, ok, err := s2.LoadKey(ctx)
	if err != nil || !ok || !bytes.Equal(got.PrivatePEM, key.PrivatePEM) || got.PublicAuthorized != key.PublicAuthorized {
		t.Errorf("LoadKey = %v, %v, %v", got, ok, err)
	}

	empty := domain.ConnectionSettings{}
	if err := s2.SaveSettings(ctx, empty); err != nil {
		t.Fatal(err)
	}
	if got, err := s2.LoadSettings(ctx); err != nil || got != empty {
		t.Errorf("LoadSettings after clearing = %v, %v", got, err)
	}
}

func TestSettingsTampered(t *testing.T) {
	ctx := context.Background()
	s, _ := createTest(t)
	if err := s.SaveSettings(ctx, domain.ConnectionSettings{SSHHost: "h"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE connection SET settings = (SELECT value FROM meta WHERE key = 'wrapped_dek')"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadSettings(ctx); !errors.Is(err, domain.ErrCorruptStorage) {
		t.Errorf("LoadSettings = %v, want ErrCorruptStorage", err)
	}
}

func TestNoPlaintextOnDisk(t *testing.T) {
	ctx := context.Background()
	s, dir := createTest(t)
	n := domain.Note{ID: testUUID(1), Name: "PLAINNAME-xyz", Info: "PLAININFO-xyz", Updated: ms(1)}
	if err := s.InsertNote(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateNoteWithVersion(ctx, domain.Note{ID: n.ID, Name: n.Name, Info: "PLAININFO2-xyz", Updated: ms(2)},
		domain.NoteVersion{ID: testUUID(2), NoteID: n.ID, Created: ms(1), Info: n.Info}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSettings(ctx, domain.ConnectionSettings{SSHHost: "h", DBPassword: "PLAINPWD-xyz", SyncPhrase: "PLAINPHRASE-xyz-123"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveKey(ctx, domain.KeyPair{PrivatePEM: []byte("PLAINPRIVATE-xyz"), PublicAuthorized: "ssh-ed25519 pub"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{dbFile, dbFile + "-journal"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"PLAINNAME-xyz", "PLAININFO-xyz", "PLAININFO2-xyz", "PLAINPWD-xyz", "PLAINPHRASE-xyz-123", "PLAINPRIVATE-xyz", "secret1"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Errorf("%s contains %q", name, secret)
			}
		}
	}
}
