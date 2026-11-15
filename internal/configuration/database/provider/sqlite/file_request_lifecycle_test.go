package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
)

func n13Pair(i, owner int) (models.FileRequest, models.ApiKey) {
	r := models.FileRequest{Id: fmt.Sprintf("request-%d", i), UserId: owner, CreationDate: 1000, Expiry: 2000, Name: "request", ApiKey: fmt.Sprintf("request-key-%d", i)}
	k := models.ApiKey{Id: r.ApiKey, PublicId: fmt.Sprintf("public-%d", i), UserId: owner, UploadRequestId: r.Id, FriendlyName: "upload"}
	return r, k
}
func TestN13SQLiteAtomicPairAndQuota(t *testing.T) {
	a, b := n12Provider(t)
	ctx := context.Background()
	for i := 0; i < 99; i++ {
		r, k := n13Pair(i, 1)
		ok, err := a.CreateFileRequestLimited(ctx, r, k)
		if !ok || err != nil {
			t.Fatal(i, ok, err)
		}
	}
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 100; i < 120; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p := a
			if i%2 == 0 {
				p = b
			}
			r, k := n13Pair(i, 1)
			ok, err := p.CreateFileRequestLimited(ctx, r, k)
			if ok {
				accepted.Add(1)
			} else if !errors.Is(err, models.ErrFileRequestQuota) {
				t.Errorf("quota: %v", err)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d", accepted.Load())
	}
	var requests, keys int
	a.sqliteDb.QueryRow("SELECT count(*) FROM UploadRequests").Scan(&requests)
	a.sqliteDb.QueryRow("SELECT count(*) FROM ApiKeys WHERE UploadRequestId<>''").Scan(&keys)
	if requests != 100 || keys != 100 {
		t.Fatal(requests, keys)
	}
	r, k := n13Pair(0, 1)
	if ok, err := a.CreateFileRequestLimited(ctx, r, k); ok || err != nil {
		t.Fatal("collision", ok, err)
	}
	r, k = n13Pair(300, 2)
	if ok, err := b.CreateFileRequestLimited(ctx, r, k); !ok || err != nil {
		t.Fatal("other owner", ok, err)
	}
	// Public key collision cannot leave a request behind.
	r, k = n13Pair(301, 2)
	k.PublicId = "public-300"
	if ok, err := b.CreateFileRequestLimited(ctx, r, k); ok || err != nil {
		t.Fatal("public collision", ok, err)
	}
	if _, ok := a.GetFileRequest(r.Id); ok {
		t.Fatal("partial request")
	}
}
func TestN13SQLiteLegacyOrphanKeyQuota(t *testing.T) {
	p, _ := n12Provider(t)
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		_, k := n13Pair(i, 1)
		p.SaveApiKey(k)
	}
	r, k := n13Pair(200, 1)
	if ok, err := p.CreateFileRequestLimited(ctx, r, k); ok || !errors.Is(err, models.ErrFileRequestQuota) {
		t.Fatal(ok, err)
	}
	if _, err := p.PruneFileRequests(ctx, 3000, 10); err != nil {
		t.Fatal(err)
	}
	var n int
	p.sqliteDb.QueryRow("SELECT count(*) FROM ApiKeys").Scan(&n)
	if n != 90 {
		t.Fatal(n)
	}
	if ok, err := p.CreateFileRequestLimited(ctx, r, k); !ok || err != nil {
		t.Fatal(ok, err)
	}
}
func TestN13SQLiteLifecycleKeepsFilesAndPermanent(t *testing.T) {
	p, _ := n12Provider(t)
	ctx := context.Background()
	r0, k0 := n13Pair(0, 1)
	r1, k1 := n13Pair(1, 1)
	r2, k2 := n13Pair(2, 1)
	r2.Expiry = 0
	for _, v := range []struct {
		r models.FileRequest
		k models.ApiKey
	}{{r0, k0}, {r1, k1}, {r2, k2}} {
		if ok, err := p.CreateFileRequestLimited(ctx, v.r, v.k); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	p.SaveMetaData(models.File{Id: "source-file", Name: "source", UserId: 1, UploadRequestId: r1.Id, SizeBytes: 50})
	if n, err := p.PruneFileRequests(ctx, 2000, 10); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if _, ok := p.GetApiKey(k0.Id); ok {
		t.Fatal("expired invitation retained")
	}
	if _, ok := p.GetApiKey(k2.Id); !ok {
		t.Fatal("permanent key removed")
	}
	if n, err := p.PruneFileRequests(ctx, 2000+7*86400, 10); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	if _, ok := p.GetFileRequest(r0.Id); ok {
		t.Fatal("empty retained")
	}
	if _, ok := p.GetFileRequest(r1.Id); !ok {
		t.Fatal("nonempty removed")
	}
	if _, ok := p.GetMetaDataById("source-file"); !ok {
		t.Fatal("source deleted")
	}
	files, err := p.ReadFileRequestFiles(ctx, r1.Id)
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	changed := r1
	changed.Expiry = 10000000
	if ok, err := p.UpdateFileRequest(ctx, r1, changed); ok || err != nil {
		t.Fatal("key reissued", ok, err)
	}
	changed = r1
	changed.Notes = "retained audit note"
	if ok, err := p.UpdateFileRequest(ctx, r1, changed); !ok || err != nil {
		t.Fatal("note update", ok, err)
	}
	if _, ok := p.GetApiKey(k1.Id); ok {
		t.Fatal("key resurrected")
	}
}
func TestN13SQLiteUpdateAndPage(t *testing.T) {
	p, _ := n12Provider(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		r, k := n13Pair(i, 1)
		r.Expiry = time.Now().Unix() + 2000
		if ok, err := p.CreateFileRequestLimited(ctx, r, k); !ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	r, k := n13Pair(99, 2)
	if ok, err := p.CreateFileRequestLimited(ctx, r, k); !ok || err != nil {
		t.Fatal(ok, err)
	}
	first, err := p.ListFileRequestsPage(ctx, 1, "", 2)
	if err != nil || len(first) != 2 || first[0].Id != "request-4" {
		t.Fatal(first, err)
	}
	cursor := models.FileRequestCursor(first[1])
	p.DeleteFileRequest(first[1])
	next, err := p.ListFileRequestsPage(ctx, 1, cursor, 2)
	if err != nil || len(next) != 2 || next[0].Id != "request-2" {
		t.Fatal(next, err)
	}
	old := first[0]
	current := old
	current.Expiry = time.Now().Unix() + 9000
	current.Name = "updated"
	if ok, err := p.UpdateFileRequest(ctx, old, current); !ok || err != nil {
		t.Fatal(ok, err)
	}
	key, ok := p.GetApiKey(current.ApiKey)
	if !ok || key.Expiry != current.Expiry {
		t.Fatal(key, ok)
	}
	stale := old
	stale.Notes = "stale"
	if ok, err := p.UpdateFileRequest(ctx, old, stale); ok || err != nil {
		t.Fatal("stale overwrite", ok, err)
	}
	p.DeleteFileRequest(current)
	if ok, err := p.UpdateFileRequest(ctx, current, current); ok || err != nil {
		t.Fatal("resurrection", ok, err)
	}
	all, err := p.ListFileRequestsPage(ctx, 0, "", 100)
	if err != nil || len(all) != 4 {
		t.Fatal(all, err)
	}
}
func TestN13SQLiteUpgradeRetainsLegacy(t *testing.T) {
	p, _ := n12Provider(t)
	r, k := n13Pair(1, 1)
	r.Expiry = 0
	p.SaveFileRequest(r)
	p.SaveApiKey(k)
	for _, name := range []string{"RequestOwnerPage", "RequestPage", "RequestExpiry", "RequestFiles", "RequestKeys", "RequestKeysOwner"} {
		if _, err := p.sqliteDb.Exec("DROP INDEX " + name); err != nil {
			t.Fatal(err)
		}
	}
	p.SetDbVersion(23)
	p.Upgrade(23)
	p.Upgrade(23)
	var n int
	if err := p.sqliteDb.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name IN ('RequestOwnerPage','RequestPage','RequestExpiry','RequestFiles','RequestKeys','RequestKeysOwner')").Scan(&n); err != nil || n != 6 {
		t.Fatal(n, err)
	}
	got, ok := p.GetFileRequest(r.Id)
	if !ok || got.Expiry != 0 || got.ApiKey != r.ApiKey {
		t.Fatal(got, ok)
	}
}

func TestN13SQLiteGlobalQuotaAndRollback(t *testing.T) {
	for _, kind := range []string{"requests", "keys"} {
		t.Run(kind, func(t *testing.T) {
			p, _ := n12Provider(t)
			var query string
			if kind == "requests" {
				query = `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<10000) INSERT INTO UploadRequests(id,name,userid,expiry,maxFiles,maxSize,creation,apiKey,note,allowedDownloads) SELECT 'legacy-'||i,'',i+100,0,0,0,1,'key-'||i,'',0 FROM n`
			} else {
				query = `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<10000) INSERT INTO ApiKeys(Id,FriendlyName,LastUsed,Permissions,Expiry,IsSystemKey,UserId,PublicId,UploadRequestId) SELECT 'key-'||i,'',0,0,0,0,i+100,'public-'||i,'legacy-'||i FROM n`
			}
			if _, err := p.sqliteDb.Exec(query); err != nil {
				t.Fatal(err)
			}
			r, k := n13Pair(10001, 1)
			if ok, err := p.CreateFileRequestLimited(context.Background(), r, k); ok || !errors.Is(err, models.ErrFileRequestQuota) {
				t.Fatal(ok, err)
			}
		})
	}
	p, _ := n12Provider(t)
	if _, err := p.sqliteDb.Exec(`CREATE TRIGGER fail_key BEFORE INSERT ON ApiKeys BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	r, k := n13Pair(1, 1)
	if ok, err := p.CreateFileRequestLimited(context.Background(), r, k); ok || err == nil {
		t.Fatal(ok, err)
	}
	if _, exists := p.GetFileRequest(r.Id); exists {
		t.Fatal("failed key write leaked request")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.CreateFileRequestLimited(ctx, r, k); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestN13SQLitePruneRetainsActiveReservations(t *testing.T) {
	p, _ := n12Provider(t)
	ctx := context.Background()
	r, k := n13Pair(1, 1)
	if ok, err := p.CreateFileRequestLimited(ctx, r, k); !ok || err != nil {
		t.Fatal(ok, err)
	}
	ledger := uploadquota.Ledger{Version: 1, Records: map[string]uploadquota.Reservation{}}
	ledger.Records["pending"] = uploadquota.Reservation{ID: "pending", UserID: 1, RequestID: r.Id, BytesKnown: true, Bytes: 1, StagingBytes: 1, CreatedAt: 1000, ExpiresAt: 4600, State: uploadquota.Reserved}
	encoded, err := uploadquota.Encode(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.sqliteDb.Exec("UPDATE UploadQuota SET State=? WHERE Id=1", encoded); err != nil {
		t.Fatal(err)
	}
	if n, err := p.PruneFileRequests(ctx, 2000+7*86400, 10); n != 0 || err != nil {
		t.Fatal(n, err)
	}
	if _, ok := p.GetFileRequest(r.Id); !ok {
		t.Fatal("active reservation lost parent")
	}
	// Expired request policy cannot be revived merely because cleanup has not run.
	r2, k2 := n13Pair(2, 1)
	if ok, err := p.CreateFileRequestLimited(ctx, r2, k2); !ok || err != nil {
		t.Fatal(ok, err)
	}
	changed := r2
	changed.Expiry = time.Now().Unix() + 1000
	if ok, err := p.UpdateFileRequest(ctx, r2, changed); ok || err != nil {
		t.Fatal("revived expired invitation", ok, err)
	}
}
