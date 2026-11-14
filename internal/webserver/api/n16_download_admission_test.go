//go:build test

package api

import (
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/test/securityfixture"
	"net/http/httptest"
	"testing"
)

func TestN16DirectDownloadCredentialReload(t *testing.T) {
	for _, collection := range []bool{false, true} {
		s := securityfixture.New(t, "")
		user := s.Users["owner"]
		key := s.Keys["owner"]
		file := s.Files["own"]
		if collection {
			file.UploadRequestId = s.Request.Id
		}
		database.SaveUser(user, false)
		database.SaveApiKey(key)
		database.SaveMetaData(file)
		old := configuration.Get().DataDir
		configuration.Get().DataDir = s.DataDir
		database.UpdateApiKeyPermission(key.Id, models.ApiPermDownload, false)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/files/download", nil)
		if collection {
			serveCollectionDownload(w, r, []models.File{file}, "", user, key, false)
		} else {
			serveOrdinaryDownload(w, r, []models.File{file}, "", user, key, false, true)
		}
		configuration.Get().DataDir = old
		if w.Code != 404 {
			t.Fatalf("stale direct key accepted collection=%v status=%d", collection, w.Code)
		}
		got, _ := database.GetMetaDataById(file.Id)
		if got.DownloadCount != file.DownloadCount {
			t.Fatal("denied direct download charged")
		}
	}
}
