package wailsui

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nikchis/smartnote/internal/adapter/cloudhttp"
	"github.com/nikchis/smartnote/internal/adapter/ssh"
	"github.com/nikchis/smartnote/internal/applog"
	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/i18n"
	"github.com/nikchis/smartnote/internal/usecase"
)

// UIError is an error as the interface sees it: a code from spec §3.5, the
// text to show in each language of the interface, and the details some codes
// carry. Message is the English text, as in 2.2.2; Messages holds the text
// in every language, so the interface can redraw it when the language
// changes (spec §16.4). The technical text of SSH, PostgreSQL and internal
// errors is the same in every language. The server key itself never goes to
// the interface, only its fingerprint and address.
type UIError struct {
	Code        string               `json:"code"`
	Message     string               `json:"message"`
	Messages    map[i18n.Lang]string `json:"messages"`
	Field       string               `json:"field,omitempty"`
	Fingerprint string               `json:"fingerprint,omitempty"`
	Expected    string               `json:"expected,omitempty"`
	Got         string               `json:"got,omitempty"`
	Address     string               `json:"address,omitempty"` // host:port that offered the key
}

// Error returns e as a JSON object. Wails rejects the call's promise with an
// Error whose message is this text, so the interface parses the code from it
// whatever else Wails adds (docs/development.md, check 1).
func (e *UIError) Error() string {
	b, err := json.Marshal(e)
	if err != nil { // a struct of strings always marshals
		return `{"code":"internal","message":"unencodable error"}`
	}

	return string(b)
}

// Error codes (spec §3.5).
const (
	codeWrongPassword         = "wrong_password"
	codeCorruptStorage        = "corrupt_storage"
	codeUnsupportedFormat     = "unsupported_format"
	codeWrongSyncPhrase       = "wrong_sync_phrase"
	codeHostKeyUnknown        = "host_key_unknown"
	codeHostKeyMismatch       = "host_key_mismatch"
	codeValidation            = "validation"
	codeNotConnected          = "not_connected"
	codeRemoteChanged         = "remote_changed"
	codeLocalChanged          = "local_changed"
	codeKeyMissing            = "key_missing"
	codeKeyPassphraseRequired = "key_passphrase_required"
	codeBusy                  = "busy"
	codeTimeout               = "timeout"
	codeCanceled              = "canceled"
	codeSSH                   = "ssh"
	codeDatabase              = "database"
	codeInternal              = "internal"
	codeReauthRequired        = "reauth_required"
	codeCloud                 = "cloud"
	codeRemoteDamaged         = "remote_damaged"
	codeRemoteTooLarge        = "remote_too_large"
	codeRemoteOutcomeUnknown  = "remote_outcome_unknown"
	codeLoopbackUnavailable   = "loopback_unavailable"
	// codeSearchUnavailable is a search the engine could not run (search
	// spec §9.1, §11); the interface shows its message in the status line.
	codeSearchUnavailable = "search_unavailable"
)

// argProvider is the placeholder of the name of a cloud in the texts.
const argProvider = "provider"

// The fields of the validation errors of a cloud storage without a sign-in;
// the interface marks the account block.
var accountFields = map[string]domain.StorageKind{
	"yandex_account": domain.KindYandexDisk,
	"google_account": domain.KindGoogleDrive,
}

// sentinels maps domain and use case errors to their code and the catalog
// key of their text, in the order they are checked.
var sentinels = []struct {
	err  error
	code string
	key  i18n.Key
	args []any
}{
	{err: domain.ErrWrongPassword, code: codeWrongPassword, key: i18n.ErrWrongPassword},
	{err: domain.ErrCorruptStorage, code: codeCorruptStorage, key: i18n.ErrCorruptStorage},
	{err: domain.ErrUnsupportedFormat, code: codeUnsupportedFormat, key: i18n.ErrUnsupportedFormat},
	{err: domain.ErrWrongSyncPhrase, code: codeWrongSyncPhrase, key: i18n.ErrWrongSyncPhrase},
	{err: domain.ErrNotConnected, code: codeNotConnected, key: i18n.ErrNotConnected},
	{err: domain.ErrRemoteChanged, code: codeRemoteChanged, key: i18n.ErrRemoteChanged},
	{err: domain.ErrLocalChanged, code: codeLocalChanged, key: i18n.ErrLocalChanged},
	{err: domain.ErrKeyMissing, code: codeKeyMissing, key: i18n.ErrKeyMissing},
	{err: domain.ErrKeyPassphraseRequired, code: codeKeyPassphraseRequired, key: i18n.ErrKeyPassphrase},
	{err: domain.ErrBusy, code: codeBusy, key: i18n.ErrBusy},
	// Without the kind in the error, the cloud of 2.2.2.
	{err: domain.ErrReauthRequired, code: codeReauthRequired, key: i18n.ErrReauthRequired, args: []any{argProvider, storageName(domain.KindYandexDisk)}},
	{err: domain.ErrRemoteDamaged, code: codeRemoteDamaged, key: i18n.ErrRemoteDamaged},
	{err: domain.ErrRemoteTooLarge, code: codeRemoteTooLarge, key: i18n.ErrRemoteTooLarge},
	{err: domain.ErrRemoteOutcomeUnknown, code: codeRemoteOutcomeUnknown, key: i18n.ErrRemoteOutcomeUnknown},
	{err: domain.ErrLoopbackUnavailable, code: codeLoopbackUnavailable, key: i18n.ErrLoopbackUnavailable},
	{err: usecase.ErrSearchUnavailable, code: codeSearchUnavailable, key: i18n.ErrSearchUnavailable},
}

// ToUIError converts err into a *UIError by the table of spec §3.5; nil stays
// nil. Wrapped errors are recognized. A deadline or a cancellation wins over
// the error it caused, then come the domain errors, SSH, PostgreSQL, and
// everything else is internal.
func ToUIError(err error) error {
	if err == nil {
		return nil
	}

	return classify(err)
}

func classify(err error) *UIError {
	if ui, ok := errors.AsType[*UIError](err); ok {
		return ui
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return newUIError(codeTimeout, texts(i18n.ErrTimeout))
	case errors.Is(err, context.Canceled):
		return newUIError(codeCanceled, texts(i18n.ErrCanceled))
	}

	if ve, ok := errors.AsType[*domain.ValidationError](err); ok {
		return validationError(ve)
	}

	if hk, ok := errors.AsType[*domain.HostKeyUnknownError](err); ok {
		ui := newUIError(codeHostKeyUnknown, texts(i18n.ErrHostKeyUnknown, "fingerprint", hk.Fingerprint))
		ui.Fingerprint, ui.Address = hk.Fingerprint, hk.Addr

		return ui
	}

	if hk, ok := errors.AsType[*domain.HostKeyMismatchError](err); ok {
		ui := newUIError(codeHostKeyMismatch, texts(i18n.ErrHostKeyMismatch, "expected", hk.Expected, "got", hk.Got))
		ui.Expected, ui.Got = hk.Expected, hk.Got

		return ui
	}

	if ce, ok := errors.AsType[cloudhttp.CloudError](err); ok {
		return newUIError(codeCloud, cloudTexts(ce))
	}

	if re, ok := errors.AsType[*domain.ReauthRequiredError](err); ok {
		return newUIError(codeReauthRequired, texts(i18n.ErrReauthRequired, argProvider, storageName(re.Kind)))
	}

	if le, ok := errors.AsType[*domain.LoopbackUnavailableError](err); ok && !le.ManualCode {
		return newUIError(codeLoopbackUnavailable, texts(i18n.ErrLoopbackNoCode))
	}

	for _, s := range sentinels {
		if errors.Is(err, s.err) {
			return newUIError(s.code, texts(s.key, s.args...))
		}
	}

	switch {
	case isSSH(err):
		return newUIError(codeSSH, technical(err.Error()))
	case isDatabase(err):
		return newUIError(codeDatabase, technical(err.Error()))
	default:
		return newUIError(codeInternal, technical(err.Error()))
	}
}

// newUIError is the UIError of code with the texts m; its message is the
// English text.
func newUIError(code string, m map[i18n.Lang]string) *UIError {
	return &UIError{Code: code, Message: m[i18n.EN], Messages: m}
}

// localized is the text of every language of the interface, made by text.
func localized(text func(l i18n.Lang) string) map[i18n.Lang]string {
	m := make(map[i18n.Lang]string, len(i18n.Langs()))
	for _, l := range i18n.Langs() {
		m[l] = text(l)
	}

	return m
}

// texts is the text of k in every language, with the pairs of args.
func texts(k i18n.Key, args ...any) map[i18n.Lang]string {
	return localized(func(l i18n.Lang) string { return i18n.T(l, k, args...) })
}

// technical is a text that is the same in every language: the text of a
// library or of an unexpected error.
func technical(text string) map[i18n.Lang]string {
	return localized(func(i18n.Lang) string { return text })
}

// validationError is the UIError of ve. The account of a cloud has a text
// that tells what to do. A known field with a known reason is translated;
// anything else keeps the English text of ve in every language.
func validationError(ve *domain.ValidationError) *UIError {
	var m map[i18n.Lang]string

	if kind, ok := accountFields[ve.Field]; ok {
		m = texts(i18n.ErrSignInFirst, argProvider, storageName(kind))
	} else {
		m = validationTexts(ve)
	}

	ui := newUIError(codeValidation, m)
	ui.Field = ve.Field

	return ui
}

// reasonRules recognize the reasons of the validation errors by the English
// texts of their keys, which are the texts of domain.ValidationError.
var reasonRules = compileReasons(
	i18n.InvalidRequired, i18n.InvalidRequiredShort, i18n.InvalidControl, i18n.InvalidMinChars,
	i18n.InvalidMaxChars, i18n.InvalidNameTaken, i18n.InvalidConfirm, i18n.InvalidUUID, i18n.InvalidKind,
	i18n.InvalidNotCloud, i18n.InvalidYandexClientID, i18n.InvalidGoogleClientID, i18n.InvalidClientSecret,
	i18n.InvalidDBHost, i18n.InvalidPort, i18n.InvalidNotOffered, i18n.InvalidOfferedOther, i18n.InvalidOwnClient,
	i18n.InvalidKeyTooLarge, i18n.InvalidKeyDSA, i18n.InvalidKeyRSA, i18n.InvalidLanguage,
)

// reasonRule matches the reason of one key; params name its groups.
type reasonRule struct {
	key    i18n.Key
	re     *regexp.Regexp
	params []string
}

// validationPrefix starts the English text of every reason key, as
// domain.ValidationError.Error starts.
const validationPrefix = "invalid {field}: "

// placeholder is a {name} of a catalog text.
var placeholder = regexp.MustCompile(`\{(\w+)\}`)

// compileReasons makes the rule of each key from its English text after
// validationPrefix: the text itself, each {name} a number.
func compileReasons(keys ...i18n.Key) []reasonRule {
	rules := make([]reasonRule, 0, len(keys))

	for _, k := range keys {
		reason, ok := strings.CutPrefix(i18n.T(i18n.EN, k), validationPrefix)
		if !ok {
			panic("wailsui: the English text of " + string(k) + " does not start with " + validationPrefix)
		}

		var (
			pattern strings.Builder
			params  []string
			last    int
		)

		pattern.WriteString("^")

		for _, loc := range placeholder.FindAllStringSubmatchIndex(reason, -1) {
			pattern.WriteString(regexp.QuoteMeta(reason[last:loc[0]]) + "([0-9]+)")
			params = append(params, reason[loc[2]:loc[3]])
			last = loc[1]
		}

		pattern.WriteString(regexp.QuoteMeta(reason[last:]) + "$")
		rules = append(rules, reasonRule{key: k, re: regexp.MustCompile(pattern.String()), params: params})
	}

	return rules
}

// validationTexts is the text of ve in every language: the text of its
// reason with the name of its field, or the English text of ve in every
// language for an unknown field or reason.
func validationTexts(ve *domain.ValidationError) map[i18n.Lang]string {
	field := i18n.Key("field." + ve.Field)
	if i18n.T(i18n.EN, field) != ve.Field {
		return technical(ve.Error())
	}

	for _, r := range reasonRules {
		m := r.re.FindStringSubmatch(ve.Reason)
		if m == nil {
			continue
		}

		args := make([]any, 0, 2*len(r.params))
		for i, name := range r.params {
			args = append(args, name, m[i+1])
		}

		return localized(func(l i18n.Lang) string {
			return i18n.T(l, r.key, append([]any{"field", i18n.T(l, field)}, args...)...)
		})
	}

	return technical(ve.Error())
}

// storageName is the name of the cloud in the texts for the user; the kind
// itself for a cloud without a name here. The names are product names in
// Latin script in every language, so the {provider} templates need no
// declension.
func storageName(k domain.StorageKind) string {
	switch k {
	case domain.KindYandexDisk:
		return "Yandex Disk"
	case domain.KindGoogleDrive:
		return "Google Drive"
	default:
		return string(k)
	}
}

// cloudTexts is the text of a cloud error: the known reasons, otherwise the
// status, if any, and the code from the closed list of the provider.
func cloudTexts(ce cloudhttp.CloudError) map[i18n.Lang]string {
	name := storageName(ce.Provider)

	switch {
	case ce.Reason == cloudhttp.ReasonNoSpace:
		return texts(i18n.ErrCloudNoSpace, argProvider, name)
	case ce.Reason == cloudhttp.ReasonTooLarge:
		return texts(i18n.ErrCloudTooLarge, argProvider, name)
	case ce.Reason == cloudhttp.ReasonBusy:
		return texts(i18n.ErrCloudBusy, argProvider, name)
	case ce.Status == 0:
		return texts(i18n.ErrCloudCode, argProvider, name, "code", ce.Code)
	default:
		return texts(i18n.ErrCloudStatus, argProvider, name, "status", ce.Status, "code", ce.Code)
	}
}

func isSSH(err error) bool {
	_, ok := errors.AsType[*ssh.TransportError](err)
	return ok
}

func isDatabase(err error) bool {
	if _, ok := errors.AsType[*pgconn.PgError](err); ok {
		return true
	}

	_, ok := errors.AsType[*pgconn.ConnectError](err)

	return ok
}

// LogError writes err to log when its code is worth a record: internal with
// its text; ssh, database and timeout without it, since those texts name the
// user, the database and the host (spec §3.5). A cloud error is logged by
// its provider, status, code, step and network class, without its text, and
// reauth_required, remote_outcome_unknown and remote_damaged by the code
// alone (spec §9). Codes the interface explains to the user are not logged.
func LogError(log *slog.Logger, msg string, err error) {
	if err == nil {
		return
	}

	ui := classify(err)

	switch ui.Code {
	case codeInternal:
		log.Error(msg, "code", ui.Code, applog.Err(err))
	case codeSSH, codeDatabase, codeTimeout:
		log.Error(msg, "code", ui.Code, applog.Redacted(err))
	case codeCloud:
		ce, _ := errors.AsType[cloudhttp.CloudError](err)
		log.Error(msg, cloudAttrs(ui.Code, ce)...)
	case codeReauthRequired, codeRemoteOutcomeUnknown, codeRemoteDamaged:
		log.Error(msg, "code", ui.Code)
	}
}

// cloudAttrs are the log attributes of a cloud error; the step and the
// network class only when the error has them.
func cloudAttrs(code string, ce cloudhttp.CloudError) []any {
	attrs := []any{"code", code, "provider", string(ce.Provider), "status", ce.Status, "cloud_code", ce.Code}
	if ce.Step != "" {
		attrs = append(attrs, "step", ce.Step)
	}

	if ce.Net != "" {
		attrs = append(attrs, "net", ce.Net)
	}

	return attrs
}
