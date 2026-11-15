package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func (p DatabaseProvider) initDeliveries() error {
	return p.rawSqlite(`CREATE TABLE IF NOT EXISTS DeliveryLists (
		Token TEXT PRIMARY KEY NOT NULL,
		OwnerID INTEGER NOT NULL,
		Title TEXT NOT NULL,
		Note TEXT NOT NULL,
		FileIDs BLOB NOT NULL,
		CreatedAt INTEGER NOT NULL,
		ExpiresAt INTEGER NOT NULL,
		RevokedAt INTEGER NOT NULL
	) WITHOUT ROWID;
	CREATE INDEX IF NOT EXISTS DeliveryListsOwner ON DeliveryLists(OwnerID, CreatedAt);
	CREATE INDEX IF NOT EXISTS DeliveryListsOwnerPage ON DeliveryLists(OwnerID, CreatedAt DESC, Token DESC);
	CREATE INDEX IF NOT EXISTS DeliveryListsExpiry ON DeliveryLists(ExpiresAt) WHERE ExpiresAt > 0;
	CREATE INDEX IF NOT EXISTS DeliveryListsRevoked ON DeliveryLists(RevokedAt) WHERE RevokedAt > 0;`)
}

func (p DatabaseProvider) checkDeliveryContext(ctx context.Context) error {
	if ctx == nil {
		return uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.sqliteDb == nil {
		return uploadquota.ErrSchema
	}
	return nil
}

func deliveryReadError(err error) error {
	if err != nil && strings.Contains(err.Error(), "no such table: DeliveryLists") {
		return errors.Join(uploadquota.ErrSchema, uploadquota.ErrNotFound)
	}
	return err
}

func (p DatabaseProvider) InsertDelivery(ctx context.Context, d models.Delivery) (bool, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return false, err
	}
	if !d.Valid() {
		return false, uploadquota.ErrInvalid
	}
	ids, err := json.Marshal(d.FileIDs)
	if err != nil {
		return false, err
	}
	result, err := p.sqliteDb.ExecContext(ctx, `INSERT INTO DeliveryLists
		(Token, OwnerID, Title, Note, FileIDs, CreatedAt, ExpiresAt, RevokedAt)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(Token) DO NOTHING`,
		d.Token, d.OwnerID, d.Title, d.Note, ids, d.CreatedAt, d.ExpiresAt, d.RevokedAt)
	if err != nil {
		return false, deliveryReadError(err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

const deliveryColumns = `Token, OwnerID,
	CASE WHEN length(CAST(Title AS BLOB)) <= 800 THEN Title ELSE NULL END,
	CASE WHEN length(CAST(Note AS BLOB)) <= 8000 THEN Note ELSE NULL END,
	CASE WHEN length(FileIDs) <= 12000 THEN FileIDs ELSE NULL END,
	CreatedAt, ExpiresAt, RevokedAt`

type deliveryScanner interface{ Scan(...any) error }

func scanDelivery(row deliveryScanner) (models.Delivery, error) {
	var d models.Delivery
	var ids []byte
	if err := row.Scan(&d.Token, &d.OwnerID, &d.Title, &d.Note, &ids, &d.CreatedAt, &d.ExpiresAt, &d.RevokedAt); err != nil {
		return models.Delivery{}, err
	}
	if err := json.Unmarshal(ids, &d.FileIDs); err != nil || !d.Valid() {
		return models.Delivery{}, uploadquota.ErrInvalid
	}
	return d, nil
}

func (p DatabaseProvider) ReadDelivery(ctx context.Context, token string) (models.Delivery, bool, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return models.Delivery{}, false, err
	}
	if !models.ValidDeliveryToken(token) {
		return models.Delivery{}, false, uploadquota.ErrInvalid
	}
	d, err := scanDelivery(p.sqliteDb.QueryRowContext(ctx, "SELECT "+deliveryColumns+" FROM DeliveryLists WHERE Token = ?", token))
	if errors.Is(err, sql.ErrNoRows) {
		return models.Delivery{}, false, nil
	}
	return d, err == nil, deliveryReadError(err)
}

func (p DatabaseProvider) ListDeliveries(ctx context.Context, ownerID int) ([]models.Delivery, error) {
	if ownerID <= 0 {
		return nil, uploadquota.ErrInvalid
	}
	return p.readDeliveries(ctx, " WHERE OwnerID = ?", ownerID)
}

// ReadAllDeliveries is an offline migration primitive, not a public listing.
func (p DatabaseProvider) ReadAllDeliveries(ctx context.Context) ([]models.Delivery, error) {
	return p.readDeliveries(ctx, "")
}

func (p DatabaseProvider) readDeliveries(ctx context.Context, where string, args ...any) ([]models.Delivery, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return nil, err
	}
	rows, err := p.sqliteDb.QueryContext(ctx, "SELECT "+deliveryColumns+" FROM DeliveryLists"+where+" ORDER BY CreatedAt DESC, Token", args...)
	if err != nil {
		return nil, deliveryReadError(err)
	}
	defer rows.Close()
	result := make([]models.Delivery, 0)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (p DatabaseProvider) RevokeDelivery(ctx context.Context, token string, ownerID int, now int64) (bool, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return false, err
	}
	if !models.ValidDeliveryToken(token) || ownerID <= 0 || now <= 0 {
		return false, uploadquota.ErrInvalid
	}
	result, err := p.sqliteDb.ExecContext(ctx, `UPDATE DeliveryLists SET RevokedAt = CASE WHEN RevokedAt = 0 THEN ? ELSE RevokedAt END WHERE Token = ? AND OwnerID = ?`, now, token, ownerID)
	if err != nil {
		return false, deliveryReadError(err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

// CreateDeliveryLimited admits quota and writes in the same SQLite statement.
// Retained expired/revoked rows count until pruned, bounding persisted records.
func (p DatabaseProvider) CreateDeliveryLimited(ctx context.Context, d models.Delivery, now int64) (bool, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return false, err
	}
	if !d.Valid() || now <= 0 {
		return false, uploadquota.ErrInvalid
	}
	if _, err := p.PruneDeliveries(ctx, now, 100); err != nil {
		return false, err
	}
	ids, err := json.Marshal(d.FileIDs)
	if err != nil {
		return false, err
	}
	result, err := p.sqliteDb.ExecContext(ctx, `INSERT INTO DeliveryLists
		(Token, OwnerID, Title, Note, FileIDs, CreatedAt, ExpiresAt, RevokedAt)
		SELECT ?, ?, ?, ?, ?, ?, ?, ?
		WHERE (SELECT COUNT(*) FROM DeliveryLists) < ?
		AND (SELECT COUNT(*) FROM DeliveryLists WHERE OwnerID = ?) < ?
		ON CONFLICT(Token) DO NOTHING`, d.Token, d.OwnerID, d.Title, d.Note, ids,
		d.CreatedAt, d.ExpiresAt, d.RevokedAt, models.MaxDeliveriesGlobal, d.OwnerID, models.MaxDeliveriesPerOwner)
	if err != nil {
		return false, deliveryReadError(err)
	}
	n, err := result.RowsAffected()
	if err != nil || n == 1 {
		return n == 1, err
	}
	// A collision is retryable by the service; quota exhaustion is not.
	var exists int
	err = p.sqliteDb.QueryRowContext(ctx, `SELECT 1 FROM DeliveryLists WHERE Token = ?`, d.Token).Scan(&exists)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, deliveryReadError(err)
	}
	return false, models.ErrDeliveryQuota
}

// ListDeliveriesPage uses an owner/creation/token index and a keyset cursor. The cursor
// need not still exist, so pruning or revocation between pages cannot skip peers.
func (p DatabaseProvider) ListDeliveriesPage(ctx context.Context, ownerID int, beforeToken string, limit int) ([]models.Delivery, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return nil, err
	}
	if ownerID <= 0 || limit < 1 || limit > 101 {
		return nil, uploadquota.ErrInvalid
	}
	query := "SELECT " + deliveryColumns + " FROM DeliveryLists WHERE OwnerID = ?"
	args := []any{ownerID}
	if beforeToken != "" {
		createdAt, token, err := models.DecodeDeliveryCursor(beforeToken)
		if err != nil {
			return nil, uploadquota.ErrInvalid
		}
		query += " AND (CreatedAt < ? OR (CreatedAt = ? AND Token < ?))"
		args = append(args, createdAt, createdAt, token)
	}
	query += " ORDER BY CreatedAt DESC, Token DESC LIMIT ?"
	args = append(args, limit)
	rows, err := p.sqliteDb.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, deliveryReadError(err)
	}
	defer rows.Close()
	out := make([]models.Delivery, 0, limit)
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// PruneDeliveries deletes only delivery records, never their source files.
// Each indexed candidate query and delete is bounded by the caller's batch size.
func (p DatabaseProvider) PruneDeliveries(ctx context.Context, now int64, limit int) (int, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return 0, err
	}
	if now <= 0 || limit < 1 || limit > 1000 {
		return 0, uploadquota.ErrInvalid
	}
	cutoff := now - models.DeliveryRetentionSeconds
	result, err := p.sqliteDb.ExecContext(ctx, `DELETE FROM DeliveryLists WHERE Token IN (
		SELECT Token FROM (
			SELECT Token FROM (SELECT Token FROM DeliveryLists WHERE ExpiresAt > 0 AND ExpiresAt <= ? ORDER BY ExpiresAt LIMIT ?)
			UNION
			SELECT Token FROM (SELECT Token FROM DeliveryLists WHERE RevokedAt > 0 AND RevokedAt <= ? ORDER BY RevokedAt LIMIT ?)
		) LIMIT ?
	)`, cutoff, limit, cutoff, limit, limit)
	if err != nil {
		return 0, deliveryReadError(err)
	}
	n, err := result.RowsAffected()
	return int(n), err
}
