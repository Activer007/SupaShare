//go:build test

package database

import (
	"database/sql"
	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
	"reflect"
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

func TestOIDCMigrationRejectsOneSidedRedisSource(t *testing.T) {
	source, target := deliveryConfig(t, 1), deliveryConfig(t, 0)
	owner := models.User{Id: 7, Name: "legacy@example.invalid", UserLevel: models.UserLevelSuperAdmin, Permissions: models.UserPermissionAll}
	file := models.File{Id: "source-file", UserId: owner.Id, SHA1: "synthetic", SizeBytes: 3}
	p := deliveryOpen(t, source)
	p.SaveUser(owner, false)
	p.SaveMetaData(file)
	p.Close()
	conn, err := redigo.Dial("tcp", source.HostUrl)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.Do("HSET", source.RedisPrefix+"oidc:users", owner.Id, `["https://idp.example.invalid","orphan-subject"]`); err != nil {
		t.Fatal(err)
	}
	before, err := redigo.StringMap(conn.Do("HGETALL", source.RedisPrefix+"oidc:users"))
	if err != nil {
		t.Fatal(err)
	}
	q := deliveryOpen(t, target)
	q.Close()
	if err = MigrateChecked(t.Context(), source, target); err == nil {
		t.Fatal("one-sided identity index was silently dropped during migration")
	}
	after, err := redigo.StringMap(conn.Do("HGETALL", source.RedisPrefix+"oidc:users"))
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("failed migration changed the source identity index", err)
	}
	p = deliveryOpen(t, source)
	defer p.Close()
	if got, ok := p.GetUser(owner.Id); !ok || !reflect.DeepEqual(got, owner) {
		t.Fatal("failed migration changed the source user", got)
	}
	if got, ok := p.GetMetaDataById(file.Id); !ok || !reflect.DeepEqual(got, file) {
		t.Fatal("failed migration changed source metadata", got)
	}
	q = deliveryOpen(t, target)
	defer q.Close()
	identities, err := q.ListOIDCIdentities()
	if err != nil || len(identities) != 0 || len(q.GetAllUsers()) != 0 || len(q.GetAllMetadata()) != 0 {
		t.Fatal("migration wrote to the destination before validating the source", err)
	}
}
