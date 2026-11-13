package presign

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
)

func isolatePresignRegistry(t *testing.T) {
	t.Helper()
	mutex.Lock()
	previousURLs := presignedUrls
	previousBytes := retainedBytes
	previousUsage := usageByUser
	presignedUrls = make(map[string]storedPresign)
	retainedBytes = 0
	usageByUser = make(map[int]userUsage)
	mutex.Unlock()
	t.Cleanup(func() {
		mutex.Lock()
		presignedUrls = previousURLs
		retainedBytes = previousBytes
		usageByUser = previousUsage
		mutex.Unlock()
	})
}

func TestSEC021PresignSingleConsumer(t *testing.T) {
	id := rand.Text()
	Save(models.Presign{Id: id, Expiry: time.Now().Add(time.Minute).Unix(), FileIds: []string{"synthetic"}})
	t.Cleanup(func() { Delete(id) })
	start := make(chan struct{})
	var wg sync.WaitGroup
	var winners atomic.Int32
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if Consume(id) {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("consumers=%d want=1", winners.Load())
	}
	if _, ok := Get(id); ok || Consume(id) {
		t.Fatal("consumed capability retained")
	}
}

func TestSEC021PresignExpiry(t *testing.T) {
	for _, expiry := range []int64{time.Now().Add(-time.Minute).Unix(), time.Now().Unix()} {
		id := rand.Text()
		Save(models.Presign{Id: id, Expiry: expiry})
		t.Cleanup(func() { Delete(id) })
		if _, ok := Get(id); ok || Consume(id) {
			t.Fatal("expired capability accepted")
		}
	}
	if Consume(rand.Text()) {
		t.Fatal("missing capability accepted")
	}
}

func TestPresignRegistryBoundsRetainedState(t *testing.T) {
	isolatePresignRegistry(t)
	future := time.Now().Add(time.Minute).Unix()
	if err := Save(models.Presign{Id: "oversized", Expiry: future,
		Filename: strings.Repeat("x", maxPresignFilenameBytes+1)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized filename error=%v want=%v", err, ErrInvalid)
	}
	for i := 0; i < maxPresignsPerUser; i++ {
		if err := Save(models.Presign{Id: fmt.Sprintf("bounded-%d", i), Expiry: future,
			FileIds: []string{"ordinary-file"}, UserId: 42, ApiKeyId: "ordinary-key"}); err != nil {
			t.Fatalf("ordinary presign %d rejected: %v", i, err)
		}
	}
	if err := Save(models.Presign{Id: "excess", Expiry: future,
		FileIds: []string{"ordinary-file"}, UserId: 42, ApiKeyId: "ordinary-key"}); !errors.Is(err, ErrCapacity) {
		t.Fatalf("issuer capacity error=%v want=%v", err, ErrCapacity)
	}
	mutex.RLock()
	defer mutex.RUnlock()
	if len(presignedUrls) != maxPresignsPerUser || retainedBytes > maxPresignBytesPerUser {
		t.Fatalf("retained entries=%d bytes=%d", len(presignedUrls), retainedBytes)
	}
}

func TestPresignRegistryClonesLegitimateGrant(t *testing.T) {
	isolatePresignRegistry(t)
	fileIDs := []string{"ordinary-file"}
	grant := models.Presign{Id: "ordinary", Expiry: time.Now().Add(time.Minute).Unix(),
		FileIds: fileIDs, Filename: "ordinary.zip", UserId: 42, ApiKeyId: "ordinary-key", Archive: true}
	if err := Save(grant); err != nil {
		t.Fatalf("ordinary presign rejected: %v", err)
	}
	fileIDs[0] = "caller-mutation"
	first, ok := Get(grant.Id)
	if !ok || first.FileIds[0] != "ordinary-file" || first.Filename != grant.Filename || !first.IsArchive() {
		t.Fatal("stored grant did not preserve ordinary presign behavior")
	}
	first.FileIds[0] = "reader-mutation"
	second, ok := Get(grant.Id)
	if !ok || second.FileIds[0] != "ordinary-file" {
		t.Fatal("returned grant exposed retained registry state")
	}
}
