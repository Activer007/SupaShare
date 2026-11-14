//go:build test

package storage

import (
	"context"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/downloadstatus"
	"testing"
	"time"
)

func TestN16PublicAdmissionRejectsStaleSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*models.File)
	}{
		{"password", func(f *models.File) { f.PasswordHash = "new-password-hash" }},
		{"pending", func(f *models.File) { f.PendingDeletion = time.Now().Unix() + 60 }},
		{"expiry", func(f *models.File) { f.UnlimitedTime = false; f.ExpireAt = time.Now().Unix() - 1 }},
		{"locator", func(f *models.File) { f.SHA1 = "new-blob" }},
		{"owner", func(f *models.File) { f.UserId++ }},
		{"encryption", func(f *models.File) { f.Encryption.IsEncrypted = true }},
		{"hotlink", func(f *models.File) { f.HotlinkId = "new-hotlink" }},
		{"request", func(f *models.File) { f.UploadRequestId = "request" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old := models.File{Id: "n16-stale-" + tc.name, SHA1: "blob", UserId: 1, UnlimitedTime: true, DownloadsRemaining: 1}
			current := old
			tc.change(&current)
			database.SaveMetaData(current)
			defer database.DeleteMetaData(old.Id)
			if _, guard, ok := admitPublicDownload(context.Background(), old, true, true, true, ""); ok || guard != "" {
				t.Fatal("stale snapshot admitted")
			}
			got, _ := database.GetMetaDataById(old.Id)
			if got.DownloadCount != 0 || got.DownloadsRemaining != 1 || downloadstatus.IsCurrentlyDownloading(old) {
				t.Fatal("rejection charged or registered")
			}
		})
	}
	old := models.File{Id: "n16-deleted", SHA1: "blob", UnlimitedTime: true, DownloadsRemaining: 1}
	database.SaveMetaData(old)
	database.DeleteMetaData(old.Id)
	if _, _, ok := admitPublicDownload(context.Background(), old, true, true, true, ""); ok {
		t.Fatal("deleted row admitted")
	}
}

func TestN16PublicAdmissionFinalAllowanceAndCurrentFields(t *testing.T) {
	old := models.File{Id: "n16-final", SHA1: "blob", UserId: 1, Name: "old", UnlimitedTime: true, DownloadsRemaining: 1}
	current := old
	current.Name = "new"
	database.SaveMetaData(current)
	defer database.DeleteMetaData(old.Id)
	file, guard, ok := admitPublicDownload(context.Background(), old, true, true, true, "")
	if !ok {
		t.Fatal("valid admission rejected")
	}
	defer downloadstatus.SetComplete(guard)
	if file.Name != "new" || file.DownloadsRemaining != 0 || file.DownloadCount != 1 || !downloadstatus.IsCurrentlyDownloading(old) {
		t.Fatal("not current or not charged/registered")
	}
	if _, _, ok := admitPublicDownload(context.Background(), old, true, true, true, ""); ok {
		t.Fatal("final allowance reused")
	}
}

func TestN16PublicAdmissionHotlinkAndCancellation(t *testing.T) {
	f := models.File{Id: "n16-hotlink", SHA1: "blob", UserId: 1, Name: "image.png", ContentType: "image/png", HotlinkId: "n16-link", UnlimitedTime: true, UnlimitedDownloads: true}
	database.SaveMetaData(f)
	defer database.DeleteMetaData(f.Id)
	if _, _, ok := admitPublicDownload(context.Background(), f, true, true, true, f.HotlinkId); ok {
		t.Fatal("missing hotlink mapping admitted")
	}
	database.SaveHotlink(f)
	defer database.DeleteHotlink(f.HotlinkId)
	_, guard, ok := admitPublicDownload(context.Background(), f, true, true, true, f.HotlinkId)
	if !ok {
		t.Fatal("valid hotlink rejected")
	}
	downloadstatus.SetComplete(guard)
	database.SaveHotlink(models.File{Id: "other-file", HotlinkId: f.HotlinkId})
	if _, _, ok := admitPublicDownload(context.Background(), f, true, true, true, f.HotlinkId); ok {
		t.Fatal("remapped hotlink admitted")
	}
	database.SaveHotlink(f)
	f.ContentType = "image/svg+xml"
	database.SaveMetaData(f)
	if _, _, ok := admitPublicDownload(context.Background(), f, true, true, true, f.HotlinkId); ok {
		t.Fatal("ineligible hotlink admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, ok := admitPublicDownload(ctx, f, true, true, true, ""); ok {
		t.Fatal("cancelled admission accepted")
	}
}
