//go:build test && !integration

package webserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

func TestN16FilenameRedirectVisibility(t *testing.T) {
	const blob = "n16-filename-route-blob"
	payloadPath := filepath.Join(configuration.Get().DataDir, blob)
	if err := os.WriteFile(payloadPath, []byte("synthetic filename route payload"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(payloadPath) })
	file := models.File{Id: "n16-public-file", Name: "Visible-public-N16.txt", Size: "31 bytes", SHA1: blob, UnlimitedTime: true, UnlimitedDownloads: true}
	database.SaveMetaData(file)
	t.Cleanup(func() { database.DeleteMetaData(file.Id) })
	collected := file
	collected.Id = "n16-collected-file"
	collected.Name = "PRIVATE-N16-COLLECTED-NAME.txt"
	collected.Size = "PRIVATE-N16-SIZE"
	collected.PasswordHash = "PRIVATE-N16-PASSWORD-HASH"
	collected.UploadRequestId = "n16-collection-entry"
	database.SaveMetaData(collected)
	t.Cleanup(func() { database.DeleteMetaData(collected.Id) })
	invitation := models.FileRequest{Id: collected.UploadRequestId, Name: "PRIVATE-N16-REQUEST-NAME", UserId: 5, ApiKey: "PRIVATE-N16-INVITATION"}
	database.SaveFileRequest(invitation)
	t.Cleanup(func() { database.DeleteFileRequest(invitation) })
	call := func(id string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, "/d/"+id+"/arbitrary.txt", nil)
		r.SetPathValue("id", id)
		r.SetPathValue("filename", "arbitrary.txt")
		redirectFromFilename(w, r)
		return w
	}
	normal := call(file.Id)
	if normal.Code != http.StatusOK || !strings.Contains(normal.Body.String(), file.Name) {
		t.Fatalf("normal filename route regressed: status=%d body=%s", normal.Code, normal.Body.String())
	}
	missing := call("n16-does-not-exist")
	if missing.Code != http.StatusTemporaryRedirect {
		t.Fatalf("invalid ID status=%d", missing.Code)
	}
	for _, id := range []string{collected.Id, invitation.Id} {
		got := call(id)
		if got.Code != missing.Code || got.Header().Get("Location") != missing.Header().Get("Location") || got.Body.String() != missing.Body.String() {
			t.Fatalf("request metadata response differs from unknown ID: %s status=%d body=%s", id, got.Code, got.Body.String())
		}
		for _, secret := range []string{collected.Name, collected.Size, collected.PasswordHash, invitation.Name, invitation.ApiKey, "download_password"} {
			if strings.Contains(got.Body.String(), secret) || strings.Contains(got.Header().Get("Location"), secret) {
				t.Fatal("collection metadata disclosed")
			}
		}
	}
}
