package redis

import (
	"fmt"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
	"sync"
	"testing"
	"time"
)

func TestN05FieldUpdatesAcrossProviders(t *testing.T) {
	server := miniredis.RunT(t)
	config := models.DbConnection{HostUrl: server.Addr(), RedisPrefix: "n05_", Type: 1}
	a, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	check := func(ok bool, err error) {
		t.Helper()
		if !ok || err != nil {
			t.Errorf("update failed: %v %v", ok, err)
		}
	}
	for iteration := 0; iteration < 30; iteration++ {
		id := fmt.Sprintf("n05-%d", iteration)
		original := models.ApiKey{Id: id, PublicId: "public-" + id, UserId: 42, FriendlyName: "old", Permissions: models.ApiPermDelete | models.ApiPermView, Expiry: time.Now().Unix() + 600, IsSystemKey: true, UploadRequestId: "request", LastUsed: 123}
		a.SaveApiKey(original)
		ttlBefore := server.TTL(config.RedisPrefix + prefixApiKeys + id)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for _, update := range []func() (bool, error){
			func() (bool, error) { return a.UpdateApiKeyPermission(id, models.ApiPermDelete, false) },
			func() (bool, error) { return b.UpdateApiKeyPermission(id, models.ApiPermUpload, true) },
			func() (bool, error) { return b.UpdateApiKeyName(id, "new") },
		} {
			wg.Add(1)
			go func(update func() (bool, error)) { defer wg.Done(); <-start; check(update()) }(update)
		}
		close(start)
		wg.Wait()
		if gotTTL := server.TTL(config.RedisPrefix + prefixApiKeys + id); gotTTL != ttlBefore || gotTTL <= 0 {
			t.Fatalf("TTL changed: %v -> %v", ttlBefore, gotTTL)
		}
		got, ok := a.GetApiKey(id)
		if !ok {
			t.Fatal("key disappeared")
		}
		want := original
		want.FriendlyName = "new"
		want.Permissions = models.ApiPermView | models.ApiPermUpload
		if got != want {
			t.Fatalf("lost field update or changed unrelated fields: got %+v want %+v", got, want)
		}
		// An old reader updating after deletion must not recreate a credential.
		a.DeleteApiKey(id)
		if ok, err := b.UpdateApiKeyName(id, "resurrect"); ok || err != nil {
			t.Fatal(ok, err)
		}
		if ok, err := b.UpdateApiKeyPermission(id, models.ApiPermDelete, true); ok || err != nil {
			t.Fatal(ok, err)
		}
		if _, ok := a.GetApiKey(id); ok {
			t.Fatal("deleted key resurrected")
		}
	}
}

func TestN05ExpiredFieldUpdateDoesNotResurrect(t *testing.T) {
	server := miniredis.RunT(t)
	p, err := New(models.DbConnection{HostUrl: server.Addr(), RedisPrefix: "n05_expired_", Type: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	key := models.ApiKey{Id: "expired", PublicId: "expired-public", UserId: 42, Expiry: time.Now().Unix() + 10, Permissions: models.ApiPermDelete}
	p.SaveApiKey(key)
	server.FastForward(11 * time.Second)
	if _, ok := p.GetApiKey(key.Id); ok {
		t.Fatal("fixture did not expire")
	}
	if ok, err := p.UpdateApiKeyName(key.Id, "edited"); ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := p.UpdateApiKeyPermission(key.Id, models.ApiPermUpload, true); ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, ok := p.GetApiKey(key.Id); ok {
		t.Fatal("expired key resurrected")
	}
}
