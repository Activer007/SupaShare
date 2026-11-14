package redis

import (
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

// Real Redis SCAN can return duplicate keys and empty nonterminal pages, unlike
// miniredis. Exercise the production script with deterministic protocol replies.
func TestN04TemporaryScanBudgets(t *testing.T) {
	for _, tc := range []struct {
		name, page string
		hashes     int
	}{{"empty", "{}", 0}, {"duplicates", "{'same','same'}", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			harness := fmt.Sprintf(`KEYS={'candidate'};ARGV={'prefix*','1','4096','64','100','400'}
  scans=0;hashes=0;writes=0
  redis={call=function(cmd,...)
   if cmd=='EXISTS' then return 0 end
   if cmd=='SCAN' then scans=scans+1;return {'1',%s} end
   if cmd=='HMGET' then hashes=hashes+1;return {'1','','1','400'} end
   if cmd=='HSET' then writes=writes+1;return 1 end
   error('unexpected '..cmd)
  end}
  result=(function() %s end)()`, tc.page, createTemporaryApiKeyScript)
			if err := l.DoString(harness); err != nil {
				t.Fatal(err)
			}
			if l.GetGlobal("result") != lua.LNumber(-2) || l.GetGlobal("scans") != lua.LNumber(128) || l.GetGlobal("hashes") != lua.LNumber(tc.hashes) || l.GetGlobal("writes") != lua.LNumber(0) {
				t.Fatal("scan unbounded or wrote on refusal")
			}
		})
	}
}
