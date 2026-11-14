package database

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/configuration/database/dbabstraction"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
)

func TestN04TemporaryApiKeyQuota(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			config := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "temporary.sqlite")}
			if backend == "redis" {
				config = models.DbConnection{Type: dbabstraction.TypeRedis, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "temporary_[*?]"}
			}
			a, err := dbabstraction.GetNew(config)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			b, err := dbabstraction.GetNew(config)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Close()
			key := func(id string, user int) models.ApiKey {
				return models.ApiKey{Id: id, PublicId: "public-" + id, UserId: user, IsSystemKey: true, Permissions: models.ApiPermEdit, Expiry: time.Now().Add(models.TemporaryApiKeyTTL).Unix()}
			}
			for _, mutate := range []func(*models.ApiKey){func(k *models.ApiKey) { k.IsSystemKey = false }, func(k *models.ApiKey) { k.UserId = 0 }, func(k *models.ApiKey) { k.UploadRequestId = "request" }, func(k *models.ApiKey) { k.Expiry = 0 }, func(k *models.ApiKey) { k.Expiry = time.Now().Unix() }, func(k *models.ApiKey) { k.Expiry = time.Now().Add(time.Hour).Unix() }} {
				k := key("invalid", 1)
				mutate(&k)
				if ok, err := a.CreateTemporaryApiKey(k); ok || !errors.Is(err, models.ErrInvalidApiKey) {
					t.Fatal("invalid admitted", ok, err)
				}
			}
			ordinary := key("ordinary", 1)
			ordinary.IsSystemKey = false
			ordinary.Expiry = 0
			a.SaveApiKey(ordinary)
			request := key("request", 1)
			request.UploadRequestId = "request"
			a.SaveApiKey(request)
			for i := 0; i < models.TemporaryApiKeyMaxPerUser-2; i++ {
				a.SaveApiKey(key(fmt.Sprintf("existing-%d", i), 1))
			}
			// Expired rows must not prevent issuance or retain unbounded stale storage.
			expired := key("expired", 1)
			expired.Expiry = time.Now().Unix()
			a.SaveApiKey(expired)
			var wg sync.WaitGroup
			results := make(chan bool, 20)
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					store := a
					if i%2 == 1 {
						store = b
					}
					ok, err := store.CreateTemporaryApiKey(key(fmt.Sprintf("race-%d", i), 1))
					if err != nil && !errors.Is(err, models.ErrTemporaryApiKeyQuotaExceeded) {
						t.Errorf("issuance: %v", err)
					}
					results <- ok
				}(i)
			}
			wg.Wait()
			close(results)
			wins := 0
			for ok := range results {
				if ok {
					wins++
				}
			}
			if wins != 2 {
				t.Fatalf("quota race wins=%d", wins)
			}
			if _, exists := a.GetApiKey("expired"); exists {
				t.Fatal("expired row retained")
			}
			if ok, err := b.CreateTemporaryApiKey(key("other-user", 2)); !ok || err != nil {
				t.Fatal("one owner blocked another", ok, err)
			}
			if ok, err := b.CreateTemporaryApiKey(key("ordinary", 2)); ok || err != nil {
				t.Fatal("collision overwrote key", ok, err)
			}
			got, _ := a.GetApiKey("ordinary")
			if got.IsSystemKey || got.UserId != 1 {
				t.Fatal("collision changed credential")
			}
			a.DeleteApiKey("existing-0")
			if ok, err := b.CreateTemporaryApiKey(key("after-delete", 1)); !ok || err != nil {
				t.Fatal("delete did not release quota", ok, err)
			}
			// Other key types remain untouched even with matching expiry/owner.
			if _, ok := a.GetApiKey("request"); !ok {
				t.Fatal("request key removed")
			}
			total := models.TemporaryApiKeyMaxPerUser + 1 // owner 1 plus other-user
			for i := total; i < models.TemporaryApiKeyMaxGlobal-1; i++ {
				a.SaveApiKey(key(fmt.Sprintf("global-%d", i), 10000+i))
			}
			results = make(chan bool, 10)
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					store := a
					if i%2 == 1 {
						store = b
					}
					ok, err := store.CreateTemporaryApiKey(key(fmt.Sprintf("global-race-%d", i), 30000+i))
					if err != nil && !errors.Is(err, models.ErrTemporaryApiKeyQuotaExceeded) {
						t.Errorf("global: %v", err)
					}
					results <- ok
				}(i)
			}
			wg.Wait()
			close(results)
			wins = 0
			for ok := range results {
				if ok {
					wins++
				}
			}
			if wins != 1 {
				t.Fatalf("global quota race wins=%d initial=%d final=%d", wins, total, len(a.GetAllApiKeys()))
			}
			if ok, err := a.CreateTemporaryApiKey(key("global-overflow", 50000)); ok || !errors.Is(err, models.ErrTemporaryApiKeyQuotaExceeded) {
				t.Fatal(ok, err)
			}
			a.DeleteApiKey("global-100")
			if ok, err := b.CreateTemporaryApiKey(key("global-recovery", 50000)); !ok || err != nil {
				t.Fatal("global recovery", ok, err)
			}
		})
	}
}
