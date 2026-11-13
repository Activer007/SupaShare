package authentication

import (
	"github.com/Activer007/SupaShare/internal/configuration/database"
	"github.com/Activer007/SupaShare/internal/models"
	"testing"
)

func TestOIDCIdentityCannotClaimEmail(t *testing.T) {
	Init(modelOauth)
	user := models.User{Id: 90151, Name: "oidc-victim@example.invalid", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	database.SaveUser(user, false)
	t.Cleanup(func() { database.DeleteAllSessionsByUser(user.Id); database.DeleteUser(user.Id) })
	info := OAuthUserInfo{Issuer: modelOauth.OAuthProvider, Subject: "attacker", Email: user.Name, EmailVerified: true}
	_, ok, err := getOrCreateOIDCUser(info)
	if err != nil || ok {
		t.Fatalf("unbound existing email must not authenticate: ok=%v err=%v", ok, err)
	}
	if err := database.BindOIDCIdentity(info.Issuer, "victim", user.Id); err != nil {
		t.Fatal(err)
	}
	_, ok, err = getOrCreateOIDCUser(info)
	if err != nil || ok {
		t.Fatal("different subject claimed victim account")
	}
	info.Subject = "victim"
	info.Email = "changed-email@example.invalid"
	info.EmailVerified = false // contact data no longer establishes identity
	authSettings.OnlyRegisteredUsers = true
	got, ok, err := getOrCreateOIDCUser(info)
	if err != nil || !ok || got.Id != user.Id || got.Permissions != user.Permissions {
		t.Fatalf("stable binding lost: %+v %v %v", got, ok, err)
	}
	info.Issuer = "https://different-provider.invalid"
	_, ok, err = getOrCreateOIDCUser(info)
	if err != nil || ok {
		t.Fatal("issuer collision authenticated")
	}
	info.Issuer = modelOauth.OAuthProvider
	info.Subject = "Victim"
	_, ok, err = getOrCreateOIDCUser(info)
	if err != nil || ok {
		t.Fatal("case-folded subject authenticated")
	}
}

func TestOIDCVerifiedNewUser(t *testing.T) {
	Init(modelOauth)
	info := OAuthUserInfo{Issuer: modelOauth.OAuthProvider, Subject: "new-oidc-subject", Email: "new-oidc-user@example.invalid"}
	_, ok, err := getOrCreateOIDCUser(info)
	if err != nil || ok {
		t.Fatal("unverified email provisioned")
	}
	info.EmailVerified = true
	user, ok, err := getOrCreateOIDCUser(info)
	if err != nil || !ok {
		t.Fatalf("new identity failed: %v", err)
	}
	t.Cleanup(func() { database.DeleteUser(user.Id) })
	if user.IsAdmin() {
		t.Fatal("new identity received admin privileges")
	}
	again, ok, err := getOrCreateOIDCUser(info)
	if err != nil || !ok || again.Id != user.Id {
		t.Fatal("repeated login not stable")
	}
}
