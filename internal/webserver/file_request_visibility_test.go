//go:build test && !integration

package webserver

import (
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestFileRequestInvitationHTML(t *testing.T) {
	request := models.FileRequest{Id: "synthetic-request", UserId: 9708, Name: "Visible collection", ApiKey: "synthetic-private-invitation"}
	for _, id := range []int{9708, 9709} {
		user := models.User{Id: id, Name: "synthetic-visibility-" + strconv.Itoa(id), UserLevel: models.UserLevelAdmin, Permissions: models.UserPermissionAll}

		database.SaveUser(user, false)
		t.Cleanup(func() { database.DeleteUser(user.Id) })
		database.SaveFileRequest(request)
		t.Cleanup(func() { database.DeleteFileRequest(request) })
		view := (&AdminView{}).convertGlobalConfig(ViewFileRequests, user)
		view.ServerUrl = "https://example.invalid/"

		w := httptest.NewRecorder()
		if err := renderPage(w, httptest.NewRequest("GET", "/uploadrequest", nil), "uploadreq", view); err != nil {
			t.Fatal(err)
		}
		body := w.Body.String()
		for _, s := range []string{request.ApiKey, `id="copy-synthetic-request"`, `href="https://example.invalid/r/`} {
			if strings.Contains(body, s) != (id == 9708) {
				t.Fatalf("viewer %d: invitation markup boundary failed for %s", id, s)
			}
		}
		if !strings.Contains(body, request.Name) {
			t.Fatal("lost metadata")
		}
	}
}
