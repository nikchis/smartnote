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
// server key.
type Connection struct {
	settings SettingsStore
	remote   RemoteConnector
	gate     *Gate
}

// NewConnection returns a Connection; g is the Gate shared with Sync.
func NewConnection(s SettingsStore, r RemoteConnector, g *Gate) (*Connection, error) {
	switch {
	case s == nil:
		return nil, nilDependency("NewConnection", "settings")
	case r == nil:
		return nil, nilDependency("NewConnection", "remote")
	case g == nil:
		return nil, nilDependency("NewConnection", "gate")
	}
	return &Connection{settings: s, remote: r, gate: g}, nil
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
// s or creating the schema. Empty settings return domain.ErrNotConnected.
func (c *Connection) Test(ctx context.Context, s domain.ConnectionSettings) (res TestResult, err error) {
	leave, err := c.gate.Enter()
	if err != nil {
		return TestResult{}, err
	}
	defer leave()

	if err := s.Validate(); err != nil {
		return TestResult{}, err
	}
	if s.IsEmpty() {
		return TestResult{}, fmt.Errorf("test connection: %w", domain.ErrNotConnected)
	}
	sess, err := c.remote.Open(ctx, s.WithDefaults(), OpenCheck)
	if err != nil {
		return TestResult{}, fmt.Errorf("test connection: %w", err)
	}
	defer closeSession(sess, &err)
	return TestResult{Empty: sess.Empty()}, nil
}

// Save validates s and saves it with defaults; empty settings are allowed and
// switch the app to local-only work.
func (c *Connection) Save(ctx context.Context, s domain.ConnectionSettings) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if err := c.settings.SaveSettings(ctx, s.WithDefaults()); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

// TrustHostKey stores authorizedKey as the trusted server key in the saved settings.
func (c *Connection) TrustHostKey(ctx context.Context, authorizedKey string) error {
	if authorizedKey == "" {
		return &domain.ValidationError{Field: "trusted_host_key", Reason: "is required"}
	}
	return c.setHostKey(ctx, authorizedKey)
}

// ResetHostKey clears the trusted server key in the saved settings.
func (c *Connection) ResetHostKey(ctx context.Context) error {
	return c.setHostKey(ctx, "")
}

func (c *Connection) setHostKey(ctx context.Context, key string) error {
	s, err := c.settings.LoadSettings(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	s.TrustedHostKey = key
	if err := c.settings.SaveSettings(ctx, s); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}
