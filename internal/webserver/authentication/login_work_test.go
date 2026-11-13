package authentication

import (
	"github.com/Activer007/SupaShare/internal/configuration"
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"strings"
	"testing"
)

func TestLoginMissingAccountUsesValidArgonHash(t *testing.T) {
	// This is a structural/work-path regression, not a flaky wall-clock threshold.
	if !strings.HasPrefix(dummyLoginHash, "argon2id$") {
		t.Fatal("dummy must exercise Argon2")
	}
	matched, legacy := configuration.VerifyPassword("synthetic-wrong-password", dummyLoginHash, "")
	if matched || legacy {
		t.Fatal("dummy must be modern and reject this password")
	}
	user := models.User{Id: 9819, Name: "login-work-user", Password: configuration.HashPassword("synthetic-right-password", false, "")}
	database.SaveUser(user, false)
	defer database.DeleteUser(user.Id)
	for _, name := range []string{user.Name, "missing-login-work-user"} {
		got, ok, csrf := checkUsernameAndPassword(name, "synthetic-wrong-password", true)
		if ok || !csrf || got.Id != 0 {
			t.Fatal("failure response differs")
		}
	}
	got, ok, csrf := checkUsernameAndPassword(user.Name, "synthetic-right-password", true)
	if !ok || !csrf || got.Id != user.Id {
		t.Fatal("valid login regressed")
	}
	_, ok, csrf = checkUsernameAndPassword(user.Name, "synthetic-right-password", false)
	if ok || csrf {
		t.Fatal("invalid CSRF allowed")
	}
}

func TestLoginLegacyAndMalformedHashWorkPath(t *testing.T) {
	legacy := configuration.HashPassword("synthetic-legacy-password", true, "salt")
	for _, hash := range []string{legacy, "", "broken", "argon2id$bad-salt$bad-hash"} {
		ok, _ := verifyLoginPassword("wrong-password", hash, "salt")
		if ok {
			t.Fatal("invalid password accepted")
		}
	}
	ok, old := verifyLoginPassword("synthetic-legacy-password", legacy, "salt")
	if !ok || !old {
		t.Fatal("legacy migration input rejected")
	}
}

func TestPasswordChangeConfinementFollowsAuthMode(t *testing.T) {
	old := configuration.Get().Authentication.Method
	t.Cleanup(func() { configuration.Get().Authentication.Method = old })
	for _, method := range []int{models.AuthenticationInternal, models.AuthenticationOAuth2, models.AuthenticationHeader, models.AuthenticationDisabled} {
		configuration.Get().Authentication.Method = method
		if RequiresPasswordChange(models.User{ResetPassword: true}) != (method == models.AuthenticationInternal) {
			t.Fatal("historical local reset flag changed external auth policy")
		}
		if RequiresPasswordChange(models.User{}) {
			t.Fatal("ordinary user confined")
		}
	}
}
