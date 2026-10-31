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

// SettingsDTO are the connection settings. TrustedHostKey and TrustedHostAddr
// travel back and forth so that a key trusted in the settings dialog is saved
// only with OK; TrustedFingerprint is shown and ignored on the way back.
type SettingsDTO struct {
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
	TrustedHostKey     string `json:"trustedHostKey"`
	TrustedHostAddr    string `json:"trustedHostAddr"`
	TrustedFingerprint string `json:"trustedFingerprint"`
}

// CountsDTO are the notes a Backup or Restore adds, updates and deletes.
type CountsDTO struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Deleted int `json:"deleted"`
}

// ConnectDTO is the result of the connect check: Status is one of offline,
// created, in_sync, diverged, remote_newer; Restore counts the Restore that
// remote_newer asks about.
type ConnectDTO struct {
	Status  string    `json:"status"`
	Restore CountsDTO `json:"restore"`
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

func toNoteDTO(n domain.Note) NoteDTO {
	return NoteDTO{ID: n.ID.String(), Name: n.Name, Info: n.Info, Updated: n.Updated.UnixMilli()}
}

func toVersionDTO(v domain.NoteVersion) VersionDTO {
	return VersionDTO{ID: v.ID.String(), Created: v.Created.UnixMilli(), Info: v.Info}
}

func toCountsDTO(c usecase.Counts) CountsDTO {
	return CountsDTO{Added: c.Added, Updated: c.Updated, Deleted: c.Deleted}
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
		SSHHost: s.SSHHost, SSHPort: s.SSHPort, SSHUser: s.SSHUser, SSHPassword: s.SSHPassword,
		DBHost: s.DBHost, DBPort: s.DBPort, DBName: s.DBName, DBUser: s.DBUser, DBPassword: s.DBPassword,
		SyncPhrase: s.SyncPhrase, TrustedHostKey: s.TrustedHostKey, TrustedHostAddr: s.TrustedHostAddr,
		TrustedFingerprint: fingerprint(s.TrustedHostKey),
	}
}

func (d SettingsDTO) settings() domain.ConnectionSettings {
	return domain.ConnectionSettings{
		SSHHost: d.SSHHost, SSHPort: d.SSHPort, SSHUser: d.SSHUser, SSHPassword: d.SSHPassword,
		DBHost: d.DBHost, DBPort: d.DBPort, DBName: d.DBName, DBUser: d.DBUser, DBPassword: d.DBPassword,
		SyncPhrase: d.SyncPhrase, TrustedHostKey: d.TrustedHostKey, TrustedHostAddr: d.TrustedHostAddr,
	}
}

// fingerprint returns the SHA256 fingerprint of a key in authorized_keys
// format, or "" when there is no key or it does not parse.
func fingerprint(authorized string) string {
	if authorized == "" {
		return ""
	}

	key, _, _, _, err := gossh.ParseAuthorizedKey([]byte(authorized)) //nolint:dogsled // only the key matters
	if err != nil {
		return "" // the connection reports the broken key when it is used
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
