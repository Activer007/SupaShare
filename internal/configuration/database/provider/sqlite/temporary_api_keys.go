package sqlite

import "github.com/Activer007/SupaShare/internal/models"

// Cleanup takes the SQLite write lock first; quota checking and insertion remain
// in the same transaction, including across separate server connections. Prune a
// bounded batch, then conservatively count any remaining legacy expired rows.
func (p DatabaseProvider) CreateTemporaryApiKey(key models.ApiKey) (bool, error) {
	now := currentTime().Unix()
	if err := models.ValidateTemporaryApiKey(key, now); err != nil {
		return false, err
	}
	tx, err := p.sqliteDb.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`DELETE FROM ApiKeys WHERE Id IN (SELECT Id FROM ApiKeys WHERE IsSystemKey=1 AND COALESCE(UploadRequestId,'')='' AND Expiry>0 AND Expiry<=? LIMIT 256)`, now)
	if err != nil {
		return false, err
	}
	result, err := tx.Exec(`INSERT INTO ApiKeys
 (Id,FriendlyName,LastUsed,Permissions,Expiry,IsSystemKey,UserId,PublicId,UploadRequestId)
 SELECT ?,?,?,?,?,1,?,? ,'' WHERE
 (SELECT COUNT(*) FROM ApiKeys WHERE IsSystemKey=1 AND COALESCE(UploadRequestId,'')='' AND UserId=?) < ? AND
 (SELECT COUNT(*) FROM ApiKeys WHERE IsSystemKey=1 AND COALESCE(UploadRequestId,'')='') < ?
 ON CONFLICT(Id) DO NOTHING`, key.Id, key.FriendlyName, key.LastUsed, key.Permissions, key.Expiry, key.UserId, key.PublicId, key.UserId, models.TemporaryApiKeyMaxPerUser, models.TemporaryApiKeyMaxGlobal)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	var collision int
	if n == 0 {
		if err = tx.QueryRow("SELECT COUNT(*) FROM ApiKeys WHERE Id=?", key.Id).Scan(&collision); err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	if n == 0 && collision == 0 {
		return false, models.ErrTemporaryApiKeyQuotaExceeded
	}
	return n == 1, nil
}
