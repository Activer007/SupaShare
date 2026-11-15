package sqlite

import (
	"context"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func (p DatabaseProvider) initFileRequestLifecycle() error {
	return p.rawSqlite(`CREATE INDEX IF NOT EXISTS RequestOwnerPage ON UploadRequests(userid, creation DESC, id DESC);
 CREATE INDEX IF NOT EXISTS RequestPage ON UploadRequests(creation DESC, id DESC);
 CREATE INDEX IF NOT EXISTS RequestExpiry ON UploadRequests(expiry) WHERE expiry > 0;
 CREATE INDEX IF NOT EXISTS RequestFiles ON FileMetaData(UploadRequestId);
 CREATE INDEX IF NOT EXISTS RequestKeys ON ApiKeys(UploadRequestId, UserId) WHERE UploadRequestId <> '';
 CREATE INDEX IF NOT EXISTS RequestKeysOwner ON ApiKeys(UserId) WHERE UploadRequestId <> '';`)
}

// Both records and their bearer key are admitted in one transaction. A legacy
// orphan key consumes a slot too: unsuccessful creation cannot grow credentials.
func (p DatabaseProvider) CreateFileRequestLimited(ctx context.Context, request models.FileRequest, key models.ApiKey) (bool, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return false, err
	}
	if !request.ValidLifecycle() || request.UserId <= 0 || request.ApiKey == "" || key.Id != request.ApiKey || key.UploadRequestId != request.Id || key.UserId != request.UserId || key.PublicId == "" || key.IsSystemKey || key.Permissions != models.ApiPermNone || request.Expiry < 0 || request.CreationDate <= 0 {
		return false, uploadquota.ErrInvalid
	}
	tx, err := p.sqliteDb.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT INTO UploadRequests(id,name,userid,expiry,maxFiles,maxSize,creation,apiKey,note,allowedDownloads)
 SELECT ?,?,?,?,?,?,?,?,?,? WHERE
 NOT EXISTS(SELECT 1 FROM UploadRequests WHERE id=? OR apiKey=?) AND
 NOT EXISTS(SELECT 1 FROM ApiKeys WHERE Id=? OR PublicId=?) AND
 (SELECT count(*) FROM UploadRequests WHERE userid=?) < ? AND (SELECT count(*) FROM ApiKeys k WHERE k.UserId=? AND k.UploadRequestId<>'') < ? AND
 (SELECT count(*) FROM UploadRequests) < ? AND (SELECT count(*) FROM ApiKeys k WHERE k.UploadRequestId<>'') < ?`, request.Id, request.Name, request.UserId, request.Expiry, request.MaxFiles, request.MaxSize, request.CreationDate, request.ApiKey, request.Notes, request.AllowedDownloads, request.Id, request.ApiKey, key.Id, key.PublicId, request.UserId, models.MaxFileRequestsPerOwner, request.UserId, models.MaxFileRequestsPerOwner, models.MaxFileRequestsGlobal, models.MaxFileRequestsGlobal)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		var collision int
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM UploadRequests WHERE id=? OR apiKey=?) OR EXISTS(SELECT 1 FROM ApiKeys WHERE Id=? OR PublicId=?)`, request.Id, request.ApiKey, key.Id, key.PublicId).Scan(&collision)
		if err != nil {
			return false, err
		}
		if collision != 0 {
			return false, nil
		}
		return false, models.ErrFileRequestQuota
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO ApiKeys(Id,FriendlyName,LastUsed,Permissions,Expiry,IsSystemKey,UserId,PublicId,UploadRequestId) VALUES(?,?,?,?,?,0,?,?,?)`, key.Id, key.FriendlyName, key.LastUsed, int(key.Permissions), request.Expiry, key.UserId, key.PublicId, key.UploadRequestId)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

const requestColumns = `id,name,userid,expiry,maxFiles,maxSize,creation,apiKey,note,allowedDownloads`

func scanFileRequest(row interface{ Scan(...any) error }) (models.FileRequest, error) {
	var r models.FileRequest
	err := row.Scan(&r.Id, &r.Name, &r.UserId, &r.Expiry, &r.MaxFiles, &r.MaxSize, &r.CreationDate, &r.ApiKey, &r.Notes, &r.AllowedDownloads)
	return r, err
}

func (p DatabaseProvider) ListFileRequestsPage(ctx context.Context, owner int, before string, limit int) ([]models.FileRequest, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return nil, err
	}
	if owner < 0 || limit < 1 || limit > 101 {
		return nil, uploadquota.ErrInvalid
	}
	query := "SELECT " + requestColumns + " FROM UploadRequests WHERE 1=1"
	args := []any{}
	if owner != 0 {
		query += " AND userid=?"
		args = append(args, owner)
	}
	if before != "" {
		date, id, err := models.ParseFileRequestCursor(before)
		if err != nil {
			return nil, uploadquota.ErrInvalid
		}
		query += " AND (creation < ? OR (creation = ? AND id < ?))"
		args = append(args, date, date, id)
	}
	query += " ORDER BY creation DESC,id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := p.sqliteDb.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.FileRequest, 0)
	for rows.Next() {
		r, err := scanFileRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p DatabaseProvider) ReadFileRequestFiles(ctx context.Context, requestID string) (map[string]models.File, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return nil, err
	}
	if requestID == "" {
		return nil, uploadquota.ErrInvalid
	}
	rows, err := p.sqliteDb.QueryContext(ctx, "SELECT * FROM FileMetaData WHERE UploadRequestId=?", requestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]models.File)
	for rows.Next() {
		var r schemaMetaData
		err = rows.Scan(&r.Id, &r.Name, &r.Size, &r.SHA1, &r.ExpireAt, &r.SizeBytes, &r.DownloadsRemaining, &r.DownloadCount, &r.PasswordHash, &r.HotlinkId, &r.ContentType, &r.AwsBucket, &r.Encryption, &r.UnlimitedDownloads, &r.UnlimitedTime, &r.UserId, &r.UploadDate, &r.PendingDeletion, &r.UploadRequestId)
		if err != nil {
			return nil, err
		}
		f, err := r.ToFileModel()
		if err != nil {
			return nil, err
		}
		out[f.Id] = f
	}
	return out, rows.Err()
}

// Conditional update cannot resurrect a deleted request or overwrite a newer
// policy. A missing/retired credential is never silently re-issued.
func (p DatabaseProvider) UpdateFileRequest(ctx context.Context, old, r models.FileRequest) (bool, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return false, err
	}
	if r.Expiry != old.Expiry && old.Expiry > 0 && old.Expiry <= time.Now().Unix() {
		return false, nil
	}
	if !r.ValidLifecycle() || old.Id == "" || r.Id != old.Id || r.UserId != old.UserId || r.ApiKey != old.ApiKey || r.CreationDate != old.CreationDate || r.Expiry < 0 {
		return false, uploadquota.ErrInvalid
	}
	tx, err := p.sqliteDb.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE UploadRequests SET name=?,expiry=?,maxFiles=?,maxSize=?,note=?,allowedDownloads=? WHERE id=? AND name=? AND userid=? AND expiry=? AND maxFiles=? AND maxSize=? AND creation=? AND apiKey=? AND note=? AND allowedDownloads=? AND (?=expiry OR EXISTS(SELECT 1 FROM ApiKeys k WHERE k.Id=UploadRequests.apiKey AND k.UploadRequestId=UploadRequests.id AND k.UserId=UploadRequests.userid))`, r.Name, r.Expiry, r.MaxFiles, r.MaxSize, r.Notes, r.AllowedDownloads, old.Id, old.Name, old.UserId, old.Expiry, old.MaxFiles, old.MaxSize, old.CreationDate, old.ApiKey, old.Notes, old.AllowedDownloads, r.Expiry)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	_, err = tx.ExecContext(ctx, "UPDATE ApiKeys SET Expiry=? WHERE Id=? AND UploadRequestId=? AND UserId=?", r.Expiry, r.ApiKey, r.Id, r.UserId)
	if err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Revoke expired invitation keys promptly. Retain non-empty requests so files
// remain manageable; only empty requests age out after the retention window.
func (p DatabaseProvider) PruneFileRequests(ctx context.Context, now int64, limit int) (int, error) {
	if err := p.checkDeliveryContext(ctx); err != nil {
		return 0, err
	}
	if now <= 0 || limit < 1 || limit > 1000 {
		return 0, uploadquota.ErrInvalid
	}
	tx, err := p.sqliteDb.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `DELETE FROM ApiKeys WHERE Id IN (SELECT k.Id FROM ApiKeys k LEFT JOIN UploadRequests r ON r.apiKey=k.Id AND r.id=k.UploadRequestId AND r.userid=k.UserId WHERE k.UploadRequestId<>'' AND (r.id IS NULL OR (r.expiry>0 AND r.expiry<=?)) ORDER BY k.Id LIMIT ?)`, now, limit)
	if err != nil {
		return 0, err
	}
	ledger, err := readUploadQuota(ctx, tx)
	if err != nil {
		return 0, err
	}
	query := `DELETE FROM UploadRequests WHERE id IN (SELECT r.id FROM UploadRequests r WHERE r.expiry>0 AND r.expiry<=? AND NOT EXISTS(SELECT 1 FROM FileMetaData f WHERE f.UploadRequestId=r.id)`
	args := []any{now - models.FileRequestRetentionSeconds}
	active := map[string]bool{}
	for _, reservation := range ledger.Records {
		if reservation.RequestID != "" && (reservation.State == uploadquota.Reserved || reservation.State == uploadquota.Uploading) {
			active[reservation.RequestID] = true
		}
	}
	if len(active) > 0 {
		query += " AND r.id NOT IN (" + strings.TrimSuffix(strings.Repeat("?,", len(active)), ",") + ")"
		for id := range active {
			args = append(args, id)
		}
	}
	query += " ORDER BY r.expiry,r.id LIMIT ?)"
	args = append(args, limit)
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}
