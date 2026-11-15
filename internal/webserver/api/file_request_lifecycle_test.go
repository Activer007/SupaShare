//go:build test && !integration

package api

import (
	"encoding/json"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRequestLifecycleAPI(t *testing.T) {
	user := models.User{Id: 98761, UserLevel: models.UserLevelUser, Permissions: models.UserPermGuestUploads}
	var created []models.FileRequest
	t.Cleanup(func() {
		for _, r := range created {
			database.DeleteApiKey(r.ApiKey)
			database.DeleteFileRequest(r)
		}
	})
	for _, tc := range []struct {
		name   string
		expiry int64
		status int
	}{
		{"default", 0, 200}, {"far-future", time.Now().Unix() + 91*86400, 400}, {"past", 1, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			apiURequestSave(w, &paramURequestSave{Name: "N13 synthetic", Expiry: tc.expiry, IsExpirySet: true, MaxFiles: 1, MaxSizeMb: 1, IsMaxFilesSet: true, IsMaxSizeSet: true}, user, models.ApiKey{})
			if w.Code != tc.status {
				t.Fatalf("status=%d body=%s", w.Code, w.Body)
			}
			if w.Code != 200 {
				return
			}
			var r models.FileRequest
			if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
				t.Fatal(err)
			}
			created = append(created, r)
			if r.Expiry < time.Now().Unix()+4*86400 || r.Expiry > time.Now().Unix()+6*86400 {
				t.Fatal("default expiry not finite")
			}
			key, ok := database.GetApiKey(r.ApiKey)
			if !ok || key.Expiry != r.Expiry || key.UploadRequestId != r.Id {
				t.Fatal("atomic credential mismatch")
			}
			w = httptest.NewRecorder()
			apiURequestSave(w, &paramURequestSave{Id: r.Id, Notes: "edited", IsNotesSet: true, Expiry: r.Expiry, IsExpirySet: true}, user, models.ApiKey{})
			if w.Code != 200 {
				t.Fatalf("unchanged expiry update: %s", w.Body)
			}
		})
	}
}

func TestRequestPaginationParsing(t *testing.T) {
	for _, tc := range []struct {
		query string
		ok    bool
	}{
		{"", true}, {"?limit=100", true}, {"?limit=101", false}, {"?limit=0", false}, {"?before=broken", false},
		{"?before=" + models.FileRequestCursor(models.FileRequest{Id: "legacy", CreationDate: 42}), true},
	} {
		p := &paramURequestPage{}
		err := p.ParseRequest(httptest.NewRequest("GET", "/api/uploadrequest/list"+tc.query, nil))
		if (err == nil) != tc.ok {
			t.Fatalf("%s: %v", tc.query, err)
		}
	}
}
