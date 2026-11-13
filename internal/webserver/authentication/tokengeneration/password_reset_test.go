package tokengeneration

import (
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/models"
	"testing"
)

func TestGenerateDeniesPasswordReset(t *testing.T) {
	old := configuration.Get().Authentication.Method
	configuration.Get().Authentication.Method = models.AuthenticationInternal
	t.Cleanup(func() { configuration.Get().Authentication.Method = old })
	for _, rank := range []models.UserRank{models.UserLevelUser, models.UserLevelAdmin, models.UserLevelSuperAdmin} {
		token, _, err := Generate(models.User{Id: 9825, UserLevel: rank, Permissions: models.UserPermissionAll, ResetPassword: true}, models.ApiPermView)
		if err == nil || token != "" {
			t.Fatal("reset required principal minted token")
		}
	}
}
