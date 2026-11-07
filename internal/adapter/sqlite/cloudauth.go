package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
	"github.com/nikchis/smartnote/internal/usecase"
)

func aadCloudAuth() sealbox.AAD {
	return sealbox.AAD{Table: tableCloudAuth, Column: columnData, RowID: "1"}
}

// LoadCloudToken returns the saved cloud token and true, or false when there is none.
func (s *Store) LoadCloudToken(ctx context.Context) (usecase.CloudToken, bool, error) {
	var (
		tok   usecase.CloudToken
		found bool
	)

	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		var sealed []byte

		err := db.QueryRowContext(ctx, "SELECT data FROM cloud_auth WHERE cloud_auth_id = 1").Scan(&sealed)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}

		if err != nil {
			return err
		}

		data, err := sealbox.Open(dek, sealed, aadCloudAuth())
		if err != nil {
			return fmt.Errorf("%w: cloud token: %w", domain.ErrCorruptStorage, err)
		}
		defer sealbox.Wipe(data)

		if err := json.Unmarshal(data, &tok); err != nil {
			return corrupt("cloud token is not valid json")
		}

		found = true

		return nil
	})
	if err != nil {
		return usecase.CloudToken{}, false, fmt.Errorf("load cloud token: %w", err)
	}

	return tok, found, nil
}

// SaveCloudToken stores the cloud token as sealed JSON in place of the old one.
func (s *Store) SaveCloudToken(ctx context.Context, t usecase.CloudToken) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		return putCloudToken(ctx, db, dek, t)
	})
	if err != nil {
		return fmt.Errorf("save cloud token: %w", err)
	}

	return nil
}

func putCloudToken(ctx context.Context, q querier, dek []byte, t usecase.CloudToken) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	defer sealbox.Wipe(data)

	sealed, err := sealbox.Seal(dek, data, aadCloudAuth())
	if err != nil {
		return err
	}

	_, err = q.ExecContext(ctx, `INSERT INTO cloud_auth (cloud_auth_id, data) VALUES (1, ?)
		ON CONFLICT (cloud_auth_id) DO UPDATE SET data = excluded.data`, sealed)

	return err
}

// SaveConnection stores the settings and applies the token change in one
// transaction. TokenDelete keeps the pending temporary file name, so that
// the next sign-in fences its move (spec §5.5).
func (s *Store) SaveConnection(ctx context.Context, settings domain.ConnectionSettings, change usecase.TokenChange, t usecase.CloudToken) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		return inTx(ctx, db, func(tx *sql.Tx) error {
			data, err := json.Marshal(settings)
			if err != nil {
				return err
			}
			defer sealbox.Wipe(data)

			sealed, err := sealbox.Seal(dek, data, aadSettings())
			if err != nil {
				return err
			}

			_, err = tx.ExecContext(ctx, `INSERT INTO connection (connection_id, settings) VALUES (1, ?)
				ON CONFLICT (connection_id) DO UPDATE SET settings = excluded.settings`, sealed)
			if err != nil {
				return err
			}

			switch change {
			case usecase.TokenKeep:
				return nil
			case usecase.TokenSet:
				return putCloudToken(ctx, tx, dek, t)
			case usecase.TokenDelete:
				// cloud_pending stays: the next sign-in fences the move it records.
				_, err := tx.ExecContext(ctx, "DELETE FROM cloud_auth")

				return err
			default:
				return fmt.Errorf("unknown token change %d", change)
			}
		})
	})
	if err != nil {
		return fmt.Errorf("save connection: %w", err)
	}

	return nil
}

// LoadPendingTemp returns the name of the temporary file waiting for fencing.
func (s *Store) LoadPendingTemp(ctx context.Context) (name string, found bool, err error) {
	err = s.unlocked(func(db *sql.DB, _ []byte) error {
		err := db.QueryRowContext(ctx, "SELECT temp_name FROM cloud_pending WHERE cloud_pending_id = 1").Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}

		found = err == nil

		return err
	})
	if err != nil {
		return "", false, fmt.Errorf("load pending temp: %w", err)
	}

	return name, found, nil
}

// SavePendingTemp stores name in place of the previous one.
func (s *Store) SavePendingTemp(ctx context.Context, name string) error {
	err := s.unlocked(func(db *sql.DB, _ []byte) error {
		_, err := db.ExecContext(ctx, `INSERT INTO cloud_pending (cloud_pending_id, temp_name) VALUES (1, ?)
			ON CONFLICT (cloud_pending_id) DO UPDATE SET temp_name = excluded.temp_name`, name)

		return err
	})
	if err != nil {
		return fmt.Errorf("save pending temp: %w", err)
	}

	return nil
}

// ClearPendingTemp forgets the pending name; it is not an error if there is none.
func (s *Store) ClearPendingTemp(ctx context.Context) error {
	err := s.unlocked(func(db *sql.DB, _ []byte) error {
		_, err := db.ExecContext(ctx, "DELETE FROM cloud_pending")

		return err
	})
	if err != nil {
		return fmt.Errorf("clear pending temp: %w", err)
	}

	return nil
}
