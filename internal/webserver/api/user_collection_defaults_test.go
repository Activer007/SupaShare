//go:build test

package api

import (
	"crypto/rand"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/test"
)

func TestNewUserCollectionPermission(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		configured bool
		permission models.UserPermission
	}{
		{name: "default", permission: models.UserPermGuestUploads},
		{name: "explicit-false", configured: true, permission: models.UserPermissionNone},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			originalConfig := *configuration.Get()
			// Setenv restores the process environment before this cleanup reloads
			// its cached configuration, without reconnecting the test database.
			t.Cleanup(func() {
				configuration.Load()
				*configuration.Get() = originalConfig
			})
			t.Setenv("GOKAPI_GUEST_UPLOAD_BY_DEFAULT", "false")
			if !scenario.configured {
				if err := os.Unsetenv("GOKAPI_GUEST_UPLOAD_BY_DEFAULT"); err != nil {
					t.Fatal(err)
				}
			}
			configuration.Load()
			configuration.Get().Authentication.Method = models.AuthenticationInternal
			adminKey := onboardingAdminKey(t)
			createUser := func() models.User {
				t.Helper()
				name := "collection-default-" + strings.ToLower(rand.Text())
				w, r := getRecorder("/user/create", adminKey.Id, []test.Header{{Name: "username", Value: name}})
				Process(w, r)
				var response onboardingCreateResponse
				if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &response) != nil || response.Id == 0 {
					t.Fatalf("create user failed: status=%d", w.Code)
				}
				t.Cleanup(func() {
					for _, request := range database.GetAllFileRequests() {
						if request.UserId == response.Id {
							database.DeleteFileRequest(request)
						}
					}
					for id, key := range database.GetAllApiKeys() {
						if key.UserId == response.Id {
							database.DeleteApiKey(id)
						}
					}
					database.DeleteUser(response.Id)
				})
				user, ok := database.GetUser(response.Id)
				if !ok || user.UserLevel != models.UserLevelUser || user.Permissions != scenario.permission || response.UserLevel != user.UserLevel || response.Permissions != user.Permissions {
					t.Fatalf("new user must have only its default collection permission: found=%t rank=%d permissions=%d", ok, user.UserLevel, user.Permissions)
				}
				// This permission test starts after mandatory onboarding completes.
				user.ResetPassword = false
				database.SaveUser(user, false)
				return user
			}
			collectionKey := func(user models.User) models.ApiKey {
				t.Helper()
				key := generateNewKey(false, user.Id, "new-user collection test", "")
				key.Permissions = models.ApiPermManageFileRequests
				database.SaveApiKey(key)
				return key
			}
			owner := createUser()
			ownerKey := collectionKey(owner)
			createHeaders := []test.Header{{Name: "maxfiles", Value: "1"}, {Name: "maxsize", Value: "1"}}
			beforeRequests := len(database.GetAllFileRequests())
			beforeKeys := len(database.GetAllApiKeys())
			w, r := getRecorderWithBody("/uploadrequest/save", ownerKey.Id, http.MethodPost, createHeaders, nil)
			Process(w, r)
			if scenario.configured {
				if w.Code != http.StatusUnauthorized || len(database.GetAllFileRequests()) != beforeRequests || len(database.GetAllApiKeys()) != beforeKeys {
					t.Fatalf("explicit false must deny collection creation without saving a request or credential: status=%d", w.Code)
				}
				return
			}
			var collection models.FileRequest
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &collection) != nil || collection.Id == "" || collection.ApiKey == "" {
				t.Fatalf("new ordinary user could not create a collection: status=%d body=%s", w.Code, w.Body.String())
			}
			stored, ok := database.GetFileRequest(collection.Id)
			if !ok || stored.UserId != owner.Id || collection.UserId != owner.Id {
				t.Fatal("collection was not saved for its ordinary-user owner")
			}

			other := createUser()
			otherKey := collectionKey(other)
			w, r = getRecorder("/uploadrequest/list/"+collection.Id, otherKey.Id, nil)
			Process(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("another ordinary user could read the collection: status=%d", w.Code)
			}
			w, r = getRecorder("/uploadrequest/list", otherKey.Id, nil)
			Process(w, r)
			var visible []models.FileRequest
			if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &visible) != nil {
				t.Fatalf("another ordinary user could not list their own collections: status=%d", w.Code)
			}
			for _, request := range visible {
				if request.UserId != other.Id {
					t.Fatal("ordinary user's list exposed another user's collection")
				}
			}

			owner.RemovePermission(models.UserPermGuestUploads)
			database.SaveUser(owner, false)
			beforeRequests = len(database.GetAllFileRequests())
			beforeKeys = len(database.GetAllApiKeys())
			w, r = getRecorderWithBody("/uploadrequest/save", ownerKey.Id, http.MethodPost, createHeaders, nil)
			Process(w, r)
			if w.Code != http.StatusUnauthorized || len(database.GetAllFileRequests()) != beforeRequests || len(database.GetAllApiKeys()) != beforeKeys {
				t.Fatalf("revoked permission must disable the existing key without saving a request or credential: status=%d", w.Code)
			}
		})
	}
}
