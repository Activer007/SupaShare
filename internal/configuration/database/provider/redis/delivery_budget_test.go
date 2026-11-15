package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"github.com/alicebob/miniredis/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func n12Delivery(i, owner int, now int64) models.Delivery {
	return models.Delivery{Token: fmt.Sprintf("n12-token-%032d", i), OwnerID: owner, Title: "delivery", FileIDs: []string{"one", "two"}, CreatedAt: now, ExpiresAt: now + 3600}
}
func TestN12DeliveryAtomicOwnerQuotaAndPaging(t *testing.T) {
	s := miniredis.RunT(t)
	config := models.DbConnection{HostUrl: s.Addr(), RedisPrefix: "n12_", Type: 1}
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
	ctx := context.Background()
	now := time.Now().Unix()
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < models.MaxDeliveriesPerOwner+20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := a
			if i%2 == 0 {
				p = b
			}
			ok, err := p.CreateDeliveryLimited(ctx, n12Delivery(i, 1, now), now)
			if err != nil && !errors.Is(err, models.ErrDeliveryQuota) {
				t.Error(err)
			}
			if ok {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != models.MaxDeliveriesPerOwner {
		t.Fatal("quota", accepted.Load())
	}
	if ok, err := a.CreateDeliveryLimited(ctx, n12Delivery(999, 2, now), now); !ok || err != nil {
		t.Fatal(ok, err)
	}
	seen := map[string]bool{}
	cursor := ""
	for {
		page, err := a.ListDeliveriesPage(ctx, 1, cursor, 13)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, d := range page {
			if seen[d.Token] || d.OwnerID != 1 {
				t.Fatal("duplicate or other owner")
			}
			seen[d.Token] = true
		}
		cursor = models.EncodeDeliveryCursor(page[len(page)-1])
	}
	if len(seen) != models.MaxDeliveriesPerOwner {
		t.Fatal("page total", len(seen))
	}
}
func TestN12LegacyIndexesAndRetention(t *testing.T) {
	s := miniredis.RunT(t)
	config := models.DbConnection{HostUrl: s.Addr(), RedisPrefix: "legacy_", Type: 1}
	ctx := context.Background()
	now := time.Now().Unix()
	// Existing legacy records predate indexes; permanent active links survive.
	d := n12Delivery(1, 1, now)
	d.ExpiresAt = 0
	raw, _ := json.Marshal(d)
	s.HSet(config.RedisPrefix+keyDeliveries, d.Token, string(raw))
	s.Set(config.RedisPrefix+keyDeliveriesVersion, "1")
	p, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	page, err := p.ListDeliveriesPage(ctx, 1, "", 10)
	if err != nil || len(page) != 1 {
		t.Fatal(page, err)
	}
	for i := 2; i < 5; i++ {
		x := n12Delivery(i, 1, now)
		x.ExpiresAt = now - 1
		if ok, err := p.InsertDelivery(ctx, x); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	if n, err := p.PruneDeliveries(ctx, now, 2); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := p.PruneDeliveries(ctx, now+models.DeliveryRetentionSeconds, 2); n != 2 || err != nil {
		t.Fatal(n, err)
	}
	if n, err := p.PruneDeliveries(ctx, now+models.DeliveryRetentionSeconds, 2); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if _, ok, err := p.ReadDelivery(ctx, d.Token); !ok || err != nil {
		t.Fatal("active legacy lost", err)
	}
	if ok, err := p.RevokeDelivery(ctx, d.Token, 2, now); ok || err != nil {
		t.Fatal("wrong owner revoke")
	}
	if ok, err := p.RevokeDelivery(ctx, d.Token, 1, now); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if n, err := p.PruneDeliveries(ctx, now+models.DeliveryRetentionSeconds, 2); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	page, err = p.ListDeliveriesPage(ctx, 1, "", 10)
	if err != nil || len(page) != 0 {
		t.Fatal(page, err)
	}
}
func TestN12GlobalQuota(t *testing.T) {
	s := miniredis.RunT(t)
	config := models.DbConnection{HostUrl: s.Addr(), RedisPrefix: "global_", Type: 1}
	p, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	now := time.Now().Unix()
	// Seed through the offline provider path without quota; live creation must
	// fail closed even for an otherwise empty owner's index.
	for i := 0; i < models.MaxDeliveriesGlobal; i++ {
		d := n12Delivery(i, 1, now)
		raw, _ := json.Marshal(d)
		s.HSet(config.RedisPrefix+keyDeliveries, d.Token, string(raw))
	}
	if ok, err := p.CreateDeliveryLimited(context.Background(), n12Delivery(99999, 2, now), now); ok || !errors.Is(err, models.ErrDeliveryQuota) {
		t.Fatal(ok, err)
	}
}

func TestN12IndexMigrationIdempotent(t *testing.T) {
	s := miniredis.RunT(t)
	config := models.DbConnection{HostUrl: s.Addr(), RedisPrefix: "restart_", Type: 1}
	ctx := context.Background()
	now := time.Now().Unix()
	a, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	for i := 0; i < 5; i++ {
		if ok, err := a.InsertDelivery(ctx, n12Delivery(i, 1, now)); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	s.Del(config.RedisPrefix + keyDeliveryIndexes)
	b, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	b.Upgrade(14)
	for i := 0; i < 5; i++ {
		if ok, err := a.RevokeDelivery(ctx, n12Delivery(i, 1, now).Token, 1, now); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	if n, err := b.PruneDeliveries(ctx, now+models.DeliveryRetentionSeconds, 10); n != 5 || err != nil {
		t.Fatal(n, err)
	}
	s.Del(config.RedisPrefix + keyDeliveryIndexes)
	c, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Upgrade(14)
	page, err := c.ListDeliveriesPage(ctx, 1, "", 101)
	if err != nil || len(page) != 0 {
		t.Fatal(page, err)
	}
	if s.Exists(config.RedisPrefix + keyDeliveryOwners + "1") {
		t.Fatal("stale owner index")
	}
}

func TestN12DeliveryCursorOrderAndDeletedCursor(t *testing.T) {
	s := miniredis.RunT(t)
	p, err := New(models.DbConnection{HostUrl: s.Addr(), RedisPrefix: "cursor_", Type: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx := context.Background()
	now := time.Now().Unix()
	oldest := n12Delivery(99, 1, now-100)
	newerA := n12Delivery(1, 1, now)
	newerB := n12Delivery(2, 1, now)
	for _, d := range []models.Delivery{oldest, newerA, newerB} {
		if ok, err := p.InsertDelivery(ctx, d); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	first, err := p.ListDeliveriesPage(ctx, 1, "", 1)
	if err != nil || len(first) != 1 || first[0].Token != newerB.Token {
		t.Fatal(first, err)
	}
	cursor := models.EncodeDeliveryCursor(first[0])
	if ok, err := p.RevokeDelivery(ctx, newerB.Token, 1, now); !ok || err != nil {
		t.Fatal(ok, err)
	}
	// Expire the revoked cursor only, leaving the other records active.
	if n, err := p.PruneDeliveries(ctx, now+models.DeliveryRetentionSeconds, 1); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	rest, err := p.ListDeliveriesPage(ctx, 1, cursor, 10)
	if err != nil || len(rest) != 2 || rest[0].Token != newerA.Token || rest[1].Token != oldest.Token {
		t.Fatal(rest, err)
	}
	if _, err := p.ListDeliveriesPage(ctx, 1, "bad", 10); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestN12MigrationOnlyAllowsValidIndexMarker(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*miniredis.Miniredis)
		want  error
	}{
		{"valid", func(s *miniredis.Miniredis) {}, nil},
		{"bad marker", func(s *miniredis.Miniredis) { s.Set("empty_"+keyDeliveryIndexes, "2") }, uploadquota.ErrSchema},
		{"owner data", func(s *miniredis.Miniredis) { s.ZAdd("empty_"+keyDeliveryOwners+"1", 0, "orphan") }, uploadquota.ErrConflict},
		{"expiry data", func(s *miniredis.Miniredis) { s.ZAdd("empty_"+keyDeliveryExpiry, 1, "orphan") }, uploadquota.ErrConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := miniredis.RunT(t)
			p, err := New(models.DbConnection{HostUrl: s.Addr(), RedisPrefix: "empty_", Type: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			tc.setup(s)
			err = p.CheckEmptyForMigration(context.Background())
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestN12Schema14UpgradeIndexesExistingRecords(t *testing.T) {
	s := miniredis.RunT(t)
	config := models.DbConnection{HostUrl: s.Addr(), RedisPrefix: "upgrade14_", Type: 1}
	ctx := context.Background()
	now := time.Now().Unix()
	s.Set(config.RedisPrefix+keyDbVersion, "14")
	s.Set(config.RedisPrefix+keyDeliveriesVersion, "1")
	for i := 0; i < models.MaxDeliveriesPerOwner; i++ {
		d := n12Delivery(i, 1, now)
		d.ExpiresAt = 0
		raw, _ := json.Marshal(d)
		s.HSet(config.RedisPrefix+keyDeliveries, d.Token, string(raw))
	}
	p, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := p.ListDeliveriesPage(ctx, 1, "", 10); !errors.Is(err, uploadquota.ErrSchema) {
		t.Fatal("unupgraded online access", err)
	}
	all, err := p.ReadAllDeliveries(ctx)
	if err != nil || len(all) != models.MaxDeliveriesPerOwner {
		t.Fatal("legacy offline read", len(all), err)
	}
	p.Upgrade(14)
	p.SetDbVersion(DatabaseSchemeVersion)
	page, err := p.ListDeliveriesPage(ctx, 1, "", 10)
	if err != nil || len(page) != 10 {
		t.Fatal(page, err)
	}
	if ok, err := p.CreateDeliveryLimited(ctx, n12Delivery(1000, 1, now), now); ok || !errors.Is(err, models.ErrDeliveryQuota) {
		t.Fatal("legacy counts", ok, err)
	}
	s.Del(config.RedisPrefix + keyDeliveryIndexes)
	if _, err := p.ListDeliveriesPage(ctx, 1, "", 10); !errors.Is(err, uploadquota.ErrSchema) {
		t.Fatal("missing index marker accepted", err)
	}
}
