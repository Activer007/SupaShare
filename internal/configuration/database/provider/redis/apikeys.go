package redis

import (
	"strings"

	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
)

const (
	prefixApiKeys = "apikey:"
)

func dbToApiKey(id string, input []any) (models.ApiKey, error) {
	var result models.ApiKey
	err := redigo.ScanStruct(input, &result)
	result.Id = strings.Replace(id, prefixApiKeys, "", 1)
	return result, err
}

// GetAllApiKeys returns a map with all API keys
func (p DatabaseProvider) GetAllApiKeys() map[string]models.ApiKey {
	result := make(map[string]models.ApiKey)
	maps := p.getAllHashesWithPrefix(prefixApiKeys)
	for k, v := range maps {
		apiKey, err := dbToApiKey(k, v)
		helper.Check(err)
		result[apiKey.Id] = apiKey
	}
	return result
}

// GetApiKey returns a models.ApiKey if valid or false if the ID is not valid
func (p DatabaseProvider) GetApiKey(id string) (models.ApiKey, bool) {
	result, ok := p.getHashMap(prefixApiKeys + id)
	if !ok {
		return models.ApiKey{}, false
	}
	apikey, err := dbToApiKey(id, result)
	helper.Check(err)
	return apikey, true
}

// GetApiKeyByPublicKey returns an API key by using the public key
func (p DatabaseProvider) GetApiKeyByPublicKey(publicKey string) (string, bool) {
	keys := p.GetAllApiKeys()
	for _, key := range keys {
		if key.PublicId == publicKey {
			return key.Id, true
		}
	}
	return "", false
}

// SaveApiKey saves the API key to the database
func (p DatabaseProvider) SaveApiKey(apikey models.ApiKey) {
	p.setHashMap(p.buildArgs(prefixApiKeys + apikey.Id).AddFlat(apikey))
	if apikey.Expiry != 0 {
		p.setExpiryAt(prefixApiKeys+apikey.Id, apikey.Expiry)
	}
}

var insertApiKey = redigo.NewScript(1, `
if redis.call('EXISTS', KEYS[1]) ~= 0 then return 0 end
redis.call('HSET', KEYS[1], unpack(ARGV, 2))
if tonumber(ARGV[1]) ~= 0 then redis.call('EXPIREAT', KEYS[1], ARGV[1]) end
return 1
`)

// InsertApiKey checks and inserts in one Redis operation, including across instances.
func (p DatabaseProvider) InsertApiKey(apikey models.ApiKey) (bool, error) {
	conn := p.pool.Get()
	defer conn.Close()
	args := redigo.Args{}.Add(p.dbPrefix + prefixApiKeys + apikey.Id).Add(apikey.Expiry).AddFlat(apikey)
	count, err := redigo.Int(insertApiKey.Do(conn, args...))
	return count == 1, err
}

// UpdateTimeApiKey writes the content of LastUsage to the database
func (p DatabaseProvider) UpdateTimeApiKey(apikey models.ApiKey) {
	p.updateUsage(prefixApiKeys+apikey.Id, "LastUsed", apikey.LastUsed)
}

// DeleteApiKey deletes an API key with the given ID
func (p DatabaseProvider) DeleteApiKey(id string) {
	p.deleteKey(prefixApiKeys + id)
}

// Scan and insert execute in one script: no other client can pass the same remaining
// quota concurrently. Counts are derived from records, so imports/deletes/expiry cannot
// desynchronize counters. Prefix glob characters are escaped by the caller.
// At most 128 SCAN batches (COUNT 256 hint) and 20,000 matched records are
// inspected. Saturated shared keyspaces fail closed, leaving deletion available.
const createUserApiKeyScript = `
if redis.call('EXISTS', KEYS[1]) ~= 0 then return 0 end
local cursor = '0'
local total, owned = 0, 0
local seen = {}
local scans, visited = 0, 0
repeat
 scans = scans + 1
 if scans > tonumber(ARGV[5]) then return -2 end
 local page = redis.call('SCAN', cursor, 'MATCH', ARGV[1], 'COUNT', 256)
 cursor = page[1]
 visited = visited + #page[2]
 if visited > tonumber(ARGV[6]) then return -2 end
 for _, key in ipairs(page[2]) do
  if not seen[key] then
   seen[key] = true
   local v = redis.call('HMGET', key, 'IsSystemKey', 'UploadRequestId', 'UserId')
   if (not v[1] or v[1] == '0' or v[1] == 'false') and (not v[2] or v[2] == '') then
    total = total + 1
    if v[3] == ARGV[2] then owned = owned + 1 end
    if total >= tonumber(ARGV[3]) or owned >= tonumber(ARGV[4]) then return -1 end
   end
  end
 end
until cursor == '0'
redis.call('HSET', KEYS[1], unpack(ARGV, 7))
return 1
`

var createUserApiKey = redigo.NewScript(1, createUserApiKeyScript)

func (p DatabaseProvider) CreateUserApiKey(key models.ApiKey) (bool, error) {
	if err := models.ValidateNewUserApiKey(key); err != nil {
		return false, err
	}
	prefix := p.dbPrefix + prefixApiKeys
	pattern := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`).Replace(prefix) + "*"
	conn := p.pool.Get()
	defer conn.Close()
	args := redigo.Args{}.Add(prefix+key.Id, pattern, key.UserId, models.ApiKeyMaxGlobal, models.ApiKeyMaxPerUser, 128, 20000).AddFlat(key)
	n, err := redigo.Int(createUserApiKey.Do(conn, args...))
	if err != nil {
		return false, err
	}
	if n == -2 {
		return false, models.ErrApiKeyAdmissionBusy
	}
	if n == -1 {
		return false, models.ErrApiKeyQuotaExceeded
	}
	return n == 1, nil
}

var updateApiKeyProperties = redigo.NewScript(1, `
if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
redis.call('HSET', KEYS[1], 'FriendlyName', ARGV[1], 'Permissions', ARGV[2])
return 1
`)

// UpdateApiKeyProperties preserves owner, classification and TTL, and never upserts.
func (p DatabaseProvider) UpdateApiKeyProperties(key models.ApiKey) (bool, error) {
	conn := p.pool.Get()
	defer conn.Close()
	n, err := redigo.Int(updateApiKeyProperties.Do(conn, p.dbPrefix+prefixApiKeys+key.Id, key.FriendlyName, key.Permissions))
	return n == 1, err
}
