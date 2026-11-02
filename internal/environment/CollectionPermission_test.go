package environment

import (
	"os"
	"testing"
)

func TestCollectionPermissionDefaults(t *testing.T) {
	for _, scenario := range []struct {
		name, value string
		unset       bool
		newUsers    bool
		legacy      bool
	}{
		{name: "unset", unset: true, newUsers: true},
		{name: "empty"},
		{name: "disabled", value: "false"},
		{name: "enabled", value: "true", newUsers: true, legacy: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Setenv("GOKAPI_GUEST_UPLOAD_BY_DEFAULT", scenario.value)
			if scenario.unset {
				if err := os.Unsetenv("GOKAPI_GUEST_UPLOAD_BY_DEFAULT"); err != nil {
					t.Fatal(err)
				}
			}
			parsed := New()
			if parsed.PermRequestGrantedByDefault != scenario.newUsers {
				t.Fatalf("new-user collection default = %v, want %v", parsed.PermRequestGrantedByDefault, scenario.newUsers)
			}
			if parsed.GrantFileRequestsOnLegacyUpgrade() != scenario.legacy {
				t.Fatalf("legacy collection grant = %v, want %v", parsed.GrantFileRequestsOnLegacyUpgrade(), scenario.legacy)
			}
		})
	}
}
