package wailsui

import "github.com/nikchis/smartnote/internal/usecase"

// ConnectionService loads, tests and saves the connection settings and the
// trusted server key.
type ConnectionService struct {
	env  Env
	conn *usecase.Connection
}

// NewConnectionService returns a ConnectionService over c.
func NewConnectionService(env Env, c *usecase.Connection) *ConnectionService {
	return &ConnectionService{env: env, conn: c}
}

// Load returns the saved settings.
func (s *ConnectionService) Load() (SettingsDTO, error) {
	settings, err := s.conn.Load(s.env.Ctx)
	if err != nil {
		return SettingsDTO{}, s.env.fail("load settings", err)
	}

	return toSettingsDTO(settings), nil
}

// Test connects with settings without saving them; empty reports that the
// server has no Smartnote storage yet.
func (s *ConnectionService) Test(settings SettingsDTO) (empty bool, err error) {
	ctx, cancel := s.env.remote()
	defer cancel()

	res, err := s.conn.Test(ctx, settings.settings())
	if err != nil {
		return false, s.env.fail("test connection", err)
	}

	return res.Empty, nil
}

// Save saves settings.
func (s *ConnectionService) Save(settings SettingsDTO) error {
	return s.env.fail("save settings", s.conn.Save(s.env.Ctx, settings.settings()))
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

	return toSettingsDTO(trusted), nil
}

// ResetHostKey clears the trusted server key in the saved settings.
func (s *ConnectionService) ResetHostKey() error {
	return s.env.fail("reset host key", s.conn.ResetHostKey(s.env.Ctx))
}
