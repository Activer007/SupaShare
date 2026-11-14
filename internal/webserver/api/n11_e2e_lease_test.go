//go:build test

package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/e2emutex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func n11Request(path string, key models.ApiKey, lease string) *httptest.ResponseRecorder {
	body := `{"content":"` + base64.StdEncoding.EncodeToString([]byte(`{"version":1,"content":"AQI="}`)) + `"}`
	r := httptest.NewRequest(http.MethodPost, "/api/e2e/"+path, strings.NewReader(body))
	r.Header.Set("apikey", key.Id)
	if lease != "" {
		r.Header.Set("e2eLease", lease)
	}
	w := httptest.NewRecorder()
	Process(w, r)
	return w
}
func n11Key(t *testing.T, id int) models.ApiKey {
	user := models.User{Id: id, Name: "n11-user-" + strconv.Itoa(id), UserLevel: models.UserLevelUser}
	database.SaveUser(user, false)
	key := models.ApiKey{Id: strings.Repeat("k", 20) + user.Name + strconv.Itoa(id), PublicId: "n11-public-" + strconv.Itoa(id), UserId: id, Permissions: models.ApiPermUpload}
	database.SaveApiKey(key)
	t.Cleanup(func() { database.DeleteApiKey(key.Id); database.DeleteUser(id) })
	return key
}
func n11Acquire(t *testing.T, key models.ApiKey) string {
	t.Helper()
	w := n11Request("mutex/lock", key, "")
	var result struct {
		Token string `json:"leaseToken"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Token) != 64 {
		t.Fatal("acquisition failed", w.Code)
	}
	t.Cleanup(func() { e2emutex.Release(key.UserId, result.Token) })
	return result.Token
}
func TestN11E2ELeaseRoutes(t *testing.T) {
	key := n11Key(t, 99101)
	other := n11Key(t, 99102)
	token := n11Acquire(t, key)
	if w := n11Request("mutex/lock", key, ""); w.Code != 409 {
		t.Fatal("contended acquisition not rejected", w.Code)
	}
	for _, path := range []string{"get", "set", "mutex/unlock"} {
		if w := n11Request(path, key, ""); w.Code != 400 {
			t.Fatal("missing token accepted", path, w.Code)
		}
		if w := n11Request(path, key, strings.Repeat("0", 64)); w.Code != 409 {
			t.Fatal("wrong token accepted", path, w.Code)
		}
		if w := n11Request(path, other, token); w.Code != 409 {
			t.Fatal("cross-user lease accepted", path, w.Code)
		}
	}
	if w := n11Request("set", key, token); w.Code != 200 {
		t.Fatal("lease holder set failed", w.Code)
	}
	if w := n11Request("get", key, token); w.Code != 200 {
		t.Fatal("lease holder get failed", w.Code)
	}
	if w := n11Request("mutex/unlock", key, token); w.Code != 200 {
		t.Fatal("release failed", w.Code)
	}
	next := n11Acquire(t, key)
	if w := n11Request("mutex/unlock", key, token); w.Code != 409 {
		t.Fatal("old lease released successor")
	}
	if w := n11Request("get", key, next); w.Code != 200 {
		t.Fatal("successor lease lost")
	}
	database.UpdateApiKeyPermission(key.Id, models.ApiPermUpload, false)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/api/e2e/get", nil)
	apiE2eGet(w, &paramE2eLease{Request: r, LeaseToken: next}, models.User{Id: key.UserId}, key)
	if w.Code != 409 {
		t.Fatal("stale authorization accepted")
	}
}
func TestN11E2ELeaseHTTPContentionAndCancellation(t *testing.T) {
	key := n11Key(t, 99103)
	token := n11Acquire(t, key)
	var wg sync.WaitGroup
	codes := make(chan int, 128)
	for i := 0; i < 128; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); codes <- n11Request("mutex/lock", key, "").Code }()
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		if code != 409 {
			t.Fatal("queued acquisition", code)
		}
	}
	e2emutex.Release(key.UserId, token)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("POST", "/api/e2e/mutex/lock", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	apiE2eMutexLock(w, &paramE2eAcquire{Request: r}, models.User{Id: key.UserId}, key)
	if strings.Contains(w.Body.String(), "leaseToken") {
		t.Fatal("cancelled request minted lease")
	}
	n11Acquire(t, key)
}
