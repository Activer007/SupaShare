package storage

import (
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"strings"
	"testing"
)

func TestN15LegacyCreationRetriesCollision(t *testing.T) {
	old := models.File{Id: "n15-old", Name: "protected", UserId: 1, HotlinkId: "n15-old.png", UnlimitedTime: true}
	if err := database.CreateFileMetadata(old); err != nil {
		t.Fatal(err)
	}
	defer database.DeleteMetaData(old.Id)
	defer database.DeleteHotlink(old.HotlinkId)
	next := old
	next.Name = "new"
	next.UserId = 2
	if err := publishNewMetadata(&next); err != nil {
		t.Fatal(err)
	}
	defer database.DeleteMetaData(next.Id)
	defer database.DeleteHotlink(next.HotlinkId)
	if next.Id == old.Id || len(next.Id) < 22 {
		t.Fatal("collision not retried with strong ID")
	}
	current, ok := database.GetMetaDataById(old.Id)
	if !ok || current.Name != old.Name || current.UserId != 1 {
		t.Fatal("old link changed")
	}
}
func TestN15GeneratedCapabilities(t *testing.T) {
	for i := 0; i < 32; i++ {
		id := createNewId()
		if len(id) < 22 || strings.ContainsAny(id, "/+=_- ") {
			t.Fatalf("invalid capability %q", id)
		}
		f := models.File{Name: "image.png", ContentType: "image/png"}
		setHotlinkCandidate(&f)
		if len(strings.TrimSuffix(f.HotlinkId, ".png")) < 22 {
			t.Fatal("weak hotlink")
		}
	}
}
