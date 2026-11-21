package wailsui

import (
	"sync"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/usecase"
)

// ConnectionService loads, tests and saves the connection settings and the
// trusted server key. The interface names the key only by fingerprint: a key
// trusted during Test waits here, in pending, until OK saves it (spec §6.1).
type ConnectionService struct {
	env      Env
	conn     *usecase.Connection
	progress *ProgressFeed // Test ends the progress of its transfer here

	mu      sync.Mutex
	pending trustedKey
}

// trustedKey is a trusted server key in authorized_keys format with the
// address it is trusted for.
type trustedKey struct {
	key, addr string
}

// NewConnectionService returns a ConnectionService over c that ends the
// progress of Test in progress; nil sends nothing.
func NewConnectionService(env Env, c *usecase.Connection, progress *ProgressFeed) *ConnectionService {
	if progress == nil {
		progress = NewProgressFeed(nil)
	}

	return &ConnectionService{env: env, conn: c, progress: progress}
}

// AccountDTO is the sign-in of a cloud as the settings form shows it: the
// account of the token, if the provider reports one, and whether there is a
// token. The token itself never crosses.
type AccountDTO struct {
	Account  string `json:"account"`
	SignedIn bool   `json:"signedIn"`
}

// Load returns the saved settings for the settings dialog; a key trusted in
// a dialog closed without OK, the pending sign-ins and the Sign outs are
// forgotten. Until the form asks Account, it also reports the Yandex
// account whatever kind is saved.
func (s *ConnectionService) Load() (SettingsDTO, error) {
	s.setPending(trustedKey{})

	s.conn.DiscardPending()

	settings, err := s.conn.Load(s.env.Ctx)
	if err != nil {
		return SettingsDTO{}, s.env.fail("load settings", err)
	}

	dto := toSettingsDTO(settings)

	yandex := settings
	yandex.StorageKind = domain.KindYandexDisk
	acc := s.account(yandex)
	dto.YandexAccount, dto.YandexSignedIn = acc.Account, acc.SignedIn

	return dto, nil
}

// Account returns the sign-in for the kind and the effective client of the
// draft settings of the form: the pending token of this session of the
// form, else the saved one, unless the form signed out. Unlike Load it
// keeps the pending sign-ins and the Sign outs. A kind that is not a cloud
// is not signed in.
func (s *ConnectionService) Account(settings SettingsDTO) (AccountDTO, error) {
	return s.account(settings.settings()), nil
}

// account is the sign-in for the draft d. A token that cannot be read must
// not keep Settings closed: it shows as Not signed in. The error text may
// carry details of the token store, so only its code is logged.
func (s *ConnectionService) account(d domain.ConnectionSettings) AccountDTO {
	account, signedIn, err := s.conn.Account(s.env.Ctx, d)
	if err != nil {
		s.env.Log.Error("load cloud account", "code", classify(err).Code)
		return AccountDTO{}
	}

	return AccountDTO{Account: account, SignedIn: signedIn}
}

// SignIn signs in to the cloud of settings.kind through the browser and the
// loopback receiver, with the effective client of settings, and returns the
// account. It waits for the browser up to the sign-in's own limit, so it
// runs on the application context rather than under the operation timeout;
// CancelSignIn ends it. The token stays pending until Save.
func (s *ConnectionService) SignIn(settings SettingsDTO) (string, error) {
	account, err := s.conn.SignIn(s.env.Ctx, settings.settings())
	if err != nil {
		return "", s.env.fail("sign in", err)
	}

	return account, nil
}

// SignInManual opens the browser for a sign-in by code to the cloud of
// settings.kind; SubmitCode finishes it. Google Drive has none.
func (s *ConnectionService) SignInManual(settings SettingsDTO) error {
	return s.env.fail("sign in manually", s.conn.SignInManual(s.env.Ctx, settings.settings()))
}

// SubmitCode finishes the manual sign-in with the code the user copied and
// returns the account.
func (s *ConnectionService) SubmitCode(code string) (string, error) {
	ctx, cancel := s.env.remote()
	defer cancel()

	account, err := s.conn.SubmitCode(ctx, code)
	if err != nil {
		return "", s.env.fail("submit code", err)
	}

	return account, nil
}

// CancelSignIn ends the sign-in in progress, if any.
func (s *ConnectionService) CancelSignIn() { s.conn.CancelSignIn() }

// SignOut forgets the token of the cloud kind; Save makes it final. A kind
// that is not a cloud changes nothing.
func (s *ConnectionService) SignOut(kind string) { s.conn.SignOut(domain.StorageKind(kind)) }

// Test connects with settings without saving them; empty reports that the
// server has no Smartnote storage yet.
func (s *ConnectionService) Test(settings SettingsDTO) (empty bool, err error) {
	defer s.progress.End()

	trusted, err := s.withTrust(settings)
	if err != nil {
		return false, s.env.fail("test connection", err)
	}

	ctx, cancel := s.env.remoteFor(trusted.Kind())
	defer cancel()

	res, err := s.conn.Test(ctx, trusted)
	if err != nil {
		return false, s.env.fail("test connection", err)
	}

	return res.Empty, nil
}

// Save saves settings with the key TrustedFingerprint names.
func (s *ConnectionService) Save(settings SettingsDTO) error {
	trusted, err := s.withTrust(settings)
	if err == nil {
		err = s.conn.Save(s.env.Ctx, trusted)
	}

	if err != nil {
		return s.env.fail("save settings", err)
	}

	s.setPending(trustedKey{})

	return nil
}

// TrustHostKey trusts, in the saved settings, the key with fingerprint that
// the server presented as unknown.
func (s *ConnectionService) TrustHostKey(fingerprint string) error {
	return s.env.fail("trust host key", s.conn.TrustHostKey(s.env.Ctx, fingerprint))
}

// TrustOfferedKey returns settings trusting the key with fingerprint that the
// server presented during Test; nothing is saved until Save.
func (s *ConnectionService) TrustOfferedKey(fingerprint string, settings SettingsDTO) (SettingsDTO, error) {
	trusted, err := s.conn.TrustOfferedKey(fingerprint, settings.settings())
	if err != nil {
		return SettingsDTO{}, s.env.fail("trust offered key", err)
	}

	s.setPending(trustedKey{key: trusted.TrustedHostKey, addr: trusted.TrustedHostAddr})
	settings.TrustedFingerprint = trustRef(trusted.TrustedHostKey)

	return settings, nil
}

// ResetHostKey clears the trusted server key in the saved settings.
func (s *ConnectionService) ResetHostKey() error {
	return s.env.fail("reset host key", s.conn.ResetHostKey(s.env.Ctx))
}

func (s *ConnectionService) setPending(k trustedKey) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pending = k
}

// withTrust returns the settings of the dialog with the trusted key that
// TrustedFingerprint names: the one trusted during Test, or the saved one.
func (s *ConnectionService) withTrust(d SettingsDTO) (domain.ConnectionSettings, error) {
	settings := d.settings()
	if d.TrustedFingerprint == "" {
		return settings, nil
	}

	s.mu.Lock()
	pending := s.pending
	s.mu.Unlock()

	if pending.key != "" && trustRef(pending.key) == d.TrustedFingerprint {
		settings.TrustedHostKey, settings.TrustedHostAddr = pending.key, pending.addr
		return settings, nil
	}

	saved, err := s.conn.Load(s.env.Ctx)
	if err != nil {
		return domain.ConnectionSettings{}, err
	}

	if saved.TrustedHostKey == "" || trustRef(saved.TrustedHostKey) != d.TrustedFingerprint {
		return domain.ConnectionSettings{}, errUnknownTrust
	}

	settings.TrustedHostKey, settings.TrustedHostAddr = saved.TrustedHostKey, saved.TrustedHostAddr

	return settings, nil
}

// errUnknownTrust rejects a fingerprint that is neither the saved key's nor
// one the user trusted in this dialog.
var errUnknownTrust = &domain.ValidationError{Field: "trusted_host_key", Reason: "was not offered by the server"}
