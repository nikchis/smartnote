package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// LoadSettings returns the connection settings; a new storage has empty ones.
func (s *Store) LoadSettings(ctx context.Context) (domain.ConnectionSettings, error) {
	var settings domain.ConnectionSettings
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		var sealed []byte
		err := db.QueryRowContext(ctx, "SELECT settings FROM connection WHERE id = 1").Scan(&sealed)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && sealed == nil) {
			return nil
		}
		if err != nil {
			return err
		}
		data, err := sealbox.Open(dek, sealed, aadSettings())
		if err != nil {
			return fmt.Errorf("%w: settings: %w", domain.ErrCorruptStorage, err)
		}
		defer sealbox.Wipe(data)
		if err := json.Unmarshal(data, &settings); err != nil {
			return corrupt("settings are not valid json")
		}
		return nil
	})
	if err != nil {
		return domain.ConnectionSettings{}, fmt.Errorf("load settings: %w", err)
	}
	return settings, nil
}

// SaveSettings stores the connection settings as sealed JSON.
func (s *Store) SaveSettings(ctx context.Context, settings domain.ConnectionSettings) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		data, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		defer sealbox.Wipe(data)
		sealed, err := sealbox.Seal(dek, data, aadSettings())
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO connection (id, settings) VALUES (1, ?)
			ON CONFLICT (id) DO UPDATE SET settings = excluded.settings`, sealed)
		return err
	})
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

// LoadKey returns the client SSH key and true, or false when there is none.
func (s *Store) LoadKey(ctx context.Context) (domain.KeyPair, bool, error) {
	var key domain.KeyPair
	var found bool
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		var sealed []byte
		var public string
		err := db.QueryRowContext(ctx, "SELECT private_key, public_key FROM ssh_key WHERE id = 1").Scan(&sealed, &public)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		private, err := sealbox.Open(dek, sealed, aadPrivateKey())
		if err != nil {
			return fmt.Errorf("%w: ssh key: %w", domain.ErrCorruptStorage, err)
		}
		key, found = domain.KeyPair{PrivatePEM: private, PublicAuthorized: public}, true
		return nil
	})
	if err != nil {
		return domain.KeyPair{}, false, fmt.Errorf("load ssh key: %w", err)
	}
	return key, found, nil
}

// SaveKey stores the client SSH key in place of the old one.
func (s *Store) SaveKey(ctx context.Context, k domain.KeyPair) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		sealed, err := sealbox.Seal(dek, k.PrivatePEM, aadPrivateKey())
		if err != nil {
			return err
		}
		_, err = db.ExecContext(ctx, `INSERT INTO ssh_key (id, private_key, public_key) VALUES (1, ?, ?)
			ON CONFLICT (id) DO UPDATE SET private_key = excluded.private_key, public_key = excluded.public_key`,
			sealed, k.PublicAuthorized)
		return err
	})
	if err != nil {
		return fmt.Errorf("save ssh key: %w", err)
	}
	return nil
}
