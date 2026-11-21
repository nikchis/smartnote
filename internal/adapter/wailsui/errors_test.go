package wailsui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/adapter/ssh"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/i18n"
	"github.com/nikchis/smartnote/internal/usecase"
)

// goldenHostKeyUnknown is shared with the TypeScript test of parseError.
var goldenHostKeyUnknown = filepath.Join("..", "..", "..", "frontend", "src", "lib", "testdata", "uierror-host-key-unknown.json")

func TestUIErrorGolden(t *testing.T) {
	got, err := json.Marshal(&UIError{
		Code: "host_key_unknown", Message: "m", Messages: map[i18n.Lang]string{i18n.EN: "m", i18n.RU: "м"},
		Fingerprint: "SHA256:x", Address: "example.org:22",
	})
	if err != nil {
		t.Fatal(err)
	}

	want, err := os.ReadFile(goldenHostKeyUnknown)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, bytes.TrimSpace(want)) {
		t.Errorf("JSON = %s, want %s", got, want)
	}
}

func TestUIErrorJSON(t *testing.T) {
	in := &UIError{
		Code: "host_key_mismatch", Message: `say "hi"`, Messages: map[i18n.Lang]string{i18n.EN: `say "hi"`, i18n.RU: "скажи"},
		Expected: "SHA256:a", Got: "SHA256:b",
	}

	var out UIError
	if err := json.Unmarshal([]byte(in.Error()), &out); err != nil {
		t.Fatalf("Error() is not JSON: %v: %s", err, in.Error())
	}

	if !reflect.DeepEqual(out, *in) {
		t.Errorf("round trip = %+v, want %+v", out, *in)
	}
}

func TestToUIError(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("operation: %w", err) }
	sshErr := &ssh.TransportError{Err: errors.New("ssh: handshake failed: ssh: unable to authenticate")}
	connErr := pgConnectError(t)
	pgErr := &pgconn.PgError{Severity: "ERROR", Code: "42501", Message: "permission denied for schema smartnote"}

	tests := []struct {
		name string
		err  error
		want UIError
	}{
		{name: "wrong password", err: wrap(domain.ErrWrongPassword),
			want: UIError{Code: "wrong_password", Message: "Access to local storage denied."}},
		{name: "corrupt storage", err: wrap(domain.ErrCorruptStorage),
			want: UIError{Code: "corrupt_storage", Message: "Local storage is damaged and cannot be opened."}},
		{name: "unsupported format", err: wrap(domain.ErrUnsupportedFormat),
			want: UIError{Code: "unsupported_format", Message: "Data was created by a newer version of Smartnote."}},
		{name: "wrong sync phrase", err: wrap(domain.ErrWrongSyncPhrase),
			want: UIError{Code: "wrong_sync_phrase", Message: "The sync phrase does not match the remote storage."}},
		{name: "host key unknown",
			err: wrap(&ssh.TransportError{Err: fmt.Errorf("ssh: handshake failed: %w",
				&domain.HostKeyUnknownError{Fingerprint: "SHA256:x", AuthorizedKey: "ssh-ed25519 AAAA", Addr: "example.org:22"})}),
			want: UIError{Code: "host_key_unknown", Message: "unknown server host key SHA256:x", Fingerprint: "SHA256:x", Address: "example.org:22"}},
		{name: "host key mismatch", err: wrap(&domain.HostKeyMismatchError{Expected: "SHA256:a", Got: "SHA256:b"}),
			want: UIError{Code: "host_key_mismatch", Message: "server host key mismatch: expected SHA256:a, got SHA256:b",
				Expected: "SHA256:a", Got: "SHA256:b"}},
		{name: "validation", err: wrap(&domain.ValidationError{Field: "ssh_port", Reason: "must be 1-65535"}),
			want: UIError{Code: "validation", Message: "invalid ssh_port: must be 1-65535", Field: "ssh_port"}},
		{name: "yandex account", err: wrap(&domain.ValidationError{Field: "yandex_account", Reason: "sign in required"}),
			want: UIError{Code: "validation", Message: "Sign in to Yandex Disk first, or choose None (local only).", Field: "yandex_account"}},
		{name: "not connected", err: wrap(domain.ErrNotConnected),
			want: UIError{Code: "not_connected", Message: "Remote storage is not configured."}},
		{name: "remote changed", err: wrap(domain.ErrRemoteChanged),
			want: UIError{Code: "remote_changed", Message: "Remote data changed during the operation. Please try again."}},
		{name: "local changed", err: wrap(domain.ErrLocalChanged),
			want: UIError{Code: "local_changed", Message: "Local data changed during the operation. Please try again."}},
		{name: "key missing", err: wrap(domain.ErrKeyMissing),
			want: UIError{Code: "key_missing", Message: "There is no SSH key. Generate or import a key first."}},
		{name: "key passphrase required", err: wrap(domain.ErrKeyPassphraseRequired),
			want: UIError{Code: "key_passphrase_required", Message: "The key is protected with a passphrase."}},
		{name: "busy", err: wrap(domain.ErrBusy),
			want: UIError{Code: "busy", Message: "Another operation with the remote storage is in progress."}},
		{name: "timeout over ssh", err: errors.Join(context.DeadlineExceeded, sshErr),
			want: UIError{Code: "timeout", Message: "The operation timed out."}},
		{name: "canceled", err: errors.Join(context.Canceled, wrap(sshErr)),
			want: UIError{Code: "canceled", Message: "The operation was canceled."}},
		{name: "ssh", err: wrap(sshErr),
			want: UIError{Code: "ssh", Message: "operation: ssh: handshake failed: ssh: unable to authenticate"}},
		{name: "postgres", err: wrap(pgErr),
			want: UIError{Code: "database", Message: "operation: ERROR: permission denied for schema smartnote (SQLSTATE 42501)"}},
		{name: "pgx connect", err: wrap(connErr),
			want: UIError{Code: "database", Message: "operation: " + connErr.Error()}},
		{name: "other", err: wrap(errors.New("disk full")),
			want: UIError{Code: "internal", Message: "operation: disk full"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ToUIError(tc.err)

			ui, ok := errors.AsType[*UIError](err)
			if !ok {
				t.Fatalf("ToUIError = %T, want *UIError", err)
			}

			if ui.Messages[i18n.EN] != ui.Message {
				t.Errorf("messages.en = %q, want the message %q", ui.Messages[i18n.EN], ui.Message)
			}

			got := *ui
			got.Messages = nil

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ToUIError = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestToUIErrorNil(t *testing.T) {
	if err := ToUIError(nil); err != nil {
		t.Errorf("ToUIError(nil) = %v, want nil", err)
	}
}

func TestToUIErrorKeepsUIError(t *testing.T) {
	in := &UIError{Code: "validation", Message: "m", Field: "id"}

	got, ok := errors.AsType[*UIError](ToUIError(fmt.Errorf("x: %w", in)))
	if !ok || got != in {
		t.Errorf("ToUIError = %v, want the same *UIError", got)
	}
}

func TestLogErrorRedactsRemoteErrors(t *testing.T) {
	var buf bytes.Buffer

	log := slog.New(slog.NewTextHandler(&buf, nil))
	secret := "alice@db.example.org"

	tests := []struct {
		name     string
		err      error
		logged   bool
		withText bool
	}{
		{name: "ssh", err: &ssh.TransportError{Err: errors.New("ssh: connect " + secret)}, logged: true},
		{name: "database", err: &pgconn.PgError{Code: "28P01", Message: "password authentication failed for " + secret}, logged: true},
		{name: "timeout", err: errors.Join(context.DeadlineExceeded, errors.New("dial "+secret)), logged: true},
		{name: "internal", err: errors.New("disk full"), logged: true, withText: true},
		{name: "validation", err: &domain.ValidationError{Field: "name", Reason: "empty"}},
		{name: "canceled", err: context.Canceled},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf.Reset()

			LogError(log, "op failed", tc.err)

			got := buf.String()
			if tc.logged != (got != "") {
				t.Fatalf("logged = %v, want %v: %q", got != "", tc.logged, got)
			}

			if strings.Contains(got, secret) {
				t.Errorf("log contains %q: %s", secret, got)
			}

			if tc.withText && !strings.Contains(got, tc.err.Error()) {
				t.Errorf("log lacks the error text: %s", got)
			}

			if tc.logged && !strings.Contains(got, "code=") {
				t.Errorf("log lacks the code: %s", got)
			}
		})
	}
}

// pgConnectError returns the error of a pgx connection to a closed local port.
func pgConnectError(t *testing.T) error {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	conn, err := pgx.Connect(t.Context(), "postgres://u@"+addr+"/db?sslmode=disable&connect_timeout=5")
	if err == nil {
		_ = conn.Close(t.Context())

		t.Fatal("connect to a closed port succeeded")
	}

	return err
}

func TestToUIErrorCloud(t *testing.T) {
	tooLarge := cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 413, Code: "other", Reason: cloudhttp.ReasonTooLarge}

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"no space", cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 507, Code: "other", Reason: cloudhttp.ReasonNoSpace}, "Not enough space on Yandex Disk."},
		{"too large", fmt.Errorf("upload: %w", tooLarge), "The file is larger than your Yandex Disk plan allows."},
		{"server error", cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 503, Code: "DiskInternalError"}, "Yandex Disk error: 503 DiskInternalError."},
		{"network", cloudhttp.CloudError{Provider: domain.KindYandexDisk, Code: "network"}, "Yandex Disk error: network."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ui := classify(tc.err)
			if ui.Code != "cloud" || ui.Message != tc.want {
				t.Errorf("classify = %q %q, want cloud %q", ui.Code, ui.Message, tc.want)
			}
		})
	}
}

func TestToUIErrorNewSentinels(t *testing.T) {
	tests := []struct {
		err        error
		code, text string
	}{
		{domain.ErrReauthRequired, "reauth_required", "Sign in to Yandex Disk again."},
		{domain.ErrRemoteDamaged, "remote_damaged", "Data in the remote storage is damaged."},
		{domain.ErrRemoteTooLarge, "remote_too_large", "Notes do not fit into the remote storage (64 MiB)."},
		{domain.ErrRemoteOutcomeUnknown, "remote_outcome_unknown", "The result of the backup is unknown. Connect again to check the remote storage."},
		{domain.ErrLoopbackUnavailable, "loopback_unavailable", "The local sign-in address is unavailable. Enter the code manually."},
	}

	for _, tc := range tests {
		ui := classify(fmt.Errorf("op: %w", tc.err))
		if ui.Code != tc.code || ui.Message != tc.text {
			t.Errorf("classify(%v) = %q %q, want %q %q", tc.err, ui.Code, ui.Message, tc.code, tc.text)
		}
	}

	if ui := classify(errors.Join(context.DeadlineExceeded, cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 503, Code: "x"})); ui.Code != codeTimeout {
		t.Errorf("deadline code = %q, want timeout", ui.Code)
	}
}

func TestLogErrorCloud(t *testing.T) {
	var buf bytes.Buffer

	log := slog.New(slog.NewTextHandler(&buf, nil))

	LogError(log, "sync", fmt.Errorf("https://cloud-api.yandex.net/v1?token=SECRET: %w", cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 503, Code: "DiskInternalError"}))

	got := buf.String()
	for _, want := range []string{"status=503", "code=cloud", "DiskInternalError"} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q lacks %q", got, want)
		}
	}

	if strings.Contains(got, "step=") || strings.Contains(got, "net=") {
		t.Errorf("log %q has a step or a network class the error lacks", got)
	}

	buf.Reset()
	LogError(log, "backup", fmt.Errorf("upload: %w", cloudhttp.CloudError{Provider: domain.KindYandexDisk, Code: "network", Step: "upload", Net: "reset"}))

	got = buf.String()
	for _, want := range []string{"code=cloud", "status=0", "cloud_code=network", "step=upload", "net=reset"} {
		if !strings.Contains(got, want) {
			t.Errorf("log %q lacks %q", got, want)
		}
	}

	if strings.Contains(got, "err=") {
		t.Errorf("log %q has the error text", got)
	}

	for _, bad := range []string{"SECRET", "https://", "description"} {
		if strings.Contains(got, bad) {
			t.Errorf("log %q contains %q", got, bad)
		}
	}

	for _, e := range []error{domain.ErrReauthRequired, domain.ErrRemoteOutcomeUnknown, domain.ErrRemoteDamaged} {
		buf.Reset()
		LogError(log, "sync", fmt.Errorf("secret-text: %w", e))

		got := buf.String()
		ui := classify(e)

		if !strings.Contains(got, "code="+ui.Code) || strings.Contains(got, "secret-text") || strings.Contains(got, e.Error()) || strings.Contains(got, "err=") {
			t.Errorf("log for %v = %q, want only the code", e, got)
		}
	}
}

func TestTransportTimeoutIsCloudError(t *testing.T) {
	err := fmt.Errorf("upload: %w", cloudhttp.CloudError{Provider: domain.KindYandexDisk, Code: "network", Step: "upload", Net: "timeout"})

	if ui := classify(err); ui.Code != codeCloud || ui.Message != "Yandex Disk error: network." {
		t.Errorf("classify = %q %q, want cloud \"Yandex Disk error: network.\"", ui.Code, ui.Message)
	}

	var buf bytes.Buffer

	LogError(slog.New(slog.NewTextHandler(&buf, nil)), "backup", err)

	for _, want := range []string{"code=cloud", "step=upload", "net=timeout"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log %q lacks %q", buf.String(), want)
		}
	}
}

func TestReauthMessageFromErrorKind(t *testing.T) {
	err := fmt.Errorf("sync: %w", domain.ReauthRequired(domain.KindYandexDisk))

	ui := classify(err)
	if ui.Code != codeReauthRequired || ui.Message != "Sign in to Yandex Disk again." {
		t.Errorf("classify = %q %q, want reauth_required \"Sign in to Yandex Disk again.\"", ui.Code, ui.Message)
	}

	google := classify(domain.ReauthRequired(domain.KindGoogleDrive))
	if google.Code != codeReauthRequired || google.Message != "Sign in to Google Drive again." {
		t.Errorf("google = %q %q", google.Code, google.Message)
	}

	other := classify(domain.ReauthRequired(domain.StorageKind("dropbox")))
	if other.Code != codeReauthRequired || other.Message != "Sign in to dropbox again." {
		t.Errorf("other kind = %q %q", other.Code, other.Message)
	}
}

func TestCloudMessageByReasonAndProvider(t *testing.T) {
	ce := cloudhttp.NewAnswerError(domain.KindYandexDisk, 507, "x", cloudhttp.ReasonNoSpace)
	if got := cloudTexts(ce)[i18n.EN]; got != "Not enough space on Yandex Disk." {
		t.Errorf("no space = %q", got)
	}

	// The reason, not the status, selects the text.
	ce = cloudhttp.NewAnswerError(domain.KindYandexDisk, 507, "x", cloudhttp.ReasonNone)
	if got := cloudTexts(ce)[i18n.EN]; got != "Yandex Disk error: 507 x." {
		t.Errorf("no reason = %q", got)
	}
}

// wantBothLanguages checks that ui has a text in each language, that they
// differ, and that the message is the English one.
func wantBothLanguages(t *testing.T, name string, ui *UIError) {
	t.Helper()

	en, ru := ui.Messages[i18n.EN], ui.Messages[i18n.RU]
	if en == "" || ru == "" || en == ru {
		t.Errorf("%s: messages = %q, want two different texts", name, ui.Messages)
	}

	if ui.Message != en {
		t.Errorf("%s: message = %q, want messages.en %q", name, ui.Message, en)
	}

	if len(ui.Messages) != 2 {
		t.Errorf("%s: messages = %q, want en and ru only", name, ui.Messages)
	}
}

func TestUIErrorHasBothLanguages(t *testing.T) {
	for _, s := range sentinels {
		ui := classify(fmt.Errorf("op: %w", s.err))
		if ui.Code != s.code {
			t.Errorf("%v: code = %q, want %q", s.err, ui.Code, s.code)
		}

		wantBothLanguages(t, s.code, ui)
	}

	others := map[string]error{
		"timeout":       context.DeadlineExceeded,
		"canceled":      context.Canceled,
		"host key":      &domain.HostKeyUnknownError{Fingerprint: "SHA256:x", Addr: "example.org:22"},
		"mismatch":      &domain.HostKeyMismatchError{Expected: "SHA256:a", Got: "SHA256:b"},
		"reauth kind":   domain.ReauthRequired(domain.KindYandexDisk),
		"reauth other":  domain.ReauthRequired(domain.KindGoogleDrive),
		"no space":      cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 507, Code: "x", Reason: cloudhttp.ReasonNoSpace},
		"too large":     cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 413, Code: "x", Reason: cloudhttp.ReasonTooLarge},
		"status":        cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 503, Code: "DiskInternalError"},
		"network":       cloudhttp.CloudError{Provider: domain.KindYandexDisk, Code: "network"},
		"yandex sign":   &domain.ValidationError{Field: "yandex_account", Reason: "sign in required"},
		"google sign":   &domain.ValidationError{Field: "google_account", Reason: "sign in required"},
		"search wraps":  fmt.Errorf("search: %w", usecase.ErrSearchUnavailable),
		"reauth sentin": domain.ErrReauthRequired,
	}

	for name, err := range others {
		wantBothLanguages(t, name, classify(err))
	}
}

func TestRussianTextsOfCloudAndReauth(t *testing.T) {
	tests := []struct {
		err    error
		en, ru string
	}{
		{cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 507, Code: "x", Reason: cloudhttp.ReasonNoSpace},
			"Not enough space on Yandex Disk.", "Недостаточно места на Yandex Disk."},
		{cloudhttp.CloudError{Provider: domain.KindYandexDisk, Status: 503, Code: "DiskInternalError"},
			"Yandex Disk error: 503 DiskInternalError.", "Ошибка Yandex Disk: 503 DiskInternalError."},
		{cloudhttp.CloudError{Provider: domain.KindYandexDisk, Code: "network"},
			"Yandex Disk error: network.", "Ошибка Yandex Disk: network."},
		{domain.ReauthRequired(domain.KindYandexDisk), "Sign in to Yandex Disk again.", "Войдите в Yandex Disk ещё раз."},
		{domain.ErrReauthRequired, "Sign in to Yandex Disk again.", "Войдите в Yandex Disk ещё раз."},
		{&domain.ValidationError{Field: "google_account", Reason: "sign in required"},
			"Sign in to Google Drive first, or choose None (local only).",
			"Сначала войдите в Google Drive или выберите «Нет (только локально)»."},
	}

	for _, tc := range tests {
		ui := classify(tc.err)
		if ui.Messages[i18n.EN] != tc.en || ui.Messages[i18n.RU] != tc.ru {
			t.Errorf("classify(%v) = %q, want en %q, ru %q", tc.err, ui.Messages, tc.en, tc.ru)
		}
	}
}

// domainValidationErrors are the validation errors the app forms, each from
// the code that forms it where that is reachable from here.
func domainValidationErrors(t *testing.T) map[string]error {
	t.Helper()

	long := strings.Repeat("a", domain.MaxNameLen+1)
	ssh := domain.ConnectionSettings{StorageKind: domain.KindSSHPostgres, SSHHost: "h", SSHPort: 22, SSHUser: "u",
		DBHost: "127.0.0.1", DBPort: 5432, DBName: "n", DBUser: "u", SyncPhrase: strings.Repeat("p", domain.MinSyncPhraseLen)}
	with := func(f func(s *domain.ConnectionSettings)) error {
		s := ssh
		f(&s)

		return s.Validate()
	}

	errs := map[string]error{
		"name required":       domain.Note{Info: "x"}.Validate(),
		"name too long":       domain.Note{Name: long, Info: "x"}.Validate(),
		"name control":        domain.Note{Name: "a\x01", Info: "x"}.Validate(),
		"info control":        domain.Note{Name: "a", Info: "x\x01"}.Validate(),
		"password short":      domain.ValidatePassword("abc"),
		"password long":       domain.ValidatePassword(strings.Repeat("a", domain.MaxPasswordLen+1)),
		"kind unknown":        domain.ConnectionSettings{StorageKind: "ftp"}.Validate(),
		"yandex client":       domain.ValidateYandexClientID("bad id!"),
		"google client":       domain.ValidateGoogleClient("bad", "s"),
		"google secret":       domain.ValidateGoogleClient("1-a.apps.googleusercontent.com", "\x01"),
		"sync phrase":         with(func(s *domain.ConnectionSettings) { s.SyncPhrase = "short" }),
		"db host":             with(func(s *domain.ConnectionSettings) { s.DBHost = "example.org" }),
		"ssh host":            with(func(s *domain.ConnectionSettings) { s.SSHHost = "" }),
		"ssh user":            with(func(s *domain.ConnectionSettings) { s.SSHUser = "" }),
		"ssh port":            with(func(s *domain.ConnectionSettings) { s.SSHPort = 70000 }),
		"db port":             with(func(s *domain.ConnectionSettings) { s.DBPort = -1 }),
		"db name":             with(func(s *domain.ConnectionSettings) { s.DBName = "" }),
		"db user":             with(func(s *domain.ConnectionSettings) { s.DBUser = "" }),
		"note name taken":     &domain.ValidationError{Field: "name", Reason: "a note with this name already exists"},
		"confirm":             &domain.ValidationError{Field: "confirm", Reason: "does not match the password"},
		"id":                  &domain.ValidationError{Field: "id", Reason: "is not a UUID"},
		"key file large":      &domain.ValidationError{Field: "key_file", Reason: "is too large for a private key"},
		"key file dsa":        &domain.ValidationError{Field: "key_file", Reason: "is a DSA key, which is no longer secure; use Ed25519 or RSA of at least 2048 bits"},
		"key file rsa":        &domain.ValidationError{Field: "key_file", Reason: "is an RSA key of 1024 bits; at least 2048 are needed"},
		"code":                &domain.ValidationError{Field: "code", Reason: "required"},
		"not a cloud":         &domain.ValidationError{Field: "kind", Reason: "is not a cloud storage"},
		"own client yandex":   &domain.ValidationError{Field: "yandex_client_id", Reason: "set your own client ID in Advanced"},
		"own client google":   &domain.ValidationError{Field: "google_client_id", Reason: "set your own client ID in Advanced"},
		"host key not offer":  &domain.ValidationError{Field: "trusted_host_key", Reason: "was not offered by the server"},
		"host key elsewhere":  &domain.ValidationError{Field: "trusted_host_key", Reason: "was offered by another server"},
		"language":            &domain.ValidationError{Field: "language", Reason: "must be en or ru"},
		"wailsui unknown key": errUnknownTrust,
	}

	for name, err := range errs {
		if _, ok := errors.AsType[*domain.ValidationError](err); !ok {
			t.Fatalf("%s: %v is not a validation error", name, err)
		}
	}

	return errs
}

func TestValidationMessageBothLanguages(t *testing.T) {
	for name, err := range domainValidationErrors(t) {
		ui := classify(err)
		ve, _ := errors.AsType[*domain.ValidationError](err)

		if ui.Code != codeValidation || ui.Field != ve.Field {
			t.Errorf("%s: classify = %q field %q", name, ui.Code, ui.Field)
		}

		wantBothLanguages(t, name, ui)

		if ui.Message != ve.Error() {
			t.Errorf("%s: message = %q, want the text of 2.2.2 %q", name, ui.Message, ve.Error())
		}

		if strings.Contains(ui.Messages[i18n.RU], ve.Field) || strings.Contains(ui.Messages[i18n.RU], "invalid") {
			t.Errorf("%s: Russian text %q has the field name or English", name, ui.Messages[i18n.RU])
		}
	}

	want := map[error]string{
		&domain.ValidationError{Field: "ssh_port", Reason: "must be between 1 and 65535"}:                          "Поле «Порт SSH» должно содержать число от 1 до 65535.",
		&domain.ValidationError{Field: "sync_phrase", Reason: "must be at least 16 characters"}:                    "Поле «Фраза синхронизации» должно содержать не меньше 16 символов.",
		&domain.ValidationError{Field: "key_file", Reason: "is an RSA key of 1024 bits; at least 2048 are needed"}: "Ключ RSA слишком короткий (1024 бит); нужно не меньше 2048 бит.",
	}

	for err, ru := range want {
		if got := classify(err).Messages[i18n.RU]; got != ru {
			t.Errorf("classify(%v) ru = %q, want %q", err, got, ru)
		}
	}

	// An unknown reason or field keeps the English text in both languages.
	for _, ve := range []*domain.ValidationError{
		{Field: "ssh_port", Reason: "is odd"},
		{Field: "colour", Reason: "is required"},
		{Field: "sync_phrase", Reason: "must be at least x characters"},
	} {
		ui := classify(ve)
		if ui.Message != ve.Error() || ui.Messages[i18n.EN] != ve.Error() || ui.Messages[i18n.RU] != ve.Error() {
			t.Errorf("classify(%v) = %q %q, want the English text in both", ve, ui.Message, ui.Messages)
		}
	}
}

func TestTechnicalErrorSameInBoth(t *testing.T) {
	errs := []error{
		fmt.Errorf("op: %w", &ssh.TransportError{Err: errors.New("ssh: handshake failed")}),
		fmt.Errorf("op: %w", &pgconn.PgError{Severity: "ERROR", Code: "42501", Message: "permission denied"}),
		fmt.Errorf("op: %w", pgConnectError(t)),
		errors.New("disk full"),
	}

	for _, err := range errs {
		ui := classify(err)
		if ui.Message != err.Error() || ui.Messages[i18n.EN] != err.Error() || ui.Messages[i18n.RU] != err.Error() {
			t.Errorf("classify(%v) = %q %q, want the same technical text in both", err, ui.Message, ui.Messages)
		}
	}
}
