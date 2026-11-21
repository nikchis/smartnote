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

func aadCloudToken(p domain.StorageKind) sealbox.AAD {
	return sealbox.AAD{Table: tableCloudToken, Column: columnData, RowID: string(p)}
}

// tokenRow is the row that holds the token of one provider (spec §7.2): the
// Yandex token stays in cloud_auth, which 2.2.x reads after a rollback; the
// token of any other cloud is the row of cloud_token keyed by its provider.
type tokenRow struct {
	selectSQL, putSQL, deleteSQL string
	key                          []any // the arguments that select the row
	aad                          sealbox.AAD
}

func tokenRowOf(p domain.StorageKind) (tokenRow, error) {
	switch {
	case p == domain.KindYandexDisk:
		return tokenRow{
			selectSQL: "SELECT data FROM cloud_auth WHERE cloud_auth_id = 1",
			putSQL: `INSERT INTO cloud_auth (cloud_auth_id, data) VALUES (1, ?)
				ON CONFLICT (cloud_auth_id) DO UPDATE SET data = excluded.data`,
			deleteSQL: "DELETE FROM cloud_auth",
			aad:       aadCloudAuth(),
		}, nil
	case domain.IsCloud(p):
		return tokenRow{
			selectSQL: "SELECT data FROM cloud_token WHERE provider = ?",
			putSQL: `INSERT INTO cloud_token (provider, data) VALUES (?, ?)
				ON CONFLICT (provider) DO UPDATE SET data = excluded.data`,
			deleteSQL: "DELETE FROM cloud_token WHERE provider = ?",
			key:       []any{string(p)},
			aad:       aadCloudToken(p),
		}, nil
	default:
		return tokenRow{}, fmt.Errorf("cloud token of %q: not a cloud provider", p)
	}
}

// LoadCloudToken returns the saved token of provider p and true, or false
// when there is none.
func (s *Store) LoadCloudToken(ctx context.Context, p domain.StorageKind) (usecase.CloudToken, bool, error) {
	var (
		tok   usecase.CloudToken
		found bool
	)

	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		var err error

		tok, found, err = getCloudToken(ctx, db, dek, p)

		return err
	})
	if err != nil {
		return usecase.CloudToken{}, false, fmt.Errorf("load cloud token: %w", err)
	}

	return tok, found, nil
}

// SaveCloudToken stores t as sealed JSON in place of the old token of
// t.Provider; the tokens of other providers stay.
func (s *Store) SaveCloudToken(ctx context.Context, t usecase.CloudToken) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		return putCloudToken(ctx, db, dek, t)
	})
	if err != nil {
		return fmt.Errorf("save cloud token: %w", err)
	}

	return nil
}

// SaveConnection stores the settings and applies the token updates in one
// transaction: a failed update leaves the settings and every token as they
// were. TokenDelete of Yandex keeps the pending temporary file name, so that
// the next sign-in fences its move (yandex spec §5.5).
func (s *Store) SaveConnection(ctx context.Context, settings domain.ConnectionSettings, updates []usecase.TokenUpdate) error {
	err := s.unlocked(func(db *sql.DB, dek []byte) error {
		return inTx(ctx, db, func(tx *sql.Tx) error {
			if err := putSettings(ctx, tx, dek, settings); err != nil {
				return err
			}

			for _, u := range updates {
				if err := applyTokenUpdate(ctx, tx, dek, u); err != nil {
					return err
				}
			}

			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("save connection: %w", err)
	}

	return nil
}

func applyTokenUpdate(ctx context.Context, q querier, dek []byte, u usecase.TokenUpdate) error {
	switch u.Change {
	case usecase.TokenSet:
		if u.Token.Provider != u.Provider {
			return fmt.Errorf("token of %q set for %q", u.Token.Provider, u.Provider)
		}

		return putCloudToken(ctx, q, dek, u.Token)
	case usecase.TokenDelete:
		row, err := tokenRowOf(u.Provider)
		if err != nil {
			return err
		}

		// cloud_pending stays: the next sign-in fences the move it records.
		_, err = q.ExecContext(ctx, row.deleteSQL, row.key...)

		return err
	default:
		return fmt.Errorf("unknown token change %d", u.Change)
	}
}

func getCloudToken(ctx context.Context, q querier, dek []byte, p domain.StorageKind) (usecase.CloudToken, bool, error) {
	row, err := tokenRowOf(p)
	if err != nil {
		return usecase.CloudToken{}, false, err
	}

	var sealed []byte

	err = q.QueryRowContext(ctx, row.selectSQL, row.key...).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return usecase.CloudToken{}, false, nil
	}

	if err != nil {
		return usecase.CloudToken{}, false, err
	}

	data, err := sealbox.Open(dek, sealed, row.aad)
	if err != nil {
		return usecase.CloudToken{}, false, fmt.Errorf("%w: cloud token: %w", domain.ErrCorruptStorage, err)
	}
	defer sealbox.Wipe(data)

	var tok usecase.CloudToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return usecase.CloudToken{}, false, corrupt("cloud token is not valid json")
	}

	// The row, bound by its AAD, names the provider.
	tok.Provider = p

	return tok, true, nil
}

func putCloudToken(ctx context.Context, q querier, dek []byte, t usecase.CloudToken) error {
	row, err := tokenRowOf(t.Provider)
	if err != nil {
		return err
	}

	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	defer sealbox.Wipe(data)

	sealed, err := sealbox.Seal(dek, data, row.aad)
	if err != nil {
		return err
	}

	_, err = q.ExecContext(ctx, row.putSQL, append(row.key, sealed)...)

	return err
}
