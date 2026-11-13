//go:build test

package redis

import (
	"fmt"
	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestUserApiKeyQuotaScanBudgets(t *testing.T) {
	scene := sec030RedisNew(t)
	p := scene.db
	for i, id := range []string{"system-one", "system-two"} {
		p.SaveApiKey(models.ApiKey{Id: id, UserId: i + 1, IsSystemKey: true})
	}
	k := models.ApiKey{Id: "new-key", PublicId: "new-public", UserId: 1, FriendlyName: "new"}
	for _, tc := range []struct {
		name        string
		scans, keys int
	}{{"scan-budget", 0, 20000}, {"matched-budget", 128, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			args := redigo.Args{}.Add(p.dbPrefix+prefixApiKeys+k.Id, p.dbPrefix+prefixApiKeys+"*", k.UserId, models.ApiKeyMaxGlobal, models.ApiKeyMaxPerUser, tc.scans, tc.keys).AddFlat(k)
			n, err := redigo.Int(createUserApiKey.Do(scene.conn, args...))
			if err != nil || n != -2 {
				t.Fatal("budget not enforced", n, err)
			}
			if _, ok := p.GetApiKey(k.Id); ok {
				t.Fatal("quota rejection persisted key")
			}
		})
	}
	// Management and creation recover after records are removed; no stuck quota counter.
	p.DeleteApiKey("system-one")
	p.DeleteApiKey("system-two")
	if ok, err := p.CreateUserApiKey(k); !ok || err != nil {
		t.Fatal("creation did not recover", ok, err)
	}
}

// Miniredis returns all keys in one SCAN and cannot generate empty nonterminal
// pages or duplicates. Exercise the actual script with a deterministic command
// stub for these permitted Redis SCAN behaviors.
func TestUserApiKeyQuotaScanAdversarialPages(t *testing.T) {
	for _, tc := range []struct {
		name, page                       string
		maxScans, maxKeys, scans, hashes int
	}{
		{"empty-nonterminal", "{}", 3, 20000, 3, 0},
		{"duplicate-returns-count", "{'same','same'}", 128, 3, 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			harness := fmt.Sprintf(`
    KEYS={'candidate'}; ARGV={'prefix*','1','10000','50','%d','%d'}
    scans=0;hashes=0;writes=0
    redis={call=function(cmd,...)
      if cmd=='EXISTS' then return 0 end
      if cmd=='SCAN' then scans=scans+1;return {'1',%s} end
      if cmd=='HMGET' then hashes=hashes+1;return {'1','','1'} end
      if cmd=='HSET' then writes=writes+1;return 1 end
      error('unexpected command '..cmd)
    end}
    result=(function() %s end)()
   `, tc.maxScans, tc.maxKeys, tc.page, createUserApiKeyScript)
			if err := l.DoString(harness); err != nil {
				t.Fatal(err)
			}
			if l.GetGlobal("result") != lua.LNumber(-2) || l.GetGlobal("scans") != lua.LNumber(tc.scans) || l.GetGlobal("hashes") != lua.LNumber(tc.hashes) || l.GetGlobal("writes") != lua.LNumber(0) {
				t.Fatal("unbounded scan or write on rejection", l.GetGlobal("result"), l.GetGlobal("scans"), l.GetGlobal("hashes"), l.GetGlobal("writes"))
			}
		})
	}
}
