package sqlite

import (
	"fmt"
	"github.com/Activer007/SupaShare/internal/models"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestN05FieldUpdatesAcrossProviders(t *testing.T) {
	config := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "n05.sqlite")}
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

// Expired rows may await cleanup, but field edits must never renew them.
func TestN05ExpiredFieldUpdateDoesNotRenew(t *testing.T) {
	p, err := New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "expired.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	expiry := time.Now().Unix() - 60
	key := models.ApiKey{Id: "expired", PublicId: "expired-public", UserId: 42, Expiry: expiry, Permissions: models.ApiPermDelete}
	p.SaveApiKey(key)
	if ok, err := p.UpdateApiKeyName(key.Id, "edited"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if ok, err := p.UpdateApiKeyPermission(key.Id, models.ApiPermDelete, false); !ok || err != nil {
		t.Fatal(ok, err)
	}
	got, ok := p.GetApiKey(key.Id)
	if !ok || got.Expiry != expiry || got.FriendlyName != "edited" || got.Permissions != 0 {
		t.Fatalf("expired key renewed or update lost: %+v %v", got, ok)
	}
}
