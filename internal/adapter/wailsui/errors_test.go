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
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nikchis/smartnote/internal/adapter/ssh"
	"github.com/nikchis/smartnote/internal/adapter/yadisk"
	"github.com/nikchis/smartnote/internal/domain"
)

// goldenHostKeyUnknown is shared with the TypeScript test of parseError.
var goldenHostKeyUnknown = filepath.Join("..", "..", "..", "frontend", "src", "lib", "testdata", "uierror-host-key-unknown.json")

func TestUIErrorGolden(t *testing.T) {
	got, err := json.Marshal(&UIError{Code: "host_key_unknown", Message: "m", Fingerprint: "SHA256:x", Address: "example.org:22"})
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
	in := &UIError{Code: "host_key_mismatch", Message: `say "hi"`, Expected: "SHA256:a", Got: "SHA256:b"}

	var out UIError
	if err := json.Unmarshal([]byte(in.Error()), &out); err != nil {
		t.Fatalf("Error() is not JSON: %v: %s", err, in.Error())
	}

	if out != *in {
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
			want: UIError{Code: "corrupt_storage", Message: msgCorruptStorage}},
		{name: "unsupported format", err: wrap(domain.ErrUnsupportedFormat),
			want: UIError{Code: "unsupported_format", Message: "Data was created by a newer version of Smartnote."}},
		{name: "wrong sync phrase", err: wrap(domain.ErrWrongSyncPhrase),
			want: UIError{Code: "wrong_sync_phrase", Message: msgWrongSyncPhrase}},
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
			want: UIError{Code: "not_connected", Message: msgNotConnected}},
		{name: "remote changed", err: wrap(domain.ErrRemoteChanged),
			want: UIError{Code: "remote_changed", Message: "Remote data changed during the operation. Please try again."}},
		{name: "local changed", err: wrap(domain.ErrLocalChanged),
			want: UIError{Code: "local_changed", Message: "Local data changed during the operation. Please try again."}},
		{name: "key missing", err: wrap(domain.ErrKeyMissing),
			want: UIError{Code: "key_missing", Message: "There is no SSH key. Generate or import a key first."}},
		{name: "key passphrase required", err: wrap(domain.ErrKeyPassphraseRequired),
			want: UIError{Code: "key_passphrase_required", Message: msgKeyPassphraseRequired}},
		{name: "busy", err: wrap(domain.ErrBusy),
			want: UIError{Code: "busy", Message: msgBusy}},
		{name: "timeout over ssh", err: errors.Join(context.DeadlineExceeded, sshErr),
			want: UIError{Code: "timeout", Message: msgTimeout}},
		{name: "canceled", err: errors.Join(context.Canceled, wrap(sshErr)),
			want: UIError{Code: "canceled", Message: msgCanceled}},
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

			if *ui != tc.want {
				t.Errorf("ToUIError = %+v, want %+v", *ui, tc.want)
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
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"no space", yadisk.CloudError{Status: 507, Code: "other"}, "Not enough space on Yandex Disk."},
		{"too large", fmt.Errorf("upload: %w", yadisk.CloudError{Status: 413, Code: "other"}), "The file is larger than your Yandex Disk plan allows."},
		{"server error", yadisk.CloudError{Status: 503, Code: "DiskInternalError"}, "Yandex Disk error: 503 DiskInternalError."},
		{"network", yadisk.CloudError{Code: "network"}, "Yandex Disk error: network."},
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
		{domain.ErrLoopbackUnavailable, "loopback_unavailable", msgLoopbackUnavailable},
	}

	for _, tc := range tests {
		ui := classify(fmt.Errorf("op: %w", tc.err))
		if ui.Code != tc.code || ui.Message != tc.text {
			t.Errorf("classify(%v) = %q %q, want %q %q", tc.err, ui.Code, ui.Message, tc.code, tc.text)
		}
	}

	if ui := classify(errors.Join(context.DeadlineExceeded, yadisk.CloudError{Status: 503, Code: "x"})); ui.Code != codeTimeout {
		t.Errorf("deadline code = %q, want timeout", ui.Code)
	}
}

func TestLogErrorCloud(t *testing.T) {
	var buf bytes.Buffer

	log := slog.New(slog.NewTextHandler(&buf, nil))

	LogError(log, "sync", fmt.Errorf("https://cloud-api.yandex.net/v1?token=SECRET: %w", yadisk.CloudError{Status: 503, Code: "DiskInternalError"}))

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
	LogError(log, "backup", fmt.Errorf("upload: %w", yadisk.CloudError{Code: "network", Step: "upload", Net: "reset"}))

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
