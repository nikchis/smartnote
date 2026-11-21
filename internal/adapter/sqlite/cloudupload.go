package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/nikchis/smartnote/internal/domain"
	"github.com/nikchis/smartnote/internal/sealbox"
)

// aadCloudUpload is the AAD of the unfinished upload record of provider p
// (spec §7.2): smartnote|1|cloud_upload|data|<provider>.
func aadCloudUpload(p domain.StorageKind) sealbox.AAD {
	return sealbox.AAD{Table: tableCloudUpload, Column: columnData, RowID: string(p)}
}

// cloudUploadProvider checks that p is a cloud provider, the only kind with
// an upload record.
func cloudUploadProvider(p domain.StorageKind) error {
	if !domain.IsCloud(p) {
		return fmt.Errorf("upload record of %q: not a cloud provider", p)
	}

	return nil
}

// LoadCloudUpload returns the record of the unfinished upload of provider p
// and true, or false when there is none (spec §5.4). The record is opaque to
// the storage; it is kept sealed, because it lets anyone finish the upload.
func (s *Store) LoadCloudUpload(ctx context.Context, p domain.StorageKind) (data []byte, found bool, err error) {
	if err := cloudUploadProvider(p); err != nil {
		return nil, false, err
	}

	err = s.unlocked(func(db *sql.DB, dek []byte) error {
		var sealed []byte

		err := db.QueryRowContext(ctx, "SELECT data FROM cloud_upload WHERE provider = ?", string(p)).Scan(&sealed)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}

		if err != nil {
			return err
		}

		if data, err = sealbox.Open(dek, sealed, aadCloudUpload(p)); err != nil {
			return fmt.Errorf("%w: upload record: %w", domain.ErrCorruptStorage, err)
		}

		found = true

		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("load upload record: %w", err)
	}

	return data, found, nil
}

// SaveCloudUpload seals data with the DEK and stores it in place of the
// record of provider p.
func (s *Store) SaveCloudUpload(ctx context.Context, p domain.StorageKind, data []byte) error {
	if err := cloudUploadProvider(p); err != nil {
		return err
	}

	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		sealed, err := sealbox.Seal(dek, data, aadCloudUpload(p))
		if err != nil {
			return err
		}

		_, err = db.ExecContext(ctx, `INSERT INTO cloud_upload (provider, data) VALUES (?, ?)
			ON CONFLICT (provider) DO UPDATE SET data = excluded.data`, string(p), sealed)

		return err
	})
	if err != nil {
		return fmt.Errorf("save upload record: %w", err)
	}

	return nil
}

// ClearCloudUpload removes the record of provider p; it is not an error if
// there is none.
func (s *Store) ClearCloudUpload(ctx context.Context, p domain.StorageKind) error {
	if err := cloudUploadProvider(p); err != nil {
		return err
	}

	err := s.unlocked(func(db *sql.DB, _ []byte) error {
		_, err := db.ExecContext(ctx, "DELETE FROM cloud_upload WHERE provider = ?", string(p))
		return err
	})
	if err != nil {
		return fmt.Errorf("clear upload record: %w", err)
	}

	return nil
}
