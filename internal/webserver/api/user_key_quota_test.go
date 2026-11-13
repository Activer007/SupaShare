package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUserKeyNameBounds(t *testing.T) {
	for _, parser := range []requestParser{&paramAuthCreate{}, &paramAuthFriendlyName{}} {
		for _, name := range []string{strings.Repeat("a", 256), strings.Repeat("鱼", 85), ""} {
			for _, encoded := range []bool{false, true} {
				r := httptest.NewRequest("POST", "/api/auth/create", nil)
				r.Header.Set("targetKey", "fixture")
				value := name
				if encoded {
					value = "base64:" + base64.StdEncoding.EncodeToString([]byte(name))
				}
				r.Header.Set("friendlyName", value)
				// Rename requires a non-empty header; base64 empty still means default name.
				if _, rename := parser.(*paramAuthFriendlyName); rename && value == "" {
					continue
				}
				if err := parser.New().ParseRequest(r); err != nil {
					t.Fatalf("valid bytes=%d encoded=%v: %v", len(name), encoded, err)
				}
			}
		}
		for _, name := range []string{strings.Repeat("a", 257), strings.Repeat("鱼", 86), "invalid\ncontrol", string([]byte{0xff}), strings.Repeat("a", 1<<20)} {
			r := httptest.NewRequest("POST", "/api/auth/create", nil)
			r.Header.Set("targetKey", "fixture")
			r.Header.Set("friendlyName", "base64:"+base64.StdEncoding.EncodeToString([]byte(name)))
			if err := parser.New().ParseRequest(r); err == nil {
				t.Fatalf("accepted invalid name bytes=%d", len(name))
			}
		}
	}
}

func TestUserKeyCreationFailures(t *testing.T) {
	calls := 0
	key, err := createUserApiKey(true, 7, "", func(k models.ApiKey) (bool, error) {
		calls++
		if k.FriendlyName != "Unnamed key" || k.Permissions != models.ApiPermDefault || k.IsSystemKey || k.UploadRequestId != "" {
			t.Fatal("invalid ordinary key", k)
		}
		return calls == 2, nil
	})
	if err != nil || key.Id == "" || calls != 2 {
		t.Fatal("collision not retried", err, calls)
	}
	calls = 0
	_, err = createUserApiKey(false, 7, strings.Repeat("x", 257), func(models.ApiKey) (bool, error) { calls++; return true, nil })
	if err == nil || calls != 0 {
		t.Fatal("oversized value reached persistence")
	}
	calls = 0
	_, err = createUserApiKey(false, 7, "normal", func(models.ApiKey) (bool, error) { calls++; return false, models.ErrApiKeyQuotaExceeded })
	if !errors.Is(err, models.ErrApiKeyQuotaExceeded) || calls != 1 {
		t.Fatal("capacity rejection retried", err, calls)
	}
	calls = 0
	_, err = createUserApiKey(false, 7, "normal", func(models.ApiKey) (bool, error) { calls++; return false, nil })
	if err == nil || calls != 5 {
		t.Fatal("collision retries not bounded", err, calls)
	}
}

func TestUserKeyAPIQuotaAndRecovery(t *testing.T) {
	for _, level := range []models.UserRank{models.UserLevelUser, models.UserLevelAdmin} {
		owner := models.User{Id: 887701 + int(level), Name: "quota@example.invalid", UserLevel: level}
		database.SaveUser(owner, false)
		var ids []string
		t.Cleanup(func() {
			for _, id := range ids {
				database.DeleteApiKey(id)
			}
			database.DeleteUser(owner.Id)
		})
		control := models.ApiKey{Id: fmt.Sprintf("quota-control-%d", owner.Id), UserId: owner.Id, IsSystemKey: true, Expiry: time.Now().Add(time.Minute).Unix(), Permissions: models.ApiPermApiMod}
		database.SaveApiKey(control)
		ids = append(ids, control.Id)
		request := func(path, name, target string) *httptest.ResponseRecorder {
			r := httptest.NewRequest("POST", path, nil)
			r.Header.Set("apikey", control.Id)
			r.Header.Set("friendlyName", name)
			r.Header.Set("targetKey", target)
			w := httptest.NewRecorder()
			Process(w, r)
			return w
		}
		for n := 0; n < models.ApiKeyMaxPerUser; n++ {
			w := request("/api/auth/create", "fixture", "")
			var key models.ApiKeyOutput
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &key) != nil || key.Id == "" {
				t.Fatalf("create %d: %d %s", n, w.Code, w.Body.String())
			}
			ids = append(ids, key.Id)
		}
		if w := request("/api/auth/create", "blocked", ""); w.Code != 409 || strings.Contains(w.Body.String(), `"PublicId"`) {
			t.Fatalf("quota: %d %s", w.Code, w.Body.String())
		}
		// Existing credentials remain available; overlong rename is rejected and cannot grow stored data.
		last := ids[len(ids)-1]
		if w := request("/api/auth/friendlyname", strings.Repeat("x", 257), last); w.Code != 400 {
			t.Fatalf("rename bound: %d %s", w.Code, w.Body.String())
		}
		if w := request("/api/auth/friendlyname", "shorter", last); w.Code != 200 {
			t.Fatalf("rename at capacity: %d %s", w.Code, w.Body.String())
		}
		if w := request("/api/auth/delete", "", last); w.Code != 200 {
			t.Fatalf("delete at capacity: %d %s", w.Code, w.Body.String())
		}
		w := request("/api/auth/create", "after deletion", "")
		var key models.ApiKeyOutput
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &key) != nil {
			t.Fatalf("recovery: %d %s", w.Code, w.Body.String())
		}
		ids = append(ids, key.Id)
	}
}
