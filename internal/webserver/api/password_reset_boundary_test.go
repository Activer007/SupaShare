package api

import (
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/test"
	"net/http"
	"testing"
)

func TestResetRequiredManagementKeysAndPublicCapabilities(t *testing.T) {
	owner := models.User{Id: 9821, Name: "reset-boundary-owner", ResetPassword: true, Permissions: models.UserPermGuestUploads}
	admin := models.User{Id: 9822, Name: "reset-boundary-admin", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermManageApiKeys}
	for _, u := range []models.User{owner, admin} {
		database.SaveUser(u, false)
		t.Cleanup(func() { database.DeleteUser(u.Id) })
	}
	management := models.ApiKey{Id: "reset-boundary-management", PublicId: "reset-boundary-pub", UserId: owner.Id, Permissions: models.ApiPermView}
	invitation := models.ApiKey{Id: "reset-boundary-invitation", PublicId: "reset-invitation-pub", UserId: owner.Id, UploadRequestId: "synthetic-request"}
	adminKey := models.ApiKey{Id: "reset-boundary-admin-key", PublicId: "reset-admin-pub", UserId: admin.Id, Permissions: models.ApiPermApiMod}
	for _, key := range []models.ApiKey{management, invitation, adminKey} {
		database.SaveApiKey(key)
		t.Cleanup(func() { database.DeleteApiKey(key.Id) })
	}
	if _, _, ok := isValidApiKey(management.Id, false, models.ApiPermView); ok {
		t.Fatal("reset management key accepted")
	}
	if _, _, ok := isValidApiKey(invitation.Id, false, models.ApiPermNone); !ok {
		t.Fatal("independent public invitation was revoked")
	}
	w, r := getRecorder("/auth/delete", adminKey.Id, []test.Header{{Name: "targetKey", Value: management.Id}})
	Process(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("administrator cannot revoke confined user's key: %d %s", w.Code, w.Body.String())
	}
	if _, exists := database.GetApiKey(management.Id); exists {
		t.Fatal("key still exists")
	}
}
