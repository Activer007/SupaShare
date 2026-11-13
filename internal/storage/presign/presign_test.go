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

func presignWithBytes(id string, userID, size int) models.Presign {
	return models.Presign{Id: id, UserId: userID, Expiry: time.Now().Add(10 * time.Minute).Unix(),
		ApiKeyId: strings.Repeat("k", size-presignEntryBaseBytes-len(id))}
}

func assertPresignUsage(t *testing.T, entries, bytes int, users map[int]userUsage) {
	t.Helper()
	mutex.RLock()
	defer mutex.RUnlock()
	if len(presignedUrls) != entries || retainedBytes != bytes {
		t.Fatalf("registry entries/bytes=%d/%d, want=%d/%d", len(presignedUrls), retainedBytes, entries, bytes)
	}
	if len(usageByUser) != len(users) {
		t.Fatalf("issuer count=%d, want=%d", len(usageByUser), len(users))
	}
	for id, expected := range users {
		if actual := usageByUser[id]; actual != expected {
			t.Fatalf("issuer %d usage=%+v, want=%+v", id, actual, expected)
		}
	}
}

func TestPresignRegistryEntryBoundaries(t *testing.T) {
	fileIDs := make([]string, maxPresignFileIDs+1)
	for n := range fileIDs {
		fileIDs[n] = "ordinary-file"
	}
	future := time.Now().Add(10 * time.Minute).Unix()
	for _, scenario := range []struct {
		name  string
		grant models.Presign
		valid bool
	}{
		{"filename-at-limit", models.Presign{Id: "filename", Expiry: future, Filename: strings.Repeat("f", maxPresignFilenameBytes)}, true},
		{"filename-over-limit", models.Presign{Id: "filename", Expiry: future, Filename: strings.Repeat("f", maxPresignFilenameBytes+1)}, false},
		{"file-count-at-limit", models.Presign{Id: "files", Expiry: future, FileIds: fileIDs[:maxPresignFileIDs]}, true},
		{"file-count-over-limit", models.Presign{Id: "files", Expiry: future, FileIds: fileIDs}, false},
		{"entry-at-limit", presignWithBytes("entry", 1, maxPresignEntryBytes), true},
		{"entry-over-limit", presignWithBytes("entry", 1, maxPresignEntryBytes+1), false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			isolatePresignRegistry(t)
			err := Save(scenario.grant)
			if scenario.valid && err != nil || !scenario.valid && !errors.Is(err, ErrInvalid) {
				t.Fatalf("admission error=%v, valid=%t", err, scenario.valid)
			}
			if _, found := Get(scenario.grant.Id); found != scenario.valid {
				t.Fatalf("retained grant=%t, want=%t", found, scenario.valid)
			}
		})
	}
}

func TestPresignRegistryByteBudgetsRecoverAfterRemoval(t *testing.T) {
	for _, processBudget := range []bool{false, true} {
		name, limit := "issuer", maxPresignBytesPerUser
		if processBudget {
			name, limit = "process", maxPresignBytes
		}
		t.Run(name, func(t *testing.T) {
			isolatePresignRegistry(t)
			entriesPerUser := maxPresignBytesPerUser / maxPresignEntryBytes
			count := limit / maxPresignEntryBytes
			for n := 0; n < count; n++ {
				userID := 1
				if processBudget {
					userID = n / entriesPerUser
				}
				if err := Save(presignWithBytes(fmt.Sprintf("bytes-%03d", n), userID, maxPresignEntryBytes)); err != nil {
					t.Fatal("admission below byte limit failed", err)
				}
			}
			newUser := 1
			if processBudget {
				newUser = count / entriesPerUser
			}
			retry := presignWithBytes("byte-retry", newUser, maxPresignEntryBytes)
			if err := Save(retry); !errors.Is(err, ErrCapacity) {
				t.Fatal("byte limit accepted another grant", err)
			}
			Delete("bytes-000")
			if err := Save(retry); err != nil {
				t.Fatal("delete did not release byte budget", err)
			}
			if !Consume(retry.Id) {
				t.Fatal("admitted grant could not be consumed")
			}
			if err := Save(retry); err != nil {
				t.Fatal("consume did not release byte budget", err)
			}
			// Simulate passage of time without sleeping through the grant's TTL.
			mutex.Lock()
			expired := presignedUrls[retry.Id]
			expired.value.Expiry = time.Now().Unix() - 1
			presignedUrls[retry.Id] = expired
			mutex.Unlock()
			if _, found := Get(retry.Id); found {
				t.Fatal("expired grant remained available")
			}
			if err := Save(retry); err != nil {
				t.Fatal("expiry did not release byte budget", err)
			}
			mutex.Lock()
			expired = presignedUrls[retry.Id]
			expired.value.Expiry = time.Now().Unix() - 1
			presignedUrls[retry.Id] = expired
			mutex.Unlock()
			if err := Save(presignWithBytes("after-expiry", newUser, maxPresignEntryBytes)); err != nil {
				t.Fatal("admission did not reclaim expired byte usage", err)
			}
			mutex.RLock()
			if len(presignedUrls) != count || retainedBytes != limit {
				t.Errorf("byte budget count/bytes=%d/%d, want=%d/%d", len(presignedUrls), retainedBytes, count, limit)
			}
			mutex.RUnlock()
		})
	}
}

func TestPresignRegistryReplacementAccounting(t *testing.T) {
	isolatePresignRegistry(t)
	fullCount := maxPresignBytesPerUser / maxPresignEntryBytes
	for _, grant := range []models.Presign{
		presignWithBytes("replace", 1, 256),
		presignWithBytes("replace", 1, 512),
		presignWithBytes("replace", 2, 1024),
	} {
		if err := Save(grant); err != nil {
			t.Fatal(err)
		}
		assertPresignUsage(t, 1, presignEntryBaseBytes+len(grant.Id)+len(grant.ApiKeyId),
			map[int]userUsage{grant.UserId: {entries: 1, bytes: presignEntryBaseBytes + len(grant.Id) + len(grant.ApiKeyId)}})
	}
	if err := Save(presignWithBytes("replace", 3, maxPresignEntryBytes+1)); !errors.Is(err, ErrInvalid) {
		t.Fatal("oversized replacement was accepted", err)
	}
	for n := 0; n < fullCount; n++ {
		if err := Save(presignWithBytes(fmt.Sprintf("full-%d", n), 3, maxPresignEntryBytes)); err != nil {
			t.Fatal(err)
		}
	}
	if err := Save(presignWithBytes("full-0", 3, maxPresignEntryBytes)); err != nil {
		t.Fatal("same-issuer replacement at full quota failed", err)
	}
	if err := Save(presignWithBytes("replace", 3, 1024)); !errors.Is(err, ErrCapacity) {
		t.Fatal("replacement bypassed the destination issuer's quota", err)
	}
	if grant, found := Get("replace"); !found || grant.UserId != 2 {
		t.Fatal("rejected replacement modified the old grant")
	}
	assertPresignUsage(t, fullCount+1, maxPresignBytesPerUser+1024,
		map[int]userUsage{2: {entries: 1, bytes: 1024}, 3: {entries: fullCount, bytes: maxPresignBytesPerUser}})
	Delete("replace")
	assertPresignUsage(t, fullCount, maxPresignBytesPerUser,
		map[int]userUsage{3: {entries: fullCount, bytes: maxPresignBytesPerUser}})
}

func TestPresignRegistryCountAndConcurrentAdmission(t *testing.T) {
	t.Run("process-count", func(t *testing.T) {
		isolatePresignRegistry(t)
		for n := 0; n < maxPresignEntries; n++ {
			grant := models.Presign{Id: fmt.Sprintf("count-%04d", n), UserId: n / maxPresignsPerUser, Expiry: time.Now().Add(10 * time.Minute).Unix()}
			if err := Save(grant); err != nil {
				t.Fatal("admission below entry limit failed", err)
			}
		}
		extra := models.Presign{Id: "count-extra", UserId: maxPresignEntries / maxPresignsPerUser, Expiry: time.Now().Add(10 * time.Minute).Unix()}
		if err := Save(extra); !errors.Is(err, ErrCapacity) {
			t.Fatal("process entry limit was bypassed", err)
		}
		Delete("count-0000")
		if err := Save(extra); err != nil {
			t.Fatal("delete did not release entry quota", err)
		}
	})
	t.Run("concurrent-issuer-count", func(t *testing.T) {
		isolatePresignRegistry(t)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var admitted atomic.Int32
		failures := make(chan error, 2*maxPresignsPerUser)
		for n := 0; n < 2*maxPresignsPerUser; n++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				<-start
				err := Save(models.Presign{Id: fmt.Sprintf("race-%03d", n), UserId: 42, Expiry: time.Now().Add(10 * time.Minute).Unix()})
				if err == nil {
					admitted.Add(1)
				} else if !errors.Is(err, ErrCapacity) {
					failures <- err
				}
			}(n)
		}
		close(start)
		wg.Wait()
		close(failures)
		for err := range failures {
			t.Fatal("unexpected admission failure", err)
		}
		if admitted.Load() != maxPresignsPerUser {
			t.Fatalf("concurrent admissions=%d, want=%d", admitted.Load(), maxPresignsPerUser)
		}
		consumed := 0
		for n := 0; n < 2*maxPresignsPerUser; n++ {
			if Consume(fmt.Sprintf("race-%03d", n)) {
				consumed++
			}
		}
		if consumed != maxPresignsPerUser {
			t.Fatalf("retained grants=%d, want=%d", consumed, maxPresignsPerUser)
		}
		assertPresignUsage(t, 0, 0, nil)
	})
}
