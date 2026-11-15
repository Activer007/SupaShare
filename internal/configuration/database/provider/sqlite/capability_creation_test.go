//go:build test

package sqlite

import (
	"errors"
	"fmt"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

func TestN15CapabilityCollision(t *testing.T) {
	db := newQuotaDatabase(t)
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
	db := newQuotaDatabase(t)
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
