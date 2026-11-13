package database

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Activer007/SupaShare/internal/configuration/database/dbabstraction"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/alicebob/miniredis/v2"
)

func TestUserApiKeyQuota(t *testing.T) {
	for _, backend := range []string{"sqlite", "redis"} {
		t.Run(backend, func(t *testing.T) {
			config := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "quota.sqlite")}
			if backend == "redis" {
				config = models.DbConnection{Type: dbabstraction.TypeRedis, HostUrl: miniredis.RunT(t).Addr(), RedisPrefix: "quota_[*?]"}
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
				return models.ApiKey{Id: id, PublicId: "public-" + id, UserId: user, FriendlyName: "synthetic"}
			}
			if backend == "redis" {
				// Literal prefix matching must not count a neighboring namespace.
				otherConfig := config
				otherConfig.RedisPrefix = "quota_x"
				other, err := dbabstraction.GetNew(otherConfig)
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				for i := 0; i < models.ApiKeyMaxPerUser; i++ {
					other.SaveApiKey(key(fmt.Sprintf("neighbor-%d", i), 1))
				}
			}

			for _, name := range []string{strings.Repeat("x", 257), strings.Repeat("鱼", 86), string([]byte{0xff})} {
				k := key("invalid", 1)
				k.FriendlyName = name
				if ok, err := a.CreateUserApiKey(k); ok || !errors.Is(err, models.ErrInvalidApiKey) {
					t.Fatalf("invalid name accepted: %v %v", ok, err)
				}
			}
			k := key("boundary", 1)
			k.FriendlyName = strings.Repeat("x", 256)
			if ok, err := a.CreateUserApiKey(k); !ok || err != nil {
				t.Fatal(ok, err)
			}
			for i := 0; i < models.ApiKeyMaxPerUser-2; i++ {
				a.SaveApiKey(key(fmt.Sprintf("existing-%d", i), 1))
			}
			// Temporary and file-request keys do not consume the ordinary-key budget.
			for i := 0; i < 60; i++ {
				k := key(fmt.Sprintf("system-%d", i), 1)
				k.IsSystemKey = true
				a.SaveApiKey(k)
				k = key(fmt.Sprintf("request-%d", i), 1)
				k.UploadRequestId = "request"
				a.SaveApiKey(k)
			}
			var wg sync.WaitGroup
			results := make(chan error, 20)
			wins := make(chan string, 20)
			for i := 0; i < 20; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					store := a
					if i%2 == 1 {
						store = b
					}
					id := fmt.Sprintf("concurrent-%d", i)
					ok, err := store.CreateUserApiKey(key(id, 1))
					if ok {
						wins <- id
					}
					results <- err
				}(i)
			}
			wg.Wait()
			close(wins)
			close(results)
			if len(wins) != 1 {
				t.Fatalf("quota admitted %d concurrent keys, want one", len(wins))
			}
			for err := range results {
				if err != nil && !errors.Is(err, models.ErrApiKeyQuotaExceeded) {
					t.Fatal(err)
				}
			}
			if ok, err := a.CreateUserApiKey(key("other-owner", 2)); !ok || err != nil {
				t.Fatal("other owner blocked", ok, err)
			}
			// Collision never overwrites an existing credential, even when the owner is full.
			if ok, err := a.CreateUserApiKey(key("boundary", 2)); ok || err != nil {
				t.Fatal("collision", ok, err)
			}
			got, _ := a.GetApiKey("boundary")
			if got.UserId != 1 {
				t.Fatal("collision overwrote owner")
			}
			stale := got
			stale.FriendlyName = "changed"
			stale.Permissions = models.ApiPermUpload
			stale.UserId = 999
			stale.IsSystemKey = true
			if ok, err := a.UpdateApiKeyProperties(stale); !ok || err != nil {
				t.Fatal("update failed", ok, err)
			}
			updated, _ := a.GetApiKey(stale.Id)
			if updated.UserId != 1 || updated.IsSystemKey || updated.FriendlyName != "changed" || updated.Permissions != models.ApiPermUpload {
				t.Fatal("update changed immutable properties")
			}
			a.DeleteApiKey("boundary")
			if ok, err := b.UpdateApiKeyProperties(stale); ok || err != nil {
				t.Fatal("stale update revived removed key", ok, err)
			}
			if _, ok := a.GetApiKey(stale.Id); ok {
				t.Fatal("deleted key revived")
			}

			if ok, err := b.CreateUserApiKey(key("after-delete", 1)); !ok || err != nil {
				t.Fatal("delete did not release slot", ok, err)
			}
			// Imported legacy records also count, without being silently deleted/revoked.
			legacy := key("legacy-name", 3)
			legacy.FriendlyName = strings.Repeat("x", 65536)
			a.SaveApiKey(legacy)
			if got, ok := a.GetApiKey(legacy.Id); !ok || got.FriendlyName != legacy.FriendlyName {
				t.Fatal("legacy key changed")
			}
			// Seed global limit through the legacy migration path to exercise cross-owner cap.
			// Count our known fixture records rather than the legacy listing helper:
			// that helper interprets special Redis prefixes as glob expressions.
			ordinary := models.ApiKeyMaxPerUser + 2 // owner 1, owner 2, legacy owner 3
			for i := ordinary; i < models.ApiKeyMaxGlobal; i++ {
				a.SaveApiKey(key(fmt.Sprintf("global-%d", i), 100+i))
			}
			a.DeleteApiKey(fmt.Sprintf("global-%d", models.ApiKeyMaxGlobal-1))

			globalResults := make(chan error, 4)
			globalWins := make(chan bool, 4)
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					store := a
					if i%2 == 1 {
						store = b
					}
					ok, err := store.CreateUserApiKey(key(fmt.Sprintf("global-race-%d", i), 80000+i))
					if ok {
						globalWins <- true
					}
					globalResults <- err
				}(i)
			}
			wg.Wait()
			close(globalResults)
			if len(globalWins) != 1 {
				t.Fatalf("global quota admitted %d racers, want one", len(globalWins))
			}
			for err := range globalResults {
				if err != nil && !errors.Is(err, models.ErrApiKeyQuotaExceeded) {
					t.Fatal(err)
				}
			}
			if ok, err := b.CreateUserApiKey(key("global-overflow", 90000)); ok || !errors.Is(err, models.ErrApiKeyQuotaExceeded) {
				t.Fatal("global quota", ok, err)
			}
		})
	}
}
