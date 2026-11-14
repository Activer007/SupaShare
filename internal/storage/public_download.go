package storage

import (
	"context"
	"net/http"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
	"github.com/Activer007/SupaShare/internal/webserver/sse"
)

// ServePublicFile requires the caller to have verified any password cookie
// against snapshot.PasswordHash. Admission rejects a subsequent hash change.
// hotlinkID is empty for ordinary public downloads, otherwise it is the exact
// requested hotlink, not a value inferred from an earlier file snapshot.
func ServePublicFile(snapshot models.File, w http.ResponseWriter, r *http.Request, forceDownload bool, hotlinkID string) bool {
	file, guard, ok := admitPublicDownload(r.Context(), snapshot, true, true, true, hotlinkID)
	if !ok {
		return false
	}
	defer downloadstatus.SetComplete(guard)
	return serveAdmittedFile(file, w, r, forceDownload, false)
}

func admitPublicDownload(ctx context.Context, snapshot models.File, count, recheckExpiry, public bool, hotlinkID string) (models.File, string, bool) {
	if snapshot.Id == "" {
		return models.File{}, "", false
	}
	unlock, err := apimutex.LockManyContext(ctx, apimutex.TypeMetaData, snapshot.Id)
	if err != nil {
		return models.File{}, "", false
	}
	defer unlock()
	file, found := database.GetMetaDataById(snapshot.Id)
	if !found || !sameDownloadFile(snapshot, file) || file.IsPendingForDeletion() ||
		(public && file.IsFileRequest()) ||
		(recheckExpiry && IsExpiredFile(file, time.Now().Unix())) ||
		snapshot.PasswordHash != file.PasswordHash || snapshot.HotlinkId != file.HotlinkId ||
		!checkIfValidAws(file) || ctx.Err() != nil {
		return models.File{}, "", false
	}
	if hotlinkID != "" {
		target, exists := database.GetHotlink(hotlinkID)
		if !exists || target != file.Id || file.HotlinkId != hotlinkID || !IsAbleHotlink(file) {
			return models.File{}, "", false
		}
	}
	// Register while holding the same metadata lock as deletion/replacement,
	// before charging the final allowance. Already admitted streams may finish.
	guard := downloadstatus.SetDownload(file)
	if count {
		database.IncreaseDownloadCount(file.Id, !file.UnlimitedDownloads)
		file.DownloadCount++
		if !file.UnlimitedDownloads {
			file.DownloadsRemaining--
		}
		go sse.PublishDownloadCount(file)
	}
	return file, guard, true
}
