// Package wailsui exposes the Smartnote use cases to the Svelte interface as
// Wails services: it converts domain values to DTOs, errors to UIError codes
// and drives the quit sequence.
package wailsui

import (
	"github.com/google/uuid"
	gossh "golang.org/x/crypto/ssh"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// NoteDTO is a note for the interface; Updated is in milliseconds since the
// Unix epoch (UTC).
type NoteDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Info    string `json:"info"`
	Updated int64  `json:"updated"`
}

// VersionDTO is a previous text of a note; Created is in milliseconds UTC.
type VersionDTO struct {
	ID      string `json:"id"`
	Created int64  `json:"created"`
	Info    string `json:"info"`
}

// SettingsDTO are the connection settings. The server key never crosses to
// the interface (spec §6.1): TrustedFingerprint names it both ways. On the way
// back it must name the saved key or the key trusted with TrustOfferedKey;
// empty resets the trust. Tokens never cross: YandexAccount and
// YandexSignedIn only report the sign-in and are ignored on the way back.
type SettingsDTO struct {
	Kind               string `json:"kind"`
	YandexClientID     string `json:"yandexClientId"`
	YandexAccount      string `json:"yandexAccount"`
	YandexSignedIn     bool   `json:"yandexSignedIn"`
	SSHHost            string `json:"sshHost"`
	SSHPort            int    `json:"sshPort"`
	SSHUser            string `json:"sshUser"`
	SSHPassword        string `json:"sshPassword"`
	DBHost             string `json:"dbHost"`
	DBPort             int    `json:"dbPort"`
	DBName             string `json:"dbName"`
	DBUser             string `json:"dbUser"`
	DBPassword         string `json:"dbPassword"`
	SyncPhrase         string `json:"syncPhrase"`
	TrustedFingerprint string `json:"trustedFingerprint"`
}

// CountsDTO are the notes a Backup or Restore adds, updates and deletes.
type CountsDTO struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
}

// ConnectDTO is the result of the connect check: Status is one of offline,
// created, in_sync, diverged, remote_newer, remote_older; Restore counts the Restore that
// remote_newer asks about.
type ConnectDTO struct {
	Status  string    `json:"status"`
	Restore CountsDTO `json:"restore"`
}

// RestorePreviewDTO is what the user is asked about before a Restore.
type RestorePreviewDTO struct {
	Counts      CountsDTO `json:"counts"`
	RemoteOlder bool      `json:"remoteOlder"`
}

// BackupPreviewDTO is what the user is asked about before a Backup.
type BackupPreviewDTO struct {
	Counts      CountsDTO `json:"counts"`
	RemoteNewer bool      `json:"remoteNewer"`
}

// StateDTO drives the Backup and Restore buttons.
type StateDTO struct {
	Connected        bool `json:"connected"`
	Dirty            bool `json:"dirty"`
	RestoreAvailable bool `json:"restoreAvailable"`
	Busy             bool `json:"busy"`
}

// SearchHitDTO is one place a search found: Line 0 is the name of the note,
// 1…n the lines of its text; Start and End are UTF-16 offsets within the name
// or the whole text, and Text is the text between them. Marks are UTF-16
// ranges within Snippet; Sources name the searches that found it. Marks and
// Sources are never null.
type SearchHitDTO struct {
	NoteID  string   `json:"noteId"`
	Line    int      `json:"line"`
	Start   int      `json:"start"`
	End     int      `json:"end"`
	Text    string   `json:"text"`
	Snippet string   `json:"snippet"`
	Marks   [][2]int `json:"marks"`
	Sources []string `json:"sources"`
}

// SearchStatusDTO is the state of the search index: Notes indexed, Queued
// waiting, and the state of each search, one of loading, ready, unavailable.
type SearchStatusDTO struct {
	Notes    int    `json:"notes"`
	Queued   int    `json:"queued"`
	Lexical  string `json:"lexical"`
	Semantic string `json:"semantic"`
}

func toNoteDTO(n domain.Note) NoteDTO {
	return NoteDTO{ID: n.ID.String(), Name: n.Name, Info: n.Info, Updated: n.Updated.UnixMilli()}
}

func toVersionDTO(v domain.NoteVersion) VersionDTO {
	return VersionDTO{ID: v.ID.String(), Created: v.Created.UnixMilli(), Info: v.Info}
}

func toSearchHitDTO(h usecase.SearchHit) SearchHitDTO {
	marks := h.Marks
	if marks == nil {
		marks = [][2]int{}
	}

	sources := h.Sources
	if sources == nil {
		sources = []string{}
	}

	return SearchHitDTO{
		NoteID: h.NoteID.String(), Line: h.Line, Start: h.Start, End: h.End,
		Text: h.Text, Snippet: h.Snippet, Marks: marks, Sources: sources,
	}
}

func toSearchStatusDTO(s usecase.SearchStatus) SearchStatusDTO {
	return SearchStatusDTO{Notes: s.Notes, Queued: s.Queued, Lexical: string(s.Lexical), Semantic: string(s.Semantic)}
}

func toCountsDTO(c usecase.Counts) CountsDTO {
	return CountsDTO{Added: c.Added, Updated: c.Updated, Deleted: c.Deleted}
}

func toRestorePreviewDTO(p usecase.RestorePreview) RestorePreviewDTO {
	return RestorePreviewDTO{Counts: toCountsDTO(p.Counts), RemoteOlder: p.RemoteOlder}
}

func toBackupPreviewDTO(p usecase.BackupPreview) BackupPreviewDTO {
	return BackupPreviewDTO{Counts: toCountsDTO(p.Counts), RemoteNewer: p.RemoteNewer}
}

// ToStateDTO converts the synchronization state for the sync:state event.
func ToStateDTO(s usecase.State) StateDTO {
	return StateDTO{Connected: s.Connected, Dirty: s.Dirty, RestoreAvailable: s.RestoreAvailable, Busy: s.Busy}
}

func toSettingsDTO(s domain.ConnectionSettings) SettingsDTO {
	return SettingsDTO{
		Kind: string(s.Kind()), YandexClientID: s.YandexClientID,
		SSHHost: s.SSHHost, SSHPort: s.SSHPort, SSHUser: s.SSHUser, SSHPassword: s.SSHPassword,
		DBHost: s.DBHost, DBPort: s.DBPort, DBName: s.DBName, DBUser: s.DBUser, DBPassword: s.DBPassword,
		SyncPhrase: s.SyncPhrase, TrustedFingerprint: trustRef(s.TrustedHostKey),
	}
}

// settings returns the settings without the trusted key, which
// ConnectionService finds by TrustedFingerprint.
func (d SettingsDTO) settings() domain.ConnectionSettings {
	return domain.ConnectionSettings{
		StorageKind: domain.StorageKind(d.Kind), YandexClientID: d.YandexClientID,
		SSHHost: d.SSHHost, SSHPort: d.SSHPort, SSHUser: d.SSHUser, SSHPassword: d.SSHPassword,
		DBHost: d.DBHost, DBPort: d.DBPort, DBName: d.DBName, DBUser: d.DBUser, DBPassword: d.DBPassword,
		SyncPhrase: d.SyncPhrase,
	}
}

// unreadableKey names a saved trusted key that does not parse, so that Save
// keeps it rather than dropping it silently; the connection reports it.
const unreadableKey = "unreadable key"

// trustRef returns what the interface shows for a trusted key in
// authorized_keys format: "" for none, its SHA256 fingerprint, or
// unreadableKey.
func trustRef(authorized string) string {
	if authorized == "" {
		return ""
	}

	key, _, _, _, err := gossh.ParseAuthorizedKey([]byte(authorized)) //nolint:dogsled // only the key matters
	if err != nil {
		return unreadableKey
	}

	return gossh.FingerprintSHA256(key)
}

// parseID converts a note ID from the interface.
func parseID(id string) (uuid.UUID, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, &domain.ValidationError{Field: "id", Reason: "is not a UUID"}
	}

	return u, nil
}
