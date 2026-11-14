package authorization

import (
	"github.com/Activer007/SupaShare/internal/models"
	"testing"
)

func TestManageUserTargetMatrix(t *testing.T) {
	for _, tc := range []struct {
		name          string
		actor, target models.User
		want          bool
	}{
		{"ordinary manager to basic user", models.User{Id: 1, UserLevel: 2, Permissions: models.UserPermManageUsers}, models.User{Id: 2, UserLevel: 2}, true},
		{"ordinary manager to admin", models.User{Id: 1, UserLevel: 2, Permissions: models.UserPermissionAll}, models.User{Id: 2, UserLevel: 1}, false},
		{"admin to admin", models.User{Id: 1, UserLevel: 1, Permissions: models.UserPermissionAll}, models.User{Id: 2, UserLevel: 1, Permissions: models.UserPermissionAll}, true},
		{"superadmin protected", models.User{Id: 1, UserLevel: 0, Permissions: models.UserPermissionAll}, models.User{Id: 2, UserLevel: 0}, false},
		{"self protected", models.User{Id: 1, UserLevel: 1, Permissions: models.UserPermissionAll}, models.User{Id: 1, UserLevel: 1}, false},
		{"missing manage permission", models.User{Id: 1, UserLevel: 1}, models.User{Id: 2, UserLevel: 2}, false},
		{"same rank higher permission", models.User{Id: 1, UserLevel: 2, Permissions: models.UserPermManageUsers}, models.User{Id: 2, UserLevel: 2, Permissions: models.UserPermManageLogs}, false},
		{"invalid actor", models.User{Id: 1, UserLevel: 3, Permissions: models.UserPermissionAll}, models.User{Id: 2, UserLevel: 2}, false},
		{"invalid target", models.User{Id: 1, UserLevel: 1, Permissions: models.UserPermissionAll}, models.User{Id: 2, UserLevel: 3}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanManageUserTarget(tc.actor, tc.target); got != tc.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}
