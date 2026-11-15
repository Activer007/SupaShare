package sqlite

import (
	"context"
	"database/sql"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

// ClaimHotlink accepts an idempotent same-target claim but never retargets a link.
func (p DatabaseProvider) ClaimHotlink(file models.File) error {
	if file.HotlinkId == "" || file.Id == "" {
		return uploadquota.ErrInvalid
	}
	return p.withUploadQuotaTransaction(context.Background(), func(conn *sql.Conn) error {
		return saveQuotaHotlink(context.Background(), conn, file)
	})
}

// CreateFileMetadata must only be used for creation, never edits or migration.
func (p DatabaseProvider) CreateFileMetadata(file models.File) error {
	if file.Id == "" {
		return uploadquota.ErrInvalid
	}
	return p.withUploadQuotaTransaction(context.Background(), func(conn *sql.Conn) error {
		var count int
		if err := conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM FileMetaData WHERE Id = ?", file.Id).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return uploadquota.ErrConflict
		}
		if err := saveQuotaHotlink(context.Background(), conn, file); err != nil {
			return err
		}
		return writeMetaData(context.Background(), conn, file, true)
	})
}
