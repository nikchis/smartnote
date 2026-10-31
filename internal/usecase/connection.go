package usecase

import (
	"context"
	"fmt"

	"github.com/nikchis/smartnote/internal/domain"
)

// TestResult is what a connection test found; Empty means the server has no
// Smartnote storage yet.
type TestResult struct {
	Empty bool
}

// Connection loads, tests and saves the connection settings and the trusted
// server key. Every method but Load enters the Gate shared with Sync, so no
// setting changes during an operation with the server.
type Connection struct {
	settings SettingsStore
	remote   RemoteConnector
	gate     *Gate
	offer    *HostKeyOffer
	changed  func()
}

// NewConnection returns a Connection. g is the Gate and offer the
// HostKeyOffer shared with Sync; changed runs after the settings or the
// trusted key are saved (main connects it to Sync.ResetConnection).
func NewConnection(s SettingsStore, r RemoteConnector, g *Gate, offer *HostKeyOffer, changed func()) (*Connection, error) {
	switch {
	case s == nil:
		return nil, nilDependency("NewConnection", "settings")
	case r == nil:
		return nil, nilDependency("NewConnection", "remote")
	case g == nil:
		return nil, nilDependency("NewConnection", "gate")
	case offer == nil:
		return nil, nilDependency("NewConnection", "offer")
	case changed == nil:
		return nil, nilDependency("NewConnection", "changed")
	}

	return &Connection{settings: s, remote: r, gate: g, offer: offer, changed: changed}, nil
}

// Load returns the saved settings.
func (c *Connection) Load(ctx context.Context) (domain.ConnectionSettings, error) {
	s, err := c.settings.LoadSettings(ctx)
	if err != nil {
		return domain.ConnectionSettings{}, fmt.Errorf("load settings: %w", err)
	}

	return s, nil
}

// Test opens a session with s in OpenCheck mode and closes it, without saving
// s or creating the schema. Empty settings return domain.ErrNotConnected. An
// unknown server key is remembered for TrustOfferedKey.
func (c *Connection) Test(ctx context.Context, s domain.ConnectionSettings) (res TestResult, err error) {
	leave, err := c.gate.Enter()
	if err != nil {
		return TestResult{}, err
	}
	defer leave()

	s = s.WithDefaults()
	if err := s.Validate(); err != nil {
		return TestResult{}, err
	}

	if s.IsEmpty() {
		return TestResult{}, fmt.Errorf("test connection: %w", domain.ErrNotConnected)
	}

	sess, err := c.remote.Open(ctx, s, OpenCheck)
	if err != nil {
		c.offer.remember(err)
		return TestResult{}, fmt.Errorf("test connection: %w", err)
	}
	defer func() { err = joinCloseError(sess, err) }()

	return TestResult{Empty: sess.Empty()}, nil
}

// Save validates s and saves it with defaults; a trusted key bound to another
// address than s's is dropped, so the user is asked again. Empty settings are
// allowed and switch the app to local-only work.
func (c *Connection) Save(ctx context.Context, s domain.ConnectionSettings) error {
	s = s.WithDefaults()
	if err := s.Validate(); err != nil {
		return err
	}

	return c.write(func() error {
		if err := c.settings.SaveSettings(ctx, s.WithoutStaleTrust()); err != nil {
			return fmt.Errorf("save settings: %w", err)
		}

		return nil
	})
}

// TrustHostKey trusts, in the saved settings, the key with fingerprint that
// the server last presented as unknown. The key is bound to the address that
// presented it, which must still be the saved one.
func (c *Connection) TrustHostKey(ctx context.Context, fingerprint string) error {
	return c.write(func() error {
		s, err := c.settings.LoadSettings(ctx)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}

		offer, err := c.offer.take(fingerprint, s.SSHAddr())
		if err != nil {
			return err
		}

		s.TrustedHostKey, s.TrustedHostAddr = offer.AuthorizedKey, offer.Addr

		return c.save(ctx, s)
	})
}

// TrustOfferedKey returns s trusting the key with fingerprint that the server
// at s's address last presented as unknown, for the settings dialog, which
// saves it only on OK. Nothing is saved here.
func (c *Connection) TrustOfferedKey(fingerprint string, s domain.ConnectionSettings) (domain.ConnectionSettings, error) {
	// The dialog may leave the port empty; the key was offered for the default.
	offer, err := c.offer.take(fingerprint, s.WithDefaults().SSHAddr())
	if err != nil {
		return domain.ConnectionSettings{}, err
	}

	s.TrustedHostKey, s.TrustedHostAddr = offer.AuthorizedKey, offer.Addr

	return s, nil
}

// ResetHostKey clears the trusted server key in the saved settings.
func (c *Connection) ResetHostKey(ctx context.Context) error {
	return c.write(func() error {
		s, err := c.settings.LoadSettings(ctx)
		if err != nil {
			return fmt.Errorf("load settings: %w", err)
		}

		s.TrustedHostKey, s.TrustedHostAddr = "", ""

		return c.save(ctx, s)
	})
}

func (c *Connection) save(ctx context.Context, s domain.ConnectionSettings) error {
	if err := c.settings.SaveSettings(ctx, s); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}

	return nil
}

// write runs save inside the Gate and reports the change after a success.
func (c *Connection) write(save func() error) error {
	leave, err := c.gate.Enter()
	if err != nil {
		return err
	}
	defer leave()

	if err := save(); err != nil {
		return err
	}

	c.changed()

	return nil
}
