package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/nikchis/smartnote/internal/sealbox"
)

// aadCloudAuth is the AAD of the Yandex token in cloud_auth, the table
// 2.2.x reads.
func aadCloudAuth() sealbox.AAD {
	return sealbox.AAD{Table: tableCloudAuth, Column: columnData, RowID: "1"}
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
