//go:build test

package redis

import (
	"errors"
	"fmt"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	redigo "github.com/gomodule/redigo/redis"
)

func TestN15CreationMaintainsInstalledFileRequestIndex(t *testing.T) {
	scene := sec030RedisNew(t)
	indexVersion := scene.config.RedisPrefix + "frqidx:version"
	sec030RedisDo(t, scene.conn, "SET", indexVersion, 1)
	file := models.File{Id: "indexed-upload", UserId: 1, UploadRequestId: "collection"}
	if err := scene.db.CreateFileMetadata(file); err != nil {
		t.Fatal(err)
	}
	member, err := redigo.Bool(scene.conn.Do("SISMEMBER", scene.config.RedisPrefix+"frqidx:files:collection", file.Id))
	if err != nil || !member {
		t.Fatalf("created file missing from installed request index: %v", err)
	}

	blocked := models.File{Id: "blocked-upload", UserId: 1, UploadRequestId: "broken-index", HotlinkId: "blocked.png"}
	sec030RedisDo(t, scene.conn, "SET", scene.config.RedisPrefix+"frqidx:files:broken-index", "wrong type")
	revision, err := redigo.String(scene.conn.Do("GET", scene.config.RedisPrefix+keyMetadataRevision))
	if err != nil {
		t.Fatal(err)
	}
	if err := scene.db.CreateFileMetadata(blocked); err == nil {
		t.Fatal("invalid installed index accepted")
	}
	if _, exists := scene.db.GetMetaDataById(blocked.Id); exists {
		t.Fatal("partial file published")
	}
	if _, exists := scene.db.GetHotlink(blocked.HotlinkId); exists {
		t.Fatal("partial hotlink published")
	}
	after, err := redigo.String(scene.conn.Do("GET", scene.config.RedisPrefix+keyMetadataRevision))
	if err != nil || after != revision {
		t.Fatal("failed publication changed the metadata revision", err)
	}
}

func TestN15CapabilityCollision(t *testing.T) {
	db := sec030RedisNew(t).db
	original := models.File{Id: "short", Name: "protected", UserId: 1, HotlinkId: "old.png", UnlimitedTime: true}
	if err := db.CreateFileMetadata(original); err != nil {
		t.Fatal(err)
	}
	before, _ := db.GetMetaDataById(original.Id)
	collision := original
	collision.Name = "replacement"
	collision.UserId = 2
	if err := db.CreateFileMetadata(collision); !errors.Is(err, uploadquota.ErrConflict) {
		t.Fatalf("collision: %v", err)
	}
	after, _ := db.GetMetaDataById(original.Id)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("replaced legacy metadata")
	}
	collision.Id = "different"
	if err := db.CreateFileMetadata(collision); !errors.Is(err, uploadquota.ErrConflict) {
		t.Fatalf("hotlink collision: %v", err)
	}
	if _, ok := db.GetMetaDataById(collision.Id); ok {
		t.Fatal("partial metadata")
	}
	if err := db.ClaimHotlink(collision); !errors.Is(err, uploadquota.ErrConflict) {
		t.Fatalf("claim collision: %v", err)
	}
	if target, ok := db.GetHotlink(original.HotlinkId); !ok || target != original.Id {
		t.Fatal("retargeted old link")
	}
	if err := db.ClaimHotlink(original); err != nil {
		t.Fatal("idempotent claim", err)
	}
}
func TestN15ConcurrentCapabilityCreation(t *testing.T) {
	db := sec030RedisNew(t).db
	for _, hotlink := range []bool{false, true} {
		t.Run(fmt.Sprint(hotlink), func(t *testing.T) {
			var successes atomic.Int32
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					f := models.File{Id: "one-new-id", UserId: 1, Name: fmt.Sprint(i)}
					if hotlink {
						f.Id = fmt.Sprintf("hot-%d", i)
						f.HotlinkId = "one-new-hot.png"
					}
					err := db.CreateFileMetadata(f)
					if err == nil {
						successes.Add(1)
					} else if !errors.Is(err, uploadquota.ErrConflict) {
						t.Errorf("unexpected error %v", err)
					}
				}(i)
			}
			wg.Wait()
			if successes.Load() != 1 {
				t.Fatalf("winners %d", successes.Load())
			}
		})
	}
}
