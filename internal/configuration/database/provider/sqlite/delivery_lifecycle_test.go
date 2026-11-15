package sqlite

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

func n12Delivery(i, owner int) models.Delivery {
	return models.Delivery{Token: fmt.Sprintf("delivery-%024d", i), OwnerID: owner, Title: "test", FileIDs: []string{"one", "two"}, CreatedAt: 1000, ExpiresAt: 2000}
}
func n12Provider(t *testing.T) (DatabaseProvider, DatabaseProvider) {
	t.Helper()
	cfg := models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "n12.sqlite")}
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}
func TestN12SQLiteOwnerQuotaAcrossProviders(t *testing.T) {
	a, b := n12Provider(t)
	ctx := context.Background()
	for i := 0; i < models.MaxDeliveriesPerOwner-1; i++ {
		if ok, err := a.InsertDelivery(ctx, n12Delivery(i, 1)); err != nil || !ok {
			t.Fatal(ok, err)
		}
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			p := a
			if i%2 == 1 {
				p = b
			}
			ok, err := p.CreateDeliveryLimited(ctx, n12Delivery(1000+i, 1), 1500)
			if ok {
				wins.Add(1)
			} else if !errors.Is(err, models.ErrDeliveryQuota) {
				t.Errorf("unexpected rejection %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("admitted %d", wins.Load())
	}
	if ok, err := a.CreateDeliveryLimited(ctx, n12Delivery(0, 1), 1500); ok || err != nil {
		t.Fatalf("collision at cap: %v %v", ok, err)
	}
	if ok, err := b.CreateDeliveryLimited(ctx, n12Delivery(2000, 2), 1500); !ok || err != nil {
		t.Fatalf("other owner: %v %v", ok, err)
	}
}
func TestN12SQLiteGlobalQuota(t *testing.T) {
	a, b := n12Provider(t)
	ctx := context.Background()
	// Fixture uses the explicitly unrestricted offline import path.
	tx, err := a.sqliteDb.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < models.MaxDeliveriesGlobal-1; i++ {
		d := n12Delivery(i, i+1)
		_, err = tx.Exec(`INSERT INTO DeliveryLists VALUES(?,?,?,?,'["one","two"]',?,?,?)`, d.Token, d.OwnerID, d.Title, d.Note, d.CreatedAt, d.ExpiresAt, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := a
			if i%2 == 1 {
				p = b
			}
			ok, err := p.CreateDeliveryLimited(ctx, n12Delivery(20000+i, 20000+i), 1500)
			if ok {
				wins.Add(1)
			} else if !errors.Is(err, models.ErrDeliveryQuota) {
				t.Errorf("%v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("global admitted %d", wins.Load())
	}
}
func TestN12SQLitePagePruneAndLegacy(t *testing.T) {
	a, _ := n12Provider(t)
	ctx := context.Background()
	now := int64(1000000)
	for i := 0; i < 6; i++ {
		d := n12Delivery(i, 1)
		d.ExpiresAt = 0
		if i == 1 {
			d.ExpiresAt = now - models.DeliveryRetentionSeconds
		}
		if i == 2 {
			d.RevokedAt = now - models.DeliveryRetentionSeconds
		}
		if i == 3 {
			d.ExpiresAt = now - models.DeliveryRetentionSeconds + 1
		}
		if i == 4 {
			d.OwnerID = 2
		}
		if ok, err := a.InsertDelivery(ctx, d); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	page, err := a.ListDeliveriesPage(ctx, 1, "", 2)
	if err != nil || len(page) != 2 || page[0].Token != n12Delivery(5, 1).Token || page[1].Token != n12Delivery(3, 1).Token {
		t.Fatalf("page: %+v %v", page, err)
	}
	// Cursor is valid even after its record is deleted.
	_, err = a.sqliteDb.Exec(`DELETE FROM DeliveryLists WHERE Token=?`, page[1].Token)
	if err != nil {
		t.Fatal(err)
	}
	next, err := a.ListDeliveriesPage(ctx, 1, models.EncodeDeliveryCursor(page[1]), 100)
	if err != nil || len(next) != 3 {
		t.Fatalf("next %+v %v", next, err)
	}
	for i := 0; i < 2; i++ {
		n, err := a.PruneDeliveries(ctx, now, 1)
		if err != nil || n != 1 {
			t.Fatal(n, err)
		}
	}
	if n, err := a.PruneDeliveries(ctx, now, 1); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if _, found, err := a.ReadDelivery(ctx, n12Delivery(0, 1).Token); !found || err != nil {
		t.Fatal("legacy permanent was removed", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := a.CreateDeliveryLimited(canceled, n12Delivery(100, 1), now); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := a.ListDeliveriesPage(ctx, 1, "", 102); err == nil {
		t.Fatal("unbounded page")
	}
	if _, err := a.PruneDeliveries(ctx, now, 1001); err == nil {
		t.Fatal("unbounded prune")
	}
}

func TestN12SQLiteCreatePrunesRetainedExpired(t *testing.T) {
	p, _ := n12Provider(t)
	ctx := context.Background()
	now := int64(1000000)
	for i := 0; i < models.MaxDeliveriesPerOwner; i++ {
		d := n12Delivery(i, 1)
		d.ExpiresAt = now - models.DeliveryRetentionSeconds
		if ok, err := p.InsertDelivery(ctx, d); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	fresh := n12Delivery(1000, 1)
	fresh.ExpiresAt = now + 1000
	if ok, err := p.CreateDeliveryLimited(ctx, fresh, now); !ok || err != nil {
		t.Fatal(ok, err)
	}
	page, err := p.ListDeliveriesPage(ctx, 1, "", 100)
	if err != nil || len(page) != 1 || page[0].Token != fresh.Token {
		t.Fatalf("prune did not free quota: %+v %v", page, err)
	}
}

func TestN12SQLitePageOrdersCreationBeforeToken(t *testing.T) {
	p, _ := n12Provider(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		d := n12Delivery(i, 1)
		d.CreatedAt = 1000 + int64(3-i)
		if ok, err := p.InsertDelivery(ctx, d); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	var all []models.Delivery
	cursor := ""
	for {
		page, err := p.ListDeliveriesPage(ctx, 1, cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		all = append(all, page...)
		cursor = models.EncodeDeliveryCursor(page[0])
	}
	if len(all) != 4 {
		t.Fatal(len(all))
	}
	for i, d := range all {
		if d.Token != n12Delivery(i, 1).Token {
			t.Fatalf("wrong order %+v", all)
		}
	}
	if _, err := p.ListDeliveriesPage(ctx, 1, "bad-cursor", 1); err == nil {
		t.Fatal("invalid cursor accepted")
	}
}

func TestN12SQLiteUpgrade22AddsIndexesPreservesDeliveries(t *testing.T) {
	p, _ := n12Provider(t)
	ctx := context.Background()
	d := n12Delivery(1, 7)
	d.ExpiresAt = 0
	if ok, err := p.InsertDelivery(ctx, d); !ok || err != nil {
		t.Fatal(ok, err)
	}
	for _, name := range []string{"DeliveryListsOwnerPage", "DeliveryListsExpiry", "DeliveryListsRevoked"} {
		if _, err := p.sqliteDb.Exec("DROP INDEX " + name); err != nil {
			t.Fatal(err)
		}
	}
	p.SetDbVersion(22)
	p.Upgrade(p.GetDbVersion())
	p.Upgrade(22) // Retry before version commit must be idempotent.
	for _, name := range []string{"DeliveryListsOwnerPage", "DeliveryListsExpiry", "DeliveryListsRevoked"} {
		var n int
		if err := p.sqliteDb.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&n); err != nil || n != 1 {
			t.Fatalf("missing %s: %d %v", name, n, err)
		}
	}
	got, found, err := p.ReadDelivery(ctx, d.Token)
	if !found || err != nil || got.OwnerID != d.OwnerID || got.ExpiresAt != 0 || got.Title != d.Title {
		t.Fatalf("legacy data changed %+v %v", got, err)
	}
	if p.GetSchemaVersion() != 23 {
		t.Fatal(p.GetSchemaVersion())
	}
}
