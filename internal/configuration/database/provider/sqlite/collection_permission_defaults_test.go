//go:build test

package sqlite

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
			p := newQuotaDatabase(t)
			ordinary := models.User{Id: 71, Name: "legacy ordinary", UserLevel: models.UserLevelUser, Permissions: models.UserPermReplaceUploads}
			admin := models.User{Id: 72, Name: "legacy administrator", UserLevel: models.UserLevelAdmin}
			p.SaveUser(ordinary, false)
			p.SaveUser(admin, false)
			// Recreate the schema-11 boundary used by the existing file-request
			// migration, while retaining the unrelated provider fixtures.
			if _, err := p.sqliteDb.Exec(`ALTER TABLE FileMetaData DROP COLUMN UploadRequestId;
				ALTER TABLE ApiKeys DROP COLUMN UploadRequestId;
				DROP TABLE UploadRequests; DROP TABLE Statistics;`); err != nil {
				t.Fatal(err)
			}
			p.SetDbVersion(11)
			p.Upgrade(11)
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
