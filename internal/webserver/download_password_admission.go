package webserver

import (
	"bytes"
	"net/http"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
)

// Password verification is intentionally outside the metadata gate. Issuance
// must share the edit/delete gate, however, so an old verification cannot mint
// a new token after a password change has already revoked the previous tokens.
func writeVerifiedFilePwCookie(w http.ResponseWriter, r *http.Request, verified models.File) bool {
	unlock, err := apimutex.LockManyContext(r.Context(), apimutex.TypeMetaData, verified.Id)
	if err != nil {
		return false
	}
	defer unlock()
	current, ok := database.GetMetaDataById(verified.Id)
	if !ok || current.IsFileRequest() || current.IsPendingForDeletion() ||
		storage.IsExpiredFile(current, time.Now().Unix()) || current.PasswordHash == "" ||
		current.PasswordHash != verified.PasswordHash || current.UserId != verified.UserId ||
		current.UploadRequestId != verified.UploadRequestId || current.SHA1 != verified.SHA1 ||
		current.AwsBucket != verified.AwsBucket || current.SizeBytes != verified.SizeBytes ||
		current.UploadDate != verified.UploadDate ||
		current.Encryption.IsEncrypted != verified.Encryption.IsEncrypted ||
		current.Encryption.IsEndToEndEncrypted != verified.Encryption.IsEndToEndEncrypted ||
		!bytes.Equal(current.Encryption.DecryptionKey, verified.Encryption.DecryptionKey) ||
		!bytes.Equal(current.Encryption.Nonce, verified.Encryption.Nonce) {
		return false
	}
	writeFilePwCookie(w, current)
	return true
}
