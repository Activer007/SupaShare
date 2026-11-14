//go:build test

package api

import (
	"crypto/rand"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
)

func managementFixture(t *testing.T, rank models.UserRank, permissions models.UserPermission) models.User {
	t.Helper()
	name := "n01-" + rand.Text()
	database.SaveUser(models.User{Name: name, UserLevel: rank, Permissions: permissions, Password: "unchanged"}, true)
	u, ok := database.GetUserByName(name)
	if !ok {
		t.Fatal("fixture unavailable")
	}
	t.Cleanup(func() { database.DeleteAllSessionsByUser(u.Id); database.DeleteUser(u.Id) })
	return u
}
func managementKey(t *testing.T, user models.User, permissions models.ApiPermission) models.ApiKey {
	t.Helper()
	k := generateNewKey(false, user.Id, "N01 test", "")
	k.Permissions = permissions
	database.SaveApiKey(k)
	t.Cleanup(func() { database.DeleteApiKey(k.Id) })
	return k
}
func managementCall(key models.ApiKey, target models.User, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/api/user/"+path, nil)
	r.Header.Set("apikey", key.Id)
	r.Header.Set("userid", strconv.Itoa(target.Id))
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	Process(w, r)
	return w
}
func TestN01PrivilegedTargetAndDemotionChain(t *testing.T) {
	actor := managementFixture(t, models.UserLevelUser, models.UserPermManageUsers)
	key := managementKey(t, actor, models.ApiPermManageUsers|models.ApiPermDelete|models.ApiPermEdit)
	for _, target := range []models.User{
		managementFixture(t, models.UserLevelAdmin, models.UserPermissionAll),
		managementFixture(t, models.UserLevelUser, models.UserPermManageUsers|models.UserPermManageLogs),
	} {
		session := "n01-session-" + rand.Text()
		database.SaveSession(session, models.Session{UserId: target.Id, ValidUntil: 2147483645})
		targetKey := managementKey(t, target, models.ApiPermView)
		for _, call := range []struct {
			path    string
			headers map[string]string
		}{
			{"changeRank", map[string]string{"newRank": "USER"}},
			{"modify", map[string]string{"userpermission": "PERM_LOGS", "permissionModifier": "REVOKE"}},
			{"resetPassword", map[string]string{"generateNewPassword": "true"}},
			{"resetPassword", map[string]string{"generateNewPassword": "false"}},
			{"delete", map[string]string{"deleteFiles": "true"}},
			{"delete", map[string]string{"transferToSelf": "true"}},
		} {
			w := managementCall(key, target, call.path, call.headers)
			if w.Code != http.StatusForbidden {
				t.Errorf("%s status %d: %s", call.path, w.Code, w.Body.String())
			}
			current, ok := database.GetUser(target.Id)
			if !ok || current != target {
				t.Fatal("denied operation changed target")
			}
			if _, ok := database.GetSession(session); !ok {
				t.Fatal("denied operation revoked session")
			}
			if _, ok := database.GetApiKey(targetKey.Id); !ok {
				t.Fatal("denied operation revoked key")
			}
		}
	}
}
func TestN01DeletionDispositionMatrix(t *testing.T) {
	for _, tc := range []struct {
		name      string
		rank      models.UserRank
		userPerm  models.UserPermission
		keyPerm   models.ApiPermission
		delete    bool
		confirm   bool
		secondary string
		want      int
	}{
		{"manager no delete grant", models.UserLevelUser, models.UserPermManageUsers, models.ApiPermManageUsers | models.ApiPermDelete, true, false, "", 403},
		{"admin key missing delete", models.UserLevelAdmin, models.UserPermissionAll, models.ApiPermManageUsers, true, false, "", 403},
		{"ordinary cannot transfer", models.UserLevelUser, models.UserPermissionAll, models.ApiPermManageUsers | models.ApiPermEdit, false, true, "", 403},
		{"admin key missing edit", models.UserLevelAdmin, models.UserPermissionAll, models.ApiPermManageUsers, false, true, "", 403},
		{"transfer confirmation missing", models.UserLevelAdmin, models.UserPermissionAll, models.ApiPermManageUsers | models.ApiPermEdit, false, false, "", 400},
		{"foreign secondary", models.UserLevelAdmin, models.UserPermissionAll, models.ApiPermManageUsers, true, false, "foreign", 403},
		{"invalid secondary", models.UserLevelAdmin, models.UserPermissionAll, models.ApiPermManageUsers, true, false, "invalid", 403},
		{"delegated delete authorized", models.UserLevelUser, models.UserPermissionAll, models.ApiPermManageUsers | models.ApiPermDelete, true, false, "", 200},
		{"admin transfer authorized", models.UserLevelAdmin, models.UserPermissionAll, models.ApiPermManageUsers | models.ApiPermEdit, false, true, "", 200},
		{"separate delete token", models.UserLevelAdmin, models.UserPermissionAll, models.ApiPermManageUsers, true, false, "same", 200},
		{"separate edit token", models.UserLevelAdmin, models.UserPermissionAll, models.ApiPermManageUsers, false, true, "same", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := managementFixture(t, tc.rank, tc.userPerm)
			target := managementFixture(t, models.UserLevelUser, 0)
			key := managementKey(t, actor, tc.keyPerm)
			targetKey := managementKey(t, target, models.ApiPermView)
			session := "n01-session-" + rand.Text()
			database.SaveSession(session, models.Session{UserId: target.Id, ValidUntil: 2147483645})
			file := models.File{Id: "n01-file-" + rand.Text(), Name: "fixture", SHA1: "n01-missing-payload", UserId: target.Id, UnlimitedTime: true, UnlimitedDownloads: true}
			database.SaveMetaData(file)
			t.Cleanup(func() { database.DeleteMetaData(file.Id) })
			fr := models.FileRequest{Id: "n01-fr-" + rand.Text(), UserId: target.Id}
			database.SaveFileRequest(fr)
			t.Cleanup(func() { database.DeleteFileRequest(fr) })
			headers := map[string]string{"deleteFiles": strconv.FormatBool(tc.delete), "transferToSelf": strconv.FormatBool(tc.confirm)}
			if tc.secondary != "" {
				headers["fileOperationKey"] = "invalid"
				if tc.secondary != "invalid" {
					owner := actor
					if tc.secondary == "foreign" {
						owner = managementFixture(t, models.UserLevelAdmin, models.UserPermissionAll)
					}
					perm := models.ApiPermEdit
					if tc.delete {
						perm = models.ApiPermDelete
					}
					headers["fileOperationKey"] = managementKey(t, owner, perm).Id
				}
			}
			w := managementCall(key, target, "delete", headers)
			if w.Code != tc.want {
				t.Fatalf("status %d want %d: %s", w.Code, tc.want, w.Body.String())
			}
			if tc.want != 200 {
				if got, ok := database.GetUser(target.Id); !ok || got != target {
					t.Fatal("denied: user changed")
				}
				if got, ok := database.GetMetaDataById(file.Id); !ok || got.UserId != target.Id {
					t.Fatal("denied: file changed")
				}
				if got, ok := database.GetFileRequest(fr.Id); !ok || got.UserId != target.Id {
					t.Fatal("denied: request changed")
				}
				if _, ok := database.GetSession(session); !ok {
					t.Fatal("denied: session revoked")
				}
				if _, ok := database.GetApiKey(targetKey.Id); !ok {
					t.Fatal("denied: key revoked")
				}
			} else {
				if _, ok := database.GetUser(target.Id); ok {
					t.Fatal("authorized deletion retained user")
				}
				got, ok := database.GetMetaDataById(file.Id)
				if tc.delete && ok || !tc.delete && (!ok || got.UserId != actor.Id) {
					t.Fatal("incorrect file disposition")
				}
				request, ok := database.GetFileRequest(fr.Id)
				if tc.delete && ok || !tc.delete && (!ok || request.UserId != actor.Id) {
					t.Fatal("incorrect request disposition")
				}
			}
		})
	}
}

func TestN01AdminPromotionCannotGrantMissingAuthority(t *testing.T) {
	actor := managementFixture(t, models.UserLevelAdmin, models.UserPermManageUsers)
	key := managementKey(t, actor, models.ApiPermManageUsers)
	for _, rank := range []models.UserRank{models.UserLevelUser, models.UserLevelAdmin} {
		target := managementFixture(t, rank, 0)
		w := managementCall(key, target, "changeRank", map[string]string{"newRank": "ADMIN"})
		if w.Code != http.StatusForbidden {
			t.Fatalf("promotion: %d", w.Code)
		}
		if got, _ := database.GetUser(target.Id); got != target {
			t.Fatal("denied promotion changed target")
		}
	}
}
func TestN01DelegatedManagerResetBasicUser(t *testing.T) {
	actor := managementFixture(t, models.UserLevelUser, models.UserPermManageUsers)
	key := managementKey(t, actor, models.ApiPermManageUsers)
	target := managementFixture(t, models.UserLevelUser, 0)
	w := managementCall(key, target, "resetPassword", map[string]string{"generateNewPassword": "true"})
	if w.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
	got, _ := database.GetUser(target.Id)
	if !got.ResetPassword || got.Password == target.Password {
		t.Fatal("authorized reset failed")
	}
}

func TestN01RechecksAuthorityAfterUserLock(t *testing.T) {
	for _, revokeActor := range []bool{true, false} {
		t.Run(strconv.FormatBool(revokeActor), func(t *testing.T) {
			actor := managementFixture(t, models.UserLevelUser, models.UserPermManageUsers)
			target := managementFixture(t, models.UserLevelUser, 0)
			key := managementKey(t, actor, models.ApiPermManageUsers)
			unlock := apimutex.LockMany(apimutex.TypeUser, strconv.Itoa(actor.Id), strconv.Itoa(target.Id))
			done := make(chan int, 1)
			go func(actor, target models.User) {
				w := httptest.NewRecorder()
				apiResetPassword(w, &paramUserResetPw{Id: target.Id, NewPassword: true}, actor, key)
				done <- w.Code
			}(actor, target)
			// Simulate a preceding management operation committing before this stale
			// request can obtain its actor/target gates.
			if revokeActor {
				actor.Permissions = 0
				database.SaveUser(actor, false)
			} else {
				target.UserLevel = models.UserLevelAdmin
				database.SaveUser(target, false)
			}
			unlock()
			select {
			case status := <-done:
				if status != http.StatusForbidden {
					t.Fatalf("status %d", status)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("user gate did not release")
			}
			current, _ := database.GetUser(target.Id)
			if current.Password != target.Password || current.ResetPassword {
				t.Fatal("stale request reset credentials")
			}
		})
	}
}
