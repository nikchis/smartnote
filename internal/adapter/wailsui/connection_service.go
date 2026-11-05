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
	env  Env
	conn *usecase.Connection

	mu      sync.Mutex
	pending trustedKey
}

// trustedKey is a trusted server key in authorized_keys format with the
// address it is trusted for.
type trustedKey struct {
	key, addr string
}

// NewConnectionService returns a ConnectionService over c.
func NewConnectionService(env Env, c *usecase.Connection) *ConnectionService {
	return &ConnectionService{env: env, conn: c}
}

// Load returns the saved settings for the settings dialog; a key trusted in
// a dialog closed without OK is forgotten.
func (s *ConnectionService) Load() (SettingsDTO, error) {
	s.setPending(trustedKey{})

	s.conn.DiscardPending()

	settings, err := s.conn.Load(s.env.Ctx)
	if err != nil {
		return SettingsDTO{}, s.env.fail("load settings", err)
	}

	dto := toSettingsDTO(settings)

	dto.YandexAccount, dto.YandexSignedIn, err = s.conn.Account(s.env.Ctx, settings)
	if err != nil {
		return SettingsDTO{}, s.env.fail("load settings", err)
	}

	return dto, nil
}

// SignIn signs in to Yandex Disk through the browser and the loopback
// receiver and returns the account. It waits for the browser up to the
// sign-in's own limit, so it runs on the application context rather than
// under the operation timeout; CancelSignIn ends it. The token stays pending
// until Save.
func (s *ConnectionService) SignIn(settings SettingsDTO) (string, error) {
	account, err := s.conn.SignIn(s.env.Ctx, settings.settings())
	if err != nil {
		return "", s.env.fail("sign in", err)
	}

	return account, nil
}

// SignInManual opens the browser for a sign-in by code; SubmitCode finishes it.
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

// SignOut forgets the Yandex Disk token; Save makes it final.
func (s *ConnectionService) SignOut() { s.conn.SignOut() }

// Test connects with settings without saving them; empty reports that the
// server has no Smartnote storage yet.
func (s *ConnectionService) Test(settings SettingsDTO) (empty bool, err error) {
	trusted, err := s.withTrust(settings)
	if err != nil {
		return false, s.env.fail("test connection", err)
	}

	ctx, cancel := s.env.remote()
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
