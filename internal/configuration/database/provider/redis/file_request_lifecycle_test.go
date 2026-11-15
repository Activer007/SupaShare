//go:build test

package redis

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
)

func n13Pair(id string, owner int) (models.FileRequest, models.ApiKey) {
	r := models.FileRequest{Id: id, UserId: owner, Name: id, CreationDate: time.Now().Unix(), Expiry: time.Now().Unix() + 3600, ApiKey: "key-" + id}
	return r, models.ApiKey{Id: r.ApiKey, PublicId: "public-" + id, UserId: owner, UploadRequestId: id, Expiry: r.Expiry}
}
func TestFileRequestAtomicQuotaAndPage(t *testing.T) {
	p := sec030RedisNew(t).db
	ctx := context.Background()
	var wg sync.WaitGroup
	var accepted atomic.Int32
	for i := 0; i < 110; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r, k := n13Pair(fmt.Sprintf("r%03d", i), 1)
			ok, e := p.CreateFileRequestLimited(ctx, r, k)
			if e != nil && e != models.ErrFileRequestQuota {
				t.Error(e)
			}
			if ok {
				accepted.Add(1)
			}
		}(i)
	}
	wg.Wait()
	if accepted.Load() != 100 {
		t.Fatalf("accepted %d", accepted.Load())
	}
	if len(p.GetAllApiKeys()) != 100 {
		t.Fatal("partial pair")
	}
	a, e := p.ListFileRequestsPage(ctx, 1, "", 50)
	if e != nil || len(a) != 50 {
		t.Fatal(len(a), e)
	}
	b, e := p.ListFileRequestsPage(ctx, 1, models.FileRequestCursor(a[49]), 51)
	if e != nil || len(b) != 50 {
		t.Fatal(len(b), e)
	}
	if models.FileRequestCursor(a[49]) <= models.FileRequestCursor(b[0]) {
		t.Fatal("bad pagination")
	}
	c, e := p.ListFileRequestsPage(ctx, 2, "", 10)
	if e != nil || len(c) != 0 {
		t.Fatal("owner leak", e)
	}
}
func TestFileRequestRetentionAndIndexedFiles(t *testing.T) {
	p := sec030RedisNew(t).db
	ctx := context.Background()
	r, k := n13Pair("retained", 1)
	if ok, e := p.CreateFileRequestLimited(ctx, r, k); !ok || e != nil {
		t.Fatal(ok, e)
	}
	f := models.File{Id: "uploaded", UserId: 1, UploadRequestId: r.Id}
	p.SaveMetaData(f)
	files, e := p.ReadFileRequestFiles(ctx, r.Id)
	if e != nil || len(files) != 1 {
		t.Fatal(files, e)
	}
	now := r.Expiry + models.FileRequestRetentionSeconds + 1
	if n, e := p.PruneFileRequests(ctx, now, 100); n != 0 || e != nil {
		t.Fatal(n, e)
	}
	if _, ok := p.GetApiKey(k.Id); ok {
		t.Fatal("key not revoked")
	}
	if _, ok := p.GetFileRequest(r.Id); !ok {
		t.Fatal("request with files deleted")
	}
	if _, ok := p.GetMetaDataById(f.Id); !ok {
		t.Fatal("file deleted")
	}
	p.DeleteMetaData(f.Id)
	if n, e := p.PruneFileRequests(ctx, now+3601, 100); n != 1 || e != nil {
		t.Fatal(n, e)
	}
	if ok, e := p.UpdateFileRequest(ctx, r, r); ok || e != nil {
		t.Fatal("resurrected", ok, e)
	}
}
func TestFileRequestUpgradeAndOrphanCleanup(t *testing.T) {
	scene := sec030RedisNew(t)
	p := scene.db
	ctx := context.Background()
	r, k := n13Pair("legacy", 1)
	r.Expiry = 0
	k.Expiry = 0
	p.SaveFileRequest(r)
	p.SaveApiKey(k)
	p.SaveMetaData(models.File{Id: "legacy-file", UploadRequestId: r.Id})
	scene.conn.Do("DEL", p.dbPrefix+keyFileRequestIndexes)
	if e := p.initFileRequestIndexes(); e != nil {
		t.Fatal(e)
	}
	files, e := p.ReadFileRequestFiles(ctx, r.Id)
	if e != nil || len(files) != 1 {
		t.Fatal(e, files)
	}
	_, orphan := n13Pair("missing", 1)
	p.SaveApiKey(orphan)
	if _, e := p.PruneFileRequests(ctx, time.Now().Unix(), 100); e != nil {
		t.Fatal(e)
	}
	if _, ok := p.GetApiKey(orphan.Id); ok {
		t.Fatal("orphan not removed")
	}
	if _, ok := p.GetApiKey(k.Id); !ok {
		t.Fatal("legacy permanent key removed")
	}
}
func TestFileRequestPairWrongTypeNoPartial(t *testing.T) {
	scene := sec030RedisNew(t)
	p := scene.db
	r, k := n13Pair("wrongtype", 1)
	scene.conn.Do("SET", p.dbPrefix+frIndex+"expiry", "bad")
	if ok, e := p.CreateFileRequestLimited(context.Background(), r, k); ok || e == nil {
		t.Fatal(ok, e)
	}
	n, _ := redigo.Int(scene.conn.Do("EXISTS", p.dbPrefix+prefixApiKeys+k.Id))
	if n != 0 {
		t.Fatal("partial key")
	}
	if _, ok := p.GetFileRequest(r.Id); ok {
		t.Fatal("partial request")
	}
}

func TestFileRequestUpdateCASAndExpiry(t *testing.T) {
	scene := sec030RedisNew(t)
	p := scene.db
	ctx := context.Background()
	r, k := n13Pair("edit", 1)
	if ok, e := p.CreateFileRequestLimited(ctx, r, k); !ok || e != nil {
		t.Fatal(ok, e)
	}
	updated := r
	updated.Expiry += 3600
	updated.Name = "new"
	if ok, e := p.UpdateFileRequest(ctx, r, updated); !ok || e != nil {
		t.Fatal(ok, e)
	}
	key, ok := p.GetApiKey(k.Id)
	if !ok || key.Expiry != updated.Expiry {
		t.Fatal("expiry out of sync", key)
	}
	if ok, e := p.UpdateFileRequest(ctx, r, updated); ok || e != nil {
		t.Fatal("stale update accepted", ok, e)
	}
	p.DeleteApiKey(k.Id)
	next := updated
	next.Expiry++
	if ok, e := p.UpdateFileRequest(ctx, updated, next); ok || e == nil {
		t.Fatal("revived key", ok, e)
	}
}

func TestFileRequestPruneKeepsOutstandingUpload(t *testing.T) {
	p := sec030RedisNew(t).db
	ctx := context.Background()
	r, k := n13Pair("request", 1)
	if ok, e := p.CreateFileRequestLimited(ctx, r, k); !ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, e := p.ApplyUploadQuota(ctx, sec030RedisChange("reserved", 1)); e != nil {
		t.Fatal(e)
	}
	if n, e := p.PruneFileRequests(ctx, r.Expiry+models.FileRequestRetentionSeconds+1, 100); n != 0 || e != nil {
		t.Fatal(n, e)
	}
	if _, ok := p.GetFileRequest(r.Id); !ok {
		t.Fatal("outstanding reservation lost its request")
	}
}
