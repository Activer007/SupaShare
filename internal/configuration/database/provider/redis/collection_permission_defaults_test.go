//go:build test

package redis

import (
	"os"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestLegacyCollectionPermissionGrant(t *testing.T) {
	for _, scenario := range []struct {
		name, value  string
		unset, grant bool
	}{
		{name: "unset", unset: true}, {name: "empty"},
		{name: "disabled", value: "false"}, {name: "enabled", value: "true", grant: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("GOKAPI_GUEST_UPLOAD_BY_DEFAULT", scenario.value)
			if scenario.unset {
				if err := os.Unsetenv("GOKAPI_GUEST_UPLOAD_BY_DEFAULT"); err != nil {
					t.Fatal(err)
				}
			}
			p := sec030RedisNew(t).db
			ordinary := models.User{Id: 71, Name: "legacy ordinary", UserLevel: models.UserLevelUser, Permissions: models.UserPermReplaceUploads}
			admin := models.User{Id: 72, Name: "legacy administrator", UserLevel: models.UserLevelAdmin}
			p.SaveUser(ordinary, false)
			p.SaveUser(admin, false)
			p.SetDbVersion(5)
			p.Upgrade(5)
			want := ordinary.Permissions
			if scenario.grant {
				want |= models.UserPermGuestUploads
			}
			stored, ok := p.GetUser(ordinary.Id)
			if !ok || stored.Permissions != want {
				t.Fatalf("legacy ordinary permissions = %d, want %d", stored.Permissions, want)
			}
			stored, ok = p.GetUser(admin.Id)
			if !ok || stored.Permissions != models.UserPermGuestUploads {
				t.Fatal("legacy administrator must retain collection initialization")
			}
		})
	}
}
