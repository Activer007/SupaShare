//go:build test

package sqlite

import (
	"fmt"
	"github.com/Activer007/SupaShare/internal/models"
	"sync"
	"testing"
)

func TestOIDCBindingsIsolation(t *testing.T) {
	p := newQuotaDatabase(t)
	p.SaveUser(models.User{Id: 1, Name: "admin@example.test", UserLevel: models.UserLevelSuperAdmin}, false)
	p.SaveUser(models.User{Id: 2, Name: "user@example.test", UserLevel: models.UserLevelUser}, false)
	const issuer = "https://idp.example.test"
	if err := p.BindOIDCIdentity(issuer, "Subject", 1); err != nil {
		t.Fatal(err)
	}
	if err := p.BindOIDCIdentity(issuer, "Subject", 1); err != nil {
		t.Fatal("idempotence", err)
	}
	for _, i := range []models.OIDCIdentity{
		{Issuer: issuer, Subject: "Subject", UserID: 2},
		{Issuer: issuer, Subject: "other", UserID: 1},
		{Issuer: issuer, Subject: "missing", UserID: 99},
		{Issuer: "", Subject: "invalid", UserID: 2},
	} {
		if err := p.BindOIDCIdentity(i.Issuer, i.Subject, i.UserID); err == nil {
			t.Fatal("accepted invalid/reassigned binding", i)
		}
	}
	for _, pair := range [][2]string{{issuer, "subject"}, {"https://other.test", "Subject"}} {
		_, ok, err := p.GetOIDCUser(pair[0], pair[1])
		if err != nil || ok {
			t.Fatal("issuer/subject not isolated", err)
		}
	}
	user, _ := p.GetUser(1)
	user.Name = "changed@example.test"
	p.SaveUser(user, false)
	got, ok, err := p.GetOIDCUser(issuer, "Subject")
	if err != nil || !ok || got.Id != 1 || got.Name != user.Name {
		t.Fatal("save/rename lost binding", got, err)
	}
	identities, err := p.ListOIDCIdentities()
	if err != nil || len(identities) != 1 {
		t.Fatal("export", identities, err)
	}
	p.DeleteUser(1)
	p.SaveUser(models.User{Id: 1, Name: "reused@example.test", UserLevel: models.UserLevelSuperAdmin}, false)
	if _, ok, err := p.GetOIDCUser(issuer, "Subject"); err != nil || ok {
		t.Fatal("deleted binding inherited", err)
	}
	identities, err = p.ListOIDCIdentities()
	if err != nil || len(identities) != 0 {
		t.Fatal("binding not deleted", identities, err)
	}
}

func TestOIDCAutoProvisionIsAtomic(t *testing.T) {
	p := newQuotaDatabase(t)
	const issuer = "https://idp.example.test"
	p.SaveUser(models.User{Id: 1, Name: "existing@example.test", UserLevel: models.UserLevelSuperAdmin}, false)
	if _, err := p.CreateOIDCUser(issuer, "attacker", "existing@example.test", 0); err == nil {
		t.Fatal("adopted existing privileged account")
	}
	if _, ok, err := p.GetOIDCUser(issuer, "attacker"); err != nil || ok {
		t.Fatal("failed create left binding", err)
	}
	user, err := p.CreateOIDCUser(issuer, "new", "NEW@example.test", models.UserPermGuestUploads)
	if err != nil || user.IsAdmin() || user.Name != "new@example.test" {
		t.Fatal("new user", user, err)
	}
	count := len(p.GetAllUsers())
	if _, err := p.CreateOIDCUser(issuer, "new", "another@example.test", 0); err == nil {
		t.Fatal("duplicate identity accepted")
	}
	if len(p.GetAllUsers()) != count {
		t.Fatal("failed creation left orphan user")
	}
	var wg sync.WaitGroup
	results := make(chan error, 8)
	for n := 0; n < 8; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, err := p.CreateOIDCUser(issuer, fmt.Sprint("racer", n), "race@example.test", 0)
			results <- err
		}(n)
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("race created duplicate account", successes)
	}
	if len(p.GetAllUsers()) != count+1 {
		t.Fatal("race leaked account")
	}
}

func TestOIDCSchemaUpgradeClearsSessionsWithoutLinkingUsers(t *testing.T) {
	p := newQuotaDatabase(t)
	p.SaveUser(models.User{Id: 1, Name: "legacy@example.test", UserLevel: models.UserLevelSuperAdmin}, false)
	p.SaveSession("legacy-session", models.Session{UserId: 1, ValidUntil: 4102444800})
	if _, err := p.sqliteDb.Exec("DROP TABLE OIDCIdentities"); err != nil {
		t.Fatal(err)
	}
	p.Upgrade(19)
	if _, ok := p.GetSession("legacy-session"); ok {
		t.Fatal("legacy session survived security upgrade")
	}
	entries, err := p.ListOIDCIdentities()
	if err != nil || len(entries) != 0 {
		t.Fatal("upgrade auto-linked legacy account", err)
	}
	if _, ok := p.GetUser(1); !ok {
		t.Fatal("upgrade removed legacy user")
	}
}
