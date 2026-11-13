//go:build test

package api

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/presign"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling/errorcodes"
)

func TestPresignAPIAdmissionBackpressure(t *testing.T) {
	owner := models.User{Id: 887101, Name: "presign-bounds@example.invalid", UserLevel: models.UserLevelUser}
	key := models.ApiKey{Id: rand.Text(), UserId: owner.Id, Permissions: models.ApiPermDownload}
	file := models.File{Id: rand.Text(), Name: "fixture.txt", SHA1: rand.Text(), UserId: owner.Id,
		SizeBytes: 3, UnlimitedTime: true, UnlimitedDownloads: true}
	path := filepath.Join(configuration.Get().DataDir, file.SHA1)
	if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
		t.Fatal(err)
	}
	database.SaveUser(owner, false)
	database.SaveApiKey(key)
	database.SaveMetaData(file)
	t.Cleanup(func() {
		database.DeleteMetaData(file.Id)
		database.DeleteApiKey(key.Id)
		database.DeleteUser(owner.Id)
		os.Remove(path)
	})
	request := func(archive bool, filename string) *httptest.ResponseRecorder {
		t.Helper()
		endpoint := "/api/files/download/" + file.Id
		if archive {
			endpoint = "/api/files/downloadzip"
		}
		r := httptest.NewRequest(http.MethodPost, endpoint, nil)
		r.Header.Set("apikey", key.Id)
		r.Header.Set("presignUrl", "true")
		r.Header.Set("ids", file.Id)
		r.Header.Set("filename", filename)
		w := httptest.NewRecorder()
		Process(w, r)
		return w
	}
	assertRejected := func(w *httptest.ResponseRecorder, status, code int) {
		t.Helper()
		var result struct {
			ErrorCode   int
			DownloadURL string `json:"downloadUrl"`
		}
		if w.Code != status || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.ErrorCode != code || result.DownloadURL != "" {
			t.Fatalf("admission status=%d body=%s, expected status=%d code=%d without a grant", w.Code, w.Body.String(), status, code)
		}
	}
	assertRejected(request(true, strings.Repeat("x", 4097)), http.StatusBadRequest, errorcodes.InvalidUserInput)
	var pending []string
	for n := 0; n < 64; n++ {
		id := rand.Text()
		if err := presign.Save(models.Presign{Id: id, Expiry: time.Now().Add(time.Minute).Unix(),
			FileIds: []string{file.Id}, UserId: owner.Id, ApiKeyId: key.Id}); err != nil {
			t.Fatalf("failed admission retained a hidden grant or fixture capacity failed: %v", err)
		}
		pending = append(pending, id)
		t.Cleanup(func() { presign.Delete(id) })
	}
	busy := request(false, "")
	assertRejected(busy, http.StatusServiceUnavailable, errorcodes.InternalServer)
	if busy.Header().Get("Retry-After") != "1" {
		t.Fatal("capacity failure omitted retry guidance")
	}
	for _, id := range pending {
		if _, ok := presign.Get(id); !ok {
			t.Fatal("rejected admission consumed an existing grant")
		}
	}
	presign.Delete(pending[0])
	w := request(true, "ordinary.zip")
	var result struct {
		DownloadURL string `json:"downloadUrl"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil {
		t.Fatalf("released capacity did not admit an ordinary archive: status=%d body=%s", w.Code, w.Body.String())
	}
	u, err := url.Parse(result.DownloadURL)
	if err != nil || u.Query().Get("key") == "" {
		t.Fatal("ordinary response omitted its capability", err)
	}
	id := u.Query().Get("key")
	t.Cleanup(func() { presign.Delete(id) })
	grant, ok := presign.Get(id)
	if !ok || grant.UserId != owner.Id || grant.ApiKeyId != key.Id || !grant.IsArchive() ||
		grant.Filename != "ordinary.zip" || !reflect.DeepEqual(grant.FileIds, []string{file.Id}) ||
		grant.Expiry < time.Now().Add(28*time.Second).Unix() || grant.Expiry > time.Now().Add(30*time.Second).Unix() {
		t.Fatal("ordinary archive lost its issuer, expiry or metadata", grant)
	}
}
