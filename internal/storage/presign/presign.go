package presign

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
)

const (
	maxPresignEntries       = 1024
	maxPresignsPerUser      = 64
	maxPresignBytes         = 8 * 1024 * 1024
	maxPresignBytesPerUser  = 512 * 1024
	maxPresignEntryBytes    = 64 * 1024
	maxPresignFilenameBytes = 4 * 1024
	maxPresignFileIDs       = 1024
	presignEntryBaseBytes   = 128
	presignFileIDEntryBytes = 16
	presignCleanupInterval  = 30 * time.Second
)

var (
	ErrInvalid  = errors.New("invalid presign")
	ErrCapacity = errors.New("presign capacity reached")
)

type storedPresign struct {
	value models.Presign
	bytes int
}

type userUsage struct {
	entries int
	bytes   int
}

var presignedUrls = make(map[string]storedPresign)
var retainedBytes int
var usageByUser = make(map[int]userUsage)
var mutex sync.RWMutex
var startCleanupOnce sync.Once

// Save saves a presigned URL if its retained state fits the fixed process and
// issuer budgets. The cloned strings prevent a small field from retaining a
// larger request-header backing allocation.
func Save(presign models.Presign) error {
	stored, err := prepare(presign)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	mutex.Lock()
	cleanupExpiredLocked(now)

	previous, replacing := presignedUrls[stored.value.Id]
	if stored.value.Expiry <= now {
		if replacing {
			deleteLocked(stored.value.Id, previous)
		}
		mutex.Unlock()
		return nil
	}

	entries := len(presignedUrls)
	totalBytes := retainedBytes
	issuerUsage := usageByUser[stored.value.UserId]
	if replacing {
		entries--
		totalBytes -= previous.bytes
		if previous.value.UserId == stored.value.UserId {
			issuerUsage.entries--
			issuerUsage.bytes -= previous.bytes
		}
	}
	if entries >= maxPresignEntries || totalBytes > maxPresignBytes-stored.bytes ||
		issuerUsage.entries >= maxPresignsPerUser || issuerUsage.bytes > maxPresignBytesPerUser-stored.bytes {
		mutex.Unlock()
		return ErrCapacity
	}
	if replacing {
		deleteLocked(stored.value.Id, previous)
	}
	presignedUrls[stored.value.Id] = stored
	retainedBytes += stored.bytes
	issuerUsage = usageByUser[stored.value.UserId]
	issuerUsage.entries++
	issuerUsage.bytes += stored.bytes
	usageByUser[stored.value.UserId] = issuerUsage
	mutex.Unlock()
	startCleanupOnce.Do(func() { go cleanUp(true) })
	return nil
}

// Get returns the presigned url with the given ID or false if not a valid ID
func Get(id string) (models.Presign, bool) {
	mutex.Lock()
	defer mutex.Unlock()
	stored, ok := presignedUrls[id]
	if !ok {
		return models.Presign{}, false
	}
	if stored.value.Expiry <= time.Now().Unix() {
		deleteLocked(id, stored)
		return models.Presign{}, false
	}
	return clone(stored.value), true
}

// Consume atomically claims a still-valid capability once. Callers must first
// check current issuer, consumer and object authority; a rejected consumer
// must not invalidate another authorized user's pending download.
func Consume(id string) bool {
	mutex.Lock()
	defer mutex.Unlock()
	stored, ok := presignedUrls[id]
	if !ok {
		return false
	}
	deleteLocked(id, stored)
	return stored.value.Expiry > time.Now().Unix()
}

// Delete deletes the presigned url with the given ID
func Delete(id string) {
	mutex.Lock()
	if stored, ok := presignedUrls[id]; ok {
		deleteLocked(id, stored)
	}
	mutex.Unlock()
}

func cleanUp(periodic bool) {
	for {
		mutex.Lock()
		cleanupExpiredLocked(time.Now().Unix())
		mutex.Unlock()
		if !periodic {
			return
		}
		time.Sleep(presignCleanupInterval)
	}
}

func prepare(presign models.Presign) (storedPresign, error) {
	if presign.Id == "" || len(presign.Filename) > maxPresignFilenameBytes ||
		len(presign.FileIds) > maxPresignFileIDs {
		return storedPresign{}, ErrInvalid
	}
	bytes := presignEntryBaseBytes
	fields := []string{presign.Id, presign.Filename, presign.ApiKeyId}
	for _, field := range fields {
		if len(field) > maxPresignEntryBytes-bytes {
			return storedPresign{}, ErrInvalid
		}
		bytes += len(field)
	}
	for _, id := range presign.FileIds {
		if len(id) > maxPresignEntryBytes-bytes-presignFileIDEntryBytes {
			return storedPresign{}, ErrInvalid
		}
		bytes += len(id) + presignFileIDEntryBytes
	}
	result := clone(presign)
	return storedPresign{value: result, bytes: bytes}, nil
}

func clone(presign models.Presign) models.Presign {
	presign.Id = strings.Clone(presign.Id)
	presign.Filename = strings.Clone(presign.Filename)
	presign.ApiKeyId = strings.Clone(presign.ApiKeyId)
	presign.FileIds = append([]string(nil), presign.FileIds...)
	for i := range presign.FileIds {
		presign.FileIds[i] = strings.Clone(presign.FileIds[i])
	}
	return presign
}

func cleanupExpiredLocked(now int64) {
	for id, stored := range presignedUrls {
		if stored.value.Expiry <= now {
			deleteLocked(id, stored)
		}
	}
}

func deleteLocked(id string, stored storedPresign) {
	delete(presignedUrls, id)
	retainedBytes -= stored.bytes
	usage := usageByUser[stored.value.UserId]
	usage.entries--
	usage.bytes -= stored.bytes
	if usage.entries == 0 {
		delete(usageByUser, stored.value.UserId)
	} else {
		usageByUser[stored.value.UserId] = usage
	}
}
