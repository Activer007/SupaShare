//go:build test

package api

import (
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func n05Keys(t *testing.T) (models.User, models.ApiKey, models.ApiKey) {
	t.Helper()
	user := models.User{Id: 889901, Name: "n05@example.invalid", UserLevel: models.UserLevelUser}
	database.SaveUser(user, false)
	actor := models.ApiKey{Id: strings.Repeat("a", 30), PublicId: strings.Repeat("a", 35), UserId: user.Id, Permissions: models.ApiPermApiMod}
	target := models.ApiKey{Id: strings.Repeat("b", 30), PublicId: strings.Repeat("b", 35), UserId: user.Id, Permissions: models.ApiPermView | models.ApiPermDelete}
	database.SaveApiKey(actor)
	database.SaveApiKey(target)
	t.Cleanup(func() {
		database.DeleteApiKey(actor.Id)
		database.DeleteApiKey(target.Id)
		database.DeleteUser(user.Id)
	})
	return user, actor, target
}

func TestN05AliasesAndConcurrentFields(t *testing.T) {
	user, actor, target := n05Keys(t)
	for i := 0; i < 30; i++ {
		database.UpdateApiKeyPermission(target.Id, models.ApiPermDelete, true)
		var wg sync.WaitGroup
		codes := make(chan int, 3)
		for _, f := range []func(*httptest.ResponseRecorder){
			func(w *httptest.ResponseRecorder) {
				apiModifyApiKey(w, &paramAuthModify{KeyId: target.PublicId, Permission: models.ApiPermDelete, GrantPermission: false}, user, actor)
			},
			func(w *httptest.ResponseRecorder) {
				apiModifyApiKey(w, &paramAuthModify{KeyId: target.Id, Permission: models.ApiPermUpload, GrantPermission: true}, user, actor)
			},
			func(w *httptest.ResponseRecorder) {
				apiChangeFriendlyName(w, &paramAuthFriendlyName{KeyId: target.PublicId, FriendlyName: "renamed"}, user, actor)
			},
		} {
			wg.Add(1)
			go func(f func(*httptest.ResponseRecorder)) {
				defer wg.Done()
				w := httptest.NewRecorder()
				f(w)
				codes <- w.Code
			}(f)
		}
		wg.Wait()
		close(codes)
		for code := range codes {
			if code != 200 {
				t.Fatalf("edit status %d", code)
			}
		}
		got, _ := database.GetApiKey(target.Id)
		if got.HasPermission(models.ApiPermDelete) || !got.HasPermission(models.ApiPermView|models.ApiPermUpload) || got.FriendlyName != "renamed" {
			t.Fatal("concurrent edit lost or restored a permission")
		}
	}
}

func TestN05RevalidatesCallerBeforeEditing(t *testing.T) {
	user, actor, target := n05Keys(t)
	database.UpdateApiKeyPermission(actor.Id, models.ApiPermApiMod, false)
	for _, f := range []func(*httptest.ResponseRecorder){
		func(w *httptest.ResponseRecorder) {
			apiModifyApiKey(w, &paramAuthModify{KeyId: target.PublicId, Permission: models.ApiPermUpload, GrantPermission: true}, user, actor)
		},
		func(w *httptest.ResponseRecorder) {
			apiChangeFriendlyName(w, &paramAuthFriendlyName{KeyId: target.Id, FriendlyName: "denied"}, user, actor)
		},
		func(w *httptest.ResponseRecorder) {
			apiDeleteKey(w, &paramAuthDelete{KeyId: target.PublicId}, user, actor)
		},
	} {
		w := httptest.NewRecorder()
		f(w)
		if w.Code != 403 {
			t.Fatalf("stale caller accepted: %d", w.Code)
		}
	}
	got, ok := database.GetApiKey(target.Id)
	if !ok || got.Permissions != target.Permissions || got.FriendlyName != target.FriendlyName {
		t.Fatal("denied operation changed target")
	}
}

func TestN05SelfEditAndRevocation(t *testing.T) {
	user, actor, _ := n05Keys(t)
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		apiChangeFriendlyName(w, &paramAuthFriendlyName{KeyId: actor.PublicId, FriendlyName: "self"}, user, actor)
		if w.Code != 200 {
			done <- w.Code
			return
		}
		w = httptest.NewRecorder()
		apiModifyApiKey(w, &paramAuthModify{KeyId: actor.Id, Permission: models.ApiPermApiMod}, user, actor)
		done <- w.Code
	}()
	select {
	case status := <-done:
		if status != 200 {
			t.Fatalf("self edit: %d", status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("self edit deadlocked")
	}
	got, _ := database.GetApiKey(actor.Id)
	if got.FriendlyName != "self" || got.HasPermission(models.ApiPermApiMod) {
		t.Fatal("self edit did not persist")
	}
}

func TestN05OwnerAuthorityReloaded(t *testing.T) {
	user, actor, target := n05Keys(t)
	user.Permissions = models.UserPermReplaceUploads
	database.SaveUser(user, false)
	unlock := apimutex.LockMany(apimutex.TypeUser, strconv.Itoa(user.Id))
	done := make(chan int, 1)
	go func() {
		w := httptest.NewRecorder()
		apiModifyApiKey(w, &paramAuthModify{KeyId: target.Id, Permission: models.ApiPermReplace, GrantPermission: true}, user, actor)
		done <- w.Code
	}()
	// The HTTP entry's user snapshot still has ReplaceUploads; the authoritative
	// user loses it before the editing gate can be acquired.
	current := user
	current.Permissions = models.UserPermissionNone
	database.SaveUser(current, false)
	unlock()
	select {
	case status := <-done:
		if status != 401 {
			t.Fatalf("stale owner authority accepted: %d", status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owner reload deadlocked")
	}
	got, _ := database.GetApiKey(target.Id)
	if got.HasPermission(models.ApiPermReplace) {
		t.Fatal("revoked owner capability was granted")
	}
}
