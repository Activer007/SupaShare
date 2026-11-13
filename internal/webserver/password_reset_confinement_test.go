//go:build test && !integration

package webserver

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/storage/delivery"
	"github.com/Activer007/SupaShare/internal/webserver/authentication/csrftoken"
	"github.com/Activer007/SupaShare/internal/webserver/errorHandling/errorcodes"
)

// Use the registered public routes with a reset-required owner and synthetic
// content. The invitation API request deliberately stops at parameter parsing.
func TestPasswordResetPreservesPublicHTTP(t *testing.T) {
	owner := models.User{Id: 9832, Name: "synthetic-reset-public", UserLevel: models.UserLevelUser, ResetPassword: true, Permissions: models.UserPermGuestUploads}
	database.SaveUser(owner, false)
	t.Cleanup(func() { database.DeleteUser(owner.Id) })
	const content = "synthetic public reset content"
	blobID := "reset-public-" + rand.Text()
	blobPath := filepath.Join(configuration.Get().DataDir, blobID)
	if err := os.WriteFile(blobPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(blobPath) })
	ids := []string{"reset-share-" + rand.Text(), "reset-share-" + rand.Text()}
	for _, id := range ids {
		file := models.File{Id: id, UserId: owner.Id, Name: "synthetic-public.txt", SHA1: blobID, SizeBytes: int64(len(content)), UnlimitedTime: true, UnlimitedDownloads: true}
		database.SaveMetaData(file)
		t.Cleanup(func() { database.DeleteMetaData(file.Id) })
	}
	d, err := delivery.Create(context.Background(), owner.Id, delivery.Input{Title: "Synthetic reset delivery", FileIDs: ids})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = database.RevokeDelivery(context.Background(), d.Token, owner.Id, time.Now().Unix()) })
	invitation := models.ApiKey{Id: rand.Text(), UserId: owner.Id, UploadRequestId: "synthetic-reset-public-request"}
	collection := models.FileRequest{Id: invitation.UploadRequestId, UserId: owner.Id, ApiKey: invitation.Id, Name: "Synthetic reset invitation"}
	database.SaveApiKey(invitation)
	t.Cleanup(func() { database.DeleteApiKey(invitation.Id) })
	database.SaveFileRequest(collection)
	t.Cleanup(func() { database.DeleteFileRequest(collection) })
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, tc := range []struct {
		path, marker string
	}{
		{"/d?id=" + ids[0], "synthetic-public.txt"},
		{"/downloadFile?id=" + ids[0], content},
		{"/delivery/" + d.Token, d.Title},
		{"/r/" + invitation.Id, collection.Name},
		{"/publicUpload?id=" + collection.Id + "&key=" + invitation.Id, collection.Name},
	} {
		t.Run(tc.path, func(t *testing.T) {
			response, err := client.Get("http://localhost:53843" + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(body), tc.marker) {
				t.Fatalf("status=%d read=%v body=%s", response.StatusCode, err, body)
			}
		})
	}
	req, err := http.NewRequest(http.MethodPost, "http://localhost:53843/api/uploadrequest/chunk/reserve", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("apikey", invitation.Id)
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var result struct{ ErrorCode int }
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil || response.StatusCode != http.StatusBadRequest || result.ErrorCode != errorcodes.CannotParse {
		t.Fatalf("public API failed before parameter parsing: status=%d code=%d error=%v", response.StatusCode, result.ErrorCode, err)
	}
	stored, ok := database.GetApiKey(invitation.Id)
	if !ok || stored.Id != invitation.Id || stored.UserId != owner.Id || stored.UploadRequestId != collection.Id {
		t.Fatal("public request changed the invitation credential")
	}
}

// Exercise the actual registered routes, including a default administrative
// force-change, a fresh login with the retained password and existing API keys.
func TestPasswordResetConfinementHTTP(t *testing.T) {
	user := models.User{Id: 9811, Name: "reset-confinement", UserLevel: models.UserLevelUser, Password: configuration.HashPassword("synthetic-old-password", false, "")}
	admin := models.User{Id: 9812, Name: "reset-administrator", UserLevel: models.UserLevelAdmin, Permissions: models.UserPermManageUsers}
	for _, u := range []models.User{user, admin} {
		database.SaveUser(u, false)
		t.Cleanup(func() { database.DeleteAllSessionsByUser(u.Id); database.DeleteUser(u.Id) })
	}
	userKey := models.ApiKey{Id: "synthetic-reset-existing-key", PublicId: "reset-existing-public", UserId: user.Id, Permissions: models.ApiPermView | models.ApiPermUpload | models.ApiPermDownload}
	adminKey := models.ApiKey{Id: "synthetic-reset-admin-key", PublicId: "reset-admin-public", UserId: admin.Id, Permissions: models.ApiPermManageUsers}
	for _, k := range []models.ApiKey{userKey, adminKey} {
		database.SaveApiKey(k)
		t.Cleanup(func() { database.DeleteApiKey(k.Id) })
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(path string, form url.Values, cookie *http.Cookie, headers map[string]string) (*http.Response, string) {
		t.Helper()
		method := http.MethodGet
		var body io.Reader
		if form != nil {
			method = http.MethodPost
			body = strings.NewReader(form.Encode())
		}
		req, err := http.NewRequest(method, "http://localhost:53843"+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if form != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		req.AddCookie(&http.Cookie{Name: browserCSRFCookieName(), Value: sec023LegacyTestBinding})
		if cookie != nil {
			req.AddCookie(cookie)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		return res, string(data)
	}
	login := func(password string) *http.Cookie {
		t.Helper()
		res, body := request("/login", url.Values{"username": {user.Name}, "password": {password}, "csrf-token": {csrftoken.GenerateBound(csrftoken.TypeLogin, sec023LegacyTestBinding)}}, nil, nil)
		if res.StatusCode != http.StatusTemporaryRedirect {
			t.Fatalf("login: %d %s", res.StatusCode, body)
		}
		for _, c := range res.Cookies() {
			if c.Name == "session_token" {
				return c
			}
		}
		t.Fatal("no session cookie")
		return nil
	}
	oldSession := login("synthetic-old-password")
	res, body := request("/api/files/list", nil, nil, map[string]string{"apikey": userKey.Id})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("pre-reset key: %d %s", res.StatusCode, body)
	}
	res, body = request("/api/user/resetPassword", url.Values{}, nil, map[string]string{"apikey": adminKey.Id, "userid": "9811"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("reset: %d %s", res.StatusCode, body)
	}
	stored, _ := database.GetUser(user.Id)
	if !stored.ResetPassword || stored.Password != user.Password {
		t.Fatal("default reset must retain credential and require change")
	}
	res, _ = request("/auth/token", nil, oldSession, map[string]string{"permission": "PERM_VIEW"})
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal("old session survived reset")
	}
	session := login("synthetic-old-password")
	for _, path := range []string{"/auth/token", "/uploadChunk", "/uploadStatus", "/downloadPresigned"} {
		res, body = request(path, nil, session, map[string]string{"permission": "PERM_VIEW"})
		if res.StatusCode != http.StatusForbidden || !strings.Contains(body, "Password change required") {
			t.Fatalf("%s not confined: %d %s", path, res.StatusCode, body)
		}
	}
	res, _ = request("/admin", nil, session, nil)
	if res.StatusCode != http.StatusTemporaryRedirect || res.Header.Get("Location") != "/changePassword" {
		t.Fatal("UI did not redirect to password change")
	}
	for _, path := range []string{"/api/files/list", "/api/files/add", "/api/files/download/missing"} {
		res, body = request(path, nil, nil, map[string]string{"apikey": userKey.Id})
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("existing key escaped reset on %s: %d %s", path, res.StatusCode, body)
		}
	}
	res, _ = request("/logout", url.Values{"csrf-token": {csrftoken.GenerateBound(csrftoken.TypeLogout, sec023LegacyTestBinding)}}, session, nil)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatal("reset-required logout blocked")
	}
	session = login("synthetic-old-password")
	res, _ = request("/changePassword", nil, session, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatal("password form blocked")
	}
	res, _ = request("/changePassword", url.Values{"newpw": {"synthetic-new-password"}, "csrf-token": {"invalid"}}, session, nil)
	stored, _ = database.GetUser(user.Id)
	if !stored.ResetPassword {
		t.Fatal("bad CSRF cleared reset")
	}
	res, body = request("/changePassword", url.Values{"newpw": {"synthetic-new-password"}, "csrf-token": {csrftoken.GenerateBound(csrftoken.TypePasswordChange, sec023LegacyTestBinding)}}, session, nil)
	if res.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("password change: %d %s", res.StatusCode, body)
	}
	stored, _ = database.GetUser(user.Id)
	if stored.ResetPassword {
		t.Fatal("reset flag not cleared")
	}
	res, _ = request("/auth/token", nil, session, nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatal("change session not revoked")
	}
	session = login("synthetic-new-password")
	res, body = request("/auth/token", nil, session, map[string]string{"permission": "PERM_VIEW"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("new session token: %d %s", res.StatusCode, body)
	}
	res, _ = request("/logout", url.Values{"csrf-token": {csrftoken.GenerateBound(csrftoken.TypeLogout, sec023LegacyTestBinding)}}, session, nil)
	if res.StatusCode != http.StatusSeeOther {
		t.Fatal("logout blocked")
	}
	res, body = request("/api/files/list", nil, nil, map[string]string{"apikey": userKey.Id})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("completed reset key: %d %s", res.StatusCode, body)
	}
}
