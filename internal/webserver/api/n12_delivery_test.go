//go:build test

package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestN12DeliveryAPIPagination(t *testing.T) {
	owner := models.User{Id: 912120, Name: "n12-" + rand.Text(), UserLevel: models.UserLevelUser}
	database.SaveUser(owner, false)
	key := generateNewKey(true, owner.Id, "n12", "")
	defer database.DeleteApiKey(key.Id)
	defer database.DeleteUser(owner.Id)
	ctx := context.Background()
	now := time.Now().Unix()
	for _, token := range []string{strings.Repeat("a", 43), strings.Repeat("b", 43), strings.Repeat("c", 43)} {
		d := models.Delivery{Token: token, OwnerID: owner.Id, Title: "page", FileIDs: []string{"a", "b"}, CreatedAt: now}
		if ok, err := database.InsertDelivery(ctx, d); err != nil || !ok {
			t.Fatal(err)
		}
	}
	first := deliveryAPIRequest(t, "list?limit=2", http.MethodGet, key.Id, nil)
	var page []deliveryOutput
	if first.Code != 200 || json.Unmarshal(first.Body.Bytes(), &page) != nil || len(page) != 2 || first.Header().Get("X-Next-Cursor") != models.EncodeDeliveryCursor(models.Delivery{Token: strings.Repeat("b", 43), CreatedAt: now}) {
		t.Fatalf("first %d %s %v", first.Code, first.Body, first.Header())
	}
	second := deliveryAPIRequest(t, "list?limit=2&before="+first.Header().Get("X-Next-Cursor"), http.MethodGet, key.Id, nil)
	if second.Code != 200 || json.Unmarshal(second.Body.Bytes(), &page) != nil || len(page) != 1 || page[0].Token != strings.Repeat("a", 43) || second.Header().Get("X-Next-Cursor") != "" {
		t.Fatalf("second %d %s", second.Code, second.Body)
	}
	for _, q := range []string{"limit=0", "limit=101", "limit=-1", "limit=x", "before=short"} {
		if w := deliveryAPIRequest(t, "list?"+q, http.MethodGet, key.Id, nil); w.Code != 400 {
			t.Fatalf("accepted %s: %d", q, w.Code)
		}
	}
}

func TestN12DeliveryAPIQuota(t *testing.T) {
	owner := models.User{Id: 912121, Name: "n12-cap-" + rand.Text(), UserLevel: models.UserLevelAdmin}
	database.SaveUser(owner, false)
	defer database.DeleteUser(owner.Id)
	key := generateNewKey(true, owner.Id, "n12-cap", "")
	defer database.DeleteApiKey(key.Id)
	ids := []string{"n12-cap-a", "n12-cap-b"}
	for _, id := range ids {
		database.SaveMetaData(models.File{Id: id, Name: "source", UserId: owner.Id, UnlimitedDownloads: true, UnlimitedTime: true})
		defer database.DeleteMetaData(id)
	}
	now := time.Now().Unix()
	for i := 0; i < models.MaxDeliveriesPerOwner; i++ {
		d := models.Delivery{Token: rand.Text() + rand.Text(), OwnerID: owner.Id, Title: "retained", FileIDs: ids, CreatedAt: now, RevokedAt: now}
		if ok, err := database.InsertDelivery(context.Background(), d); !ok || err != nil {
			t.Fatal(err)
		}
	}
	w := deliveryAPIRequest(t, "create", http.MethodPost, key.Id, InputForN12(ids))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected quota rejection, got %d %s", w.Code, w.Body)
	}
}
func InputForN12(ids []string) map[string]any {
	return map[string]any{"title": "over-cap", "fileIds": ids, "expiresAt": 0}
}
