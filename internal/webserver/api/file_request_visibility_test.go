//go:build test

package api

import (
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

// Exercise the public HTTP entry point, including live user/key grants and
// parameter parsing, using only disposable users and credentials.
func TestFileRequestInvitationHTTPAuthorization(t *testing.T) {
	const ownerID = 9800
	request := models.FileRequest{
		Id: "synthetic-http-invitation", UserId: ownerID,
		ApiKey: rand.Text(), Name: "HTTP collection metadata",
	}
	database.SaveFileRequest(request)
	t.Cleanup(func() { database.DeleteFileRequest(request) })
	invitation := models.ApiKey{Id: request.ApiKey, UserId: ownerID, UploadRequestId: request.Id}
	database.SaveApiKey(invitation)
	t.Cleanup(func() { database.DeleteApiKey(invitation.Id) })
	server := httptest.NewServer(http.HandlerFunc(Process))
	t.Cleanup(server.Close)

	for index, tc := range []struct {
		name        string
		level       models.UserRank
		permissions models.UserPermission
		keyGrant    models.ApiPermission
		owner       bool
		read, edit  bool
		gate        bool
	}{
		{"owner", models.UserLevelUser, models.UserPermGuestUploads, models.ApiPermManageFileRequests, true, true, true, true},
		{"reader", models.UserLevelUser, models.UserPermGuestUploads | models.UserPermListOtherUploads, models.ApiPermManageFileRequests, false, true, false, true},
		{"editor", models.UserLevelUser, models.UserPermGuestUploads | models.UserPermListOtherUploads | models.UserPermEditOtherUploads, models.ApiPermManageFileRequests, false, true, true, true},
		{"cross-owner-admin", models.UserLevelAdmin, models.UserPermissionAll, models.ApiPermManageFileRequests, false, true, true, true},
		{"unrelated", models.UserLevelUser, models.UserPermGuestUploads, models.ApiPermManageFileRequests, false, false, false, true},
		{"owner-without-key-grant", models.UserLevelUser, models.UserPermGuestUploads, models.ApiPermNone, true, false, false, false},
		{"owner-without-user-grant", models.UserLevelUser, models.UserPermissionNone, models.ApiPermManageFileRequests, true, false, false, false},
		{"admin-without-user-grant", models.UserLevelAdmin, models.UserPermListOtherUploads | models.UserPermEditOtherUploads, models.ApiPermManageFileRequests, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			userID := ownerID + index + 1
			if tc.owner {
				userID = ownerID
			}
			user := models.User{Id: userID, Name: tc.name, UserLevel: tc.level, Permissions: tc.permissions}
			database.SaveUser(user, false)
			t.Cleanup(func() { database.DeleteUser(user.Id) })
			key := models.ApiKey{Id: rand.Text(), PublicId: rand.Text(), UserId: user.Id, Permissions: tc.keyGrant}
			database.SaveApiKey(key)
			t.Cleanup(func() { database.DeleteApiKey(key.Id) })

			for _, endpoint := range []struct {
				path    string
				allowed bool
				visible bool
			}{
				{"/list", tc.gate, tc.read},
				{"/list/" + request.Id, tc.read, tc.read},
				{"/save", tc.edit, tc.edit},
			} {
				database.SaveFileRequest(request)
				req, err := http.NewRequest(http.MethodPost, server.URL+"/api/uploadrequest"+endpoint.path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("apikey", key.Id)
				req.Header.Set("id", request.Id)
				req.Header.Set("notes", "edited through HTTP")
				response, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				status := http.StatusUnauthorized
				if endpoint.allowed {
					status = http.StatusOK
				}
				if response.StatusCode != status || !json.Valid(body) {
					t.Fatalf("%s: status=%d body=%s", endpoint.path, response.StatusCode, body)
				}
				if strings.Contains(string(body), request.ApiKey) != (tc.owner && endpoint.allowed) {
					t.Fatalf("%s: invitation visibility mismatch", endpoint.path)
				}
				if strings.Contains(string(body), request.Name) != endpoint.visible {
					t.Fatalf("%s: metadata visibility mismatch", endpoint.path)
				}
				stored, ok := database.GetFileRequest(request.Id)
				if !ok || stored.ApiKey != request.ApiKey || stored.UserId != ownerID {
					t.Fatalf("%s: stored invitation or owner changed", endpoint.path)
				}
				if endpoint.path == "/save" && (stored.Notes == "edited through HTTP") != tc.edit {
					t.Fatal("edit authorization did not match persisted metadata")
				}
				storedKey, ok := database.GetApiKey(invitation.Id)
				if !ok || storedKey != invitation {
					t.Fatal("management request changed the public invitation credential")
				}
			}
		})
	}
}

func TestFileRequestInvitationVisibility(t *testing.T) {
	request := models.FileRequest{Id: "synthetic-visibility", UserId: idUser, ApiKey: "synthetic-invitation-not-a-real-key", Name: "visible metadata"}
	database.SaveFileRequest(request)
	t.Cleanup(func() { database.DeleteFileRequest(request) })
	for _, tc := range []struct {
		name            string
		id              int
		level           models.UserRank
		permissions     models.UserPermission
		visible, secret bool
	}{
		{"owner", idUser, models.UserLevelUser, 0, true, true},
		{"reader", 500, models.UserLevelUser, models.UserPermListOtherUploads, true, false},
		{"editor", 501, models.UserLevelUser, models.UserPermListOtherUploads | models.UserPermEditOtherUploads, true, false},
		{"admin", 502, models.UserLevelAdmin, models.UserPermissionAll, true, false},
		{"unrelated", 503, models.UserLevelUser, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := models.User{Id: tc.id, UserLevel: tc.level, Permissions: tc.permissions}
			for _, single := range []bool{false, true} {
				w := httptest.NewRecorder()
				if single {
					apiUploadRequestListSingle(w, &paramURequestListSingle{Id: request.Id}, user, models.ApiKey{})
				} else {
					apiUploadRequestList(w, nil, user, models.ApiKey{})
				}
				body := w.Body.String()
				if strings.Contains(body, request.ApiKey) != tc.secret {
					t.Fatalf("single=%v invitation boundary failed", single)
				}
				if strings.Contains(body, request.Name) != tc.visible {
					t.Fatalf("single=%v metadata visibility failed", single)
				}
				if !json.Valid(w.Body.Bytes()) {
					t.Fatal("invalid JSON")
				}
			}
		})
	}

	// Editing metadata is not an invitation-sharing grant either.
	editor := models.User{Id: 501, UserLevel: models.UserLevelUser, Permissions: models.UserPermEditOtherUploads}
	w := httptest.NewRecorder()
	apiURequestSave(w, &paramURequestSave{Id: request.Id, Name: "edited metadata", IsNameSet: true}, editor, models.ApiKey{})
	if w.Code != 200 || strings.Contains(w.Body.String(), request.ApiKey) {
		t.Fatalf("edit exposed invitation or failed: status %d", w.Code)
	}
	stored, _ := database.GetFileRequest(request.Id)
	if stored.ApiKey != request.ApiKey {
		t.Fatal("read changed stored invitation")
	}
}
