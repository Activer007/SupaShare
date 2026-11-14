package redis

import (
	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
	"strings"
	"time"
)

// Record-derived quotas cannot drift on revocation, expiry or imports. The
// bounded scan and write run in one Lua invocation across all application nodes.
const createTemporaryApiKeyScript = `
if redis.call('EXISTS', KEYS[1]) ~= 0 then return 0 end
local cursor, scans, visited, total, owned, pruned = '0', 0, 0, 0, 0, 0
local seen = {}
repeat
 scans = scans + 1
 if scans > 128 then return -2 end
 local page = redis.call('SCAN', cursor, 'MATCH', ARGV[1], 'COUNT', 256)
 cursor = page[1]
 visited = visited + #page[2]
 if visited > 20000 then return -2 end
 for _, key in ipairs(page[2]) do
  if not seen[key] then
   seen[key] = true
   local v = redis.call('HMGET', key, 'IsSystemKey', 'UploadRequestId', 'UserId', 'Expiry')
   if (v[1] == '1' or v[1] == 'true') and (not v[2] or v[2] == '') then
    local expiry = tonumber(v[4]) or 0
    if expiry > 0 and expiry <= tonumber(ARGV[5]) and pruned < 256 then
     redis.call('DEL', key)
     pruned = pruned + 1
    else
     total = total + 1
     if v[3] == ARGV[2] then owned = owned + 1 end
    end
   end
  end
 end
until cursor == '0'
if total >= tonumber(ARGV[3]) or owned >= tonumber(ARGV[4]) then return -1 end
redis.call('HSET', KEYS[1], unpack(ARGV, 7))
redis.call('EXPIREAT', KEYS[1], ARGV[6])
return 1
`

var createTemporaryApiKey = redigo.NewScript(1, createTemporaryApiKeyScript)

func (p DatabaseProvider) CreateTemporaryApiKey(key models.ApiKey) (bool, error) {
	now := time.Now().Unix()
	if err := models.ValidateTemporaryApiKey(key, now); err != nil {
		return false, err
	}
	prefix := p.dbPrefix + prefixApiKeys
	pattern := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`).Replace(prefix) + "*"
	conn := p.pool.Get()
	defer conn.Close()
	args := redigo.Args{}.Add(prefix+key.Id, pattern, key.UserId, models.TemporaryApiKeyMaxGlobal, models.TemporaryApiKeyMaxPerUser, now, key.Expiry).AddFlat(key)
	n, err := redigo.Int(createTemporaryApiKey.Do(conn, args...))
	if err != nil {
		return false, err
	}
	if n == -2 {
		return false, models.ErrApiKeyAdmissionBusy
	}
	if n == -1 {
		return false, models.ErrTemporaryApiKeyQuotaExceeded
	}
	return n == 1, nil
}
