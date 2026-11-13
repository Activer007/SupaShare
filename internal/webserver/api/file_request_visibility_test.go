//go:build test

package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

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
