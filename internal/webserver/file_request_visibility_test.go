//go:build test && !integration

package webserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/sessionmanager"
)

func TestFileRequestInvitationHTML(t *testing.T) {
	request := models.FileRequest{Id: "synthetic-request", UserId: 9708, Name: "Visible collection", ApiKey: "synthetic-private-invitation"}
	owner := models.User{Id: request.UserId, Name: "synthetic-owner", UserLevel: models.UserLevelUser, Permissions: models.UserPermGuestUploads}
	database.SaveUser(owner, false)
	t.Cleanup(func() { database.DeleteUser(owner.Id) })
	database.SaveFileRequest(request)
	t.Cleanup(func() { database.DeleteFileRequest(request) })
	server := httptest.NewServer(requireLogin(showUploadRequest, true, false))
	t.Cleanup(server.Close)
	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	for index, tc := range []struct {
		name        string
		level       models.UserRank
		permissions models.UserPermission
		visible     bool
	}{
		{"owner", models.UserLevelUser, models.UserPermGuestUploads, true},
		{"reader", models.UserLevelUser, models.UserPermGuestUploads | models.UserPermListOtherUploads, true},
		{"editor", models.UserLevelUser, models.UserPermGuestUploads | models.UserPermListOtherUploads | models.UserPermEditOtherUploads, true},
		{"cross-owner-admin", models.UserLevelAdmin, models.UserPermissionAll, true},
		{"unrelated", models.UserLevelUser, models.UserPermGuestUploads, false},
		{"without-guest-grant", models.UserLevelAdmin, models.UserPermListOtherUploads | models.UserPermEditOtherUploads, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := models.User{Id: request.UserId + index, Name: tc.name, UserLevel: tc.level, Permissions: tc.permissions}
			database.SaveUser(user, false)
			if index != 0 {
				t.Cleanup(func() { database.DeleteUser(user.Id) })
			}
			cookieRecorder := httptest.NewRecorder()
			sessionmanager.CreateSession(cookieRecorder, false, 0, user.Id)
			cookie := cookieRecorder.Result().Cookies()[0]
			t.Cleanup(func() { database.DeleteSession(cookie.Value) })
			req, err := http.NewRequest(http.MethodGet, server.URL+"/filerequests", nil)
			if err != nil {
				t.Fatal(err)
			}
			req.AddCookie(cookie)
			response, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			status := http.StatusOK
			if !user.HasPermissionCreateFileRequests() {
				status = http.StatusTemporaryRedirect
			}
			if response.StatusCode != status {
				t.Fatalf("status=%d body=%s", response.StatusCode, body)
			}
			for _, marker := range []string{request.ApiKey, `id="copy-synthetic-request"`, `id="collection-name-synthetic-request" href=`} {
				if strings.Contains(string(body), marker) != (index == 0) {
					t.Fatalf("invitation markup boundary failed for %s", marker)
				}
			}
			if strings.Contains(string(body), request.Name) != tc.visible {
				t.Fatal("metadata visibility mismatch")
			}
			stored, ok := database.GetFileRequest(request.Id)
			if !ok || stored.ApiKey != request.ApiKey {
				t.Fatal("HTML request changed stored invitation")
			}
		})
	}
}
