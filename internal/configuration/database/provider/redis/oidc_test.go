//go:build test

package redis

import (
	"fmt"
	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
	"reflect"
	"strconv"
	"sync"
	"testing"
)

func TestOIDCBindingsIsolation(t *testing.T) {
	p := sec030RedisNew(t).db
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
	p := sec030RedisNew(t).db
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
	p := sec030RedisNew(t).db
	p.SaveUser(models.User{Id: 1, Name: "legacy@example.test", UserLevel: models.UserLevelSuperAdmin}, false)
	p.SaveSession("legacy-session", models.Session{UserId: 1, ValidUntil: 4102444800})
	p.Upgrade(11)
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

func TestOIDCProvisionPreservesExactUserIDs(t *testing.T) {
	maxUserID := int(^uint(0) >> 1)
	counters := []int64{int64(maxUserID) - 1}
	if strconv.IntSize == 64 {
		counters = append(counters, 1<<53)
	}
	for _, counter := range counters {
		t.Run(strconv.FormatInt(counter, 10), func(t *testing.T) {
			p := sec030RedisNew(t).db
			conn := p.pool.Get()
			defer conn.Close()
			if _, err := conn.Do("SET", p.dbPrefix+prefixUserIdCounter, counter); err != nil {
				t.Fatal(err)
			}
			user, err := p.CreateOIDCUser("https://idp.example.test", "large-id", "large@example.test", 0)
			if err != nil {
				t.Fatal(err)
			}
			want := int(counter + 1)
			if user.Id != want {
				t.Fatalf("created user ID = %d, want exact next ID %d", user.Id, want)
			}
			stored, ok, err := p.GetOIDCUser("https://idp.example.test", "large-id")
			if err != nil || !ok || stored.Id != want {
				t.Fatalf("stored identity returned user %+v, found=%t, error=%v", stored, ok, err)
			}
			identities, err := p.ListOIDCIdentities()
			if err != nil || len(identities) != 1 || identities[0].UserID != want {
				t.Fatalf("export did not preserve the exact user ID: %+v, error=%v", identities, err)
			}
		})
	}
}

func TestOIDCProvisionRejectsInvalidUserIDBeforeWritingAccount(t *testing.T) {
	for _, counter := range []string{"-1", strconv.Itoa(int(^uint(0) >> 1))} {
		t.Run(counter, func(t *testing.T) {
			p := sec030RedisNew(t).db
			conn := p.pool.Get()
			defer conn.Close()
			if _, err := conn.Do("SET", p.dbPrefix+prefixUserIdCounter, counter); err != nil {
				t.Fatal(err)
			}
			if _, err := p.CreateOIDCUser("https://idp.example.test", "invalid-id", "invalid@example.test", 0); err == nil {
				t.Fatal("accepted a nonpositive or overflowing local user ID")
			}
			if len(p.GetAllUsers()) != 0 {
				t.Fatal("rejected user ID left an account")
			}
			identities, err := p.ListOIDCIdentities()
			if err != nil || len(identities) != 0 {
				t.Fatalf("rejected user ID left a binding: %+v, error=%v", identities, err)
			}
		})
	}
}

func TestOIDCProvisionRejects32BitUserIDOverflow(t *testing.T) {
	p := sec030RedisNew(t).db
	conn := p.pool.Get()
	defer conn.Close()
	const max32BitUserID = "2147483647"
	if _, err := conn.Do("SET", p.dbPrefix+prefixUserIdCounter, max32BitUserID); err != nil {
		t.Fatal(err)
	}
	_, err := createOIDCUserScript.Do(conn,
		p.dbPrefix+oidcBindings, p.dbPrefix+oidcUsers, p.dbPrefix+prefixUserIdCounter,
		oidcKey("https://idp.example.test", "overflow"), p.dbPrefix+prefixUsers,
		"overflow@example.test", 0, max32BitUserID)
	if err == nil {
		t.Fatal("script accepted an ID beyond the Go caller's range")
	}
	if len(p.GetAllUsers()) != 0 {
		t.Fatal("rejected user ID left an account")
	}
	identities, err := p.ListOIDCIdentities()
	if err != nil || len(identities) != 0 {
		t.Fatalf("rejected user ID left a binding: %+v, error=%v", identities, err)
	}
}

func TestOIDCIdentityExportRejectsInconsistentIndexesWithoutMutation(t *testing.T) {
	const issuer, subject = "https://idp.example.test", "subject"
	for _, scenario := range []string{"inverse-only", "forward-only", "mismatched-owner", "noncanonical-key"} {
		t.Run(scenario, func(t *testing.T) {
			p := sec030RedisNew(t).db
			p.SaveUser(models.User{Id: 7, Name: "existing@example.test", UserLevel: models.UserLevelUser}, false)
			if err := p.BindOIDCIdentity(issuer, subject, 7); err != nil {
				t.Fatal(err)
			}
			conn := p.pool.Get()
			defer conn.Close()
			forward, inverse := p.dbPrefix+oidcBindings, p.dbPrefix+oidcUsers
			var err error
			switch scenario {
			case "inverse-only":
				_, err = conn.Do("HDEL", forward, oidcKey(issuer, subject))
			case "forward-only":
				_, err = conn.Do("HDEL", inverse, 7)
			case "mismatched-owner":
				_, err = conn.Do("HSET", inverse, 7, oidcKey(issuer, "different-subject"))
			case "noncanonical-key":
				key := `["https://idp.example.test", "subject"]`
				if _, err = conn.Do("HDEL", forward, oidcKey(issuer, subject)); err == nil {
					_, err = conn.Do("HSET", forward, key, 7)
				}
				if err == nil {
					_, err = conn.Do("HSET", inverse, 7, key)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			readIndexes := func() [2]map[string]string {
				t.Helper()
				var result [2]map[string]string
				for n, key := range []string{forward, inverse} {
					result[n], err = redigo.StringMap(conn.Do("HGETALL", key))
					if err != nil {
						t.Fatal(err)
					}
				}
				return result
			}
			before := readIndexes()
			if _, err := p.ListOIDCIdentities(); err == nil {
				t.Fatal("export accepted inconsistent identity indexes")
			}
			if after := readIndexes(); !reflect.DeepEqual(before, after) {
				t.Fatal("failed export changed source identity indexes")
			}
		})
	}
}
