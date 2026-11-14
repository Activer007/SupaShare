package sqlite

import (
	"errors"
	"fmt"
	"github.com/Activer007/SupaShare/internal/models"
	"path/filepath"
	"testing"
	"time"
)

func TestN04TemporaryCleanupBounded(t *testing.T) {
	p, err := New(models.DbConnection{HostUrl: filepath.Join(t.TempDir(), "temporary.sqlite")})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	now := time.Now().Unix()
	for i := 0; i < 600; i++ {
		p.SaveApiKey(models.ApiKey{Id: fmt.Sprint(i), PublicId: "public-" + fmt.Sprint(i), UserId: 1, IsSystemKey: true, Expiry: now})
	}
	p.SaveApiKey(models.ApiKey{Id: "ordinary", PublicId: "ordinary-public", UserId: 1, Expiry: now})
	p.SaveApiKey(models.ApiKey{Id: "request", PublicId: "request-public", UserId: 1, IsSystemKey: true, UploadRequestId: "request", Expiry: now})
	k := models.ApiKey{Id: "new", PublicId: "new-public", UserId: 1, IsSystemKey: true, Expiry: now + 300}
	for attempt, want := range []int{344, 88, 0} {
		ok, err := p.CreateTemporaryApiKey(k)
		if attempt < 2 && (ok || !errors.Is(err, models.ErrTemporaryApiKeyQuotaExceeded)) {
			t.Fatal("legacy backlog should fail closed", ok, err)
		}
		if attempt == 2 && (!ok || err != nil) {
			t.Fatal("cleanup did not recover", ok, err)
		}
		var count int
		if err := p.sqliteDb.QueryRow("SELECT COUNT(*) FROM ApiKeys WHERE IsSystemKey=1 AND UploadRequestId='' AND Expiry<=?", now).Scan(&count); err != nil || count != want {
			t.Fatal("unbounded cleanup", count, want, err)
		}
	}
	for _, id := range []string{"ordinary", "request"} {
		if _, ok := p.GetApiKey(id); !ok {
			t.Fatal("removed unrelated key", id)
		}
	}
}
