package wailsui

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

func TestNoteDTO(t *testing.T) {
	id := uuid.MustParse("0b9d7a3c-2f4e-4c55-9a51-3c1f0e6d8a10")
	updated := time.Date(2026, 10, 31, 12, 30, 15, 123_000_000, time.FixedZone("UTC+3", 3*3600))

	got := toNoteDTO(domain.Note{ID: id, Name: "name", Info: "text", Updated: updated})

	want := NoteDTO{ID: "0b9d7a3c-2f4e-4c55-9a51-3c1f0e6d8a10", Name: "name", Info: "text", Updated: updated.UnixMilli()}
	if got != want {
		t.Errorf("toNoteDTO = %+v, want %+v", got, want)
	}

	if want.Updated != time.Date(2026, 10, 31, 9, 30, 15, 123_000_000, time.UTC).UnixMilli() {
		t.Errorf("Updated = %d is not milliseconds of UTC", want.Updated)
	}

	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}

	if s := string(b); !strings.Contains(s, `"id":"0b9d7a3c-`) || !strings.Contains(s, `"updated":1793`) {
		t.Errorf("JSON = %s, want camelCase fields with the UUID string and milliseconds", s)
	}
}

func TestVersionDTO(t *testing.T) {
	id := uuid.New()
	created := time.UnixMilli(1_700_000_000_123)

	got := toVersionDTO(domain.NoteVersion{ID: id, NoteID: uuid.New(), Created: created, Info: "old"})

	want := VersionDTO{ID: id.String(), Created: 1_700_000_000_123, Info: "old"}
	if got != want {
		t.Errorf("toVersionDTO = %+v, want %+v", got, want)
	}
}

func TestSettingsDTORoundTrip(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	key, err := gossh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}

	s := domain.ConnectionSettings{
		SSHHost: "example.org", SSHPort: 22, SSHUser: "u", SSHPassword: "pw", DBHost: "127.0.0.1", DBPort: 5432,
		DBName: "db", DBUser: "dbu", DBPassword: "dbpw", SyncPhrase: "sixteen chars ok",
		TrustedHostKey:  strings.TrimSpace(string(gossh.MarshalAuthorizedKey(key))),
		TrustedHostAddr: "example.org:22",
	}

	dto := toSettingsDTO(s)
	if dto.TrustedFingerprint != gossh.FingerprintSHA256(key) {
		t.Errorf("TrustedFingerprint = %q, want %q", dto.TrustedFingerprint, gossh.FingerprintSHA256(key))
	}

	if got := dto.settings(); got != s {
		t.Errorf("round trip = %+v, want %+v", got, s)
	}
}

func TestSettingsDTOBrokenTrustedKey(t *testing.T) {
	dto := toSettingsDTO(domain.ConnectionSettings{TrustedHostKey: "not a key", TrustedHostAddr: "example.org:22"})

	if dto.TrustedFingerprint != "" {
		t.Errorf("TrustedFingerprint = %q, want empty for a key that does not parse", dto.TrustedFingerprint)
	}

	if dto.TrustedHostKey != "not a key" {
		t.Errorf("TrustedHostKey = %q, want it kept so that Save does not drop it silently", dto.TrustedHostKey)
	}
}

func TestCountsDTO(t *testing.T) {
	got := toBackupPreviewDTO(usecase.BackupPreview{Counts: usecase.Counts{Added: 1, Updated: 2, Deleted: 3}, RemoteNewer: true})

	want := BackupPreviewDTO{Counts: CountsDTO{Added: 1, Updated: 2, Deleted: 3}, RemoteNewer: true}
	if got != want {
		t.Errorf("toBackupPreviewDTO = %+v, want %+v", got, want)
	}
}
