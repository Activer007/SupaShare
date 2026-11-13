//go:build test

package database

import (
	"database/sql"
	"github.com/Activer007/SupaShare/internal/models"
	"testing"
)

func TestOIDCMigrationPreservesBindings(t *testing.T) {
	for _, kind := range []int{0, 1} {
		source, target := deliveryConfig(t, kind), deliveryConfig(t, 1-kind)
		p := deliveryOpen(t, source)
		p.SaveUser(models.User{Id: 7, Name: "legacy@example.test", UserLevel: models.UserLevelSuperAdmin}, false)
		if err := p.BindOIDCIdentity("https://idp.example.test", "CaseSensitiveSubject", 7); err != nil {
			t.Fatal(err)
		}
		p.Close()
		if err := MigrateChecked(t.Context(), source, target); err != nil {
			t.Fatal(err)
		}
		q := deliveryOpen(t, target)
		got, ok, err := q.GetOIDCUser("https://idp.example.test", "CaseSensitiveSubject")
		if err != nil || !ok || got.Id != 7 || !got.IsSuperAdmin() {
			t.Fatal("migration lost binding", got, err)
		}
		if _, ok, err := q.GetOIDCUser("https://idp.example.test", "casesensitivesubject"); err != nil || ok {
			t.Fatal("migration collapsed identity case", err)
		}
		q.Close()
	}
}

func TestOIDCLegacyMigrationAndCorruptCurrentSchema(t *testing.T) {
	for _, version := range []int{19, 20} {
		source, target := deliveryConfig(t, 0), deliveryConfig(t, 1)
		p := deliveryOpen(t, source)
		p.SaveUser(models.User{Id: 1, Name: "legacy@example.test", UserLevel: models.UserLevelSuperAdmin}, false)
		p.SetDbVersion(version)
		p.Close()
		conn, err := sql.Open("sqlite", source.HostUrl)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = conn.Exec("DROP TABLE OIDCIdentities"); err != nil {
			t.Fatal(err)
		}
		conn.Close()
		err = MigrateChecked(t.Context(), source, target)
		if version == 19 {
			if err != nil {
				t.Fatal("old schema migration failed", err)
			}
			q := deliveryOpen(t, target)
			identities, listErr := q.ListOIDCIdentities()
			if listErr != nil || len(identities) != 0 {
				t.Fatal("legacy account auto-linked", identities, listErr)
			}
			if _, ok := q.GetUser(1); !ok {
				t.Fatal("legacy account lost")
			}
			q.Close()
		} else {
			if err == nil {
				t.Fatal("current schema missing security table accepted")
			}
			q := deliveryOpen(t, target)
			if len(q.GetAllUsers()) != 0 {
				t.Fatal("target changed before source validation")
			}
			q.Close()
		}
	}
}
