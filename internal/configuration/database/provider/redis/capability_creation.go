package redis

import (
	"crypto/rand"
	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

var claimCapabilityHotlink = redigo.NewScript(1, `
local kind = redis.call('TYPE', KEYS[1]).ok
if kind ~= 'none' then
 if kind == 'string' and redis.call('GET', KEYS[1]) == ARGV[1] then return 1 end
 return 0
end
redis.call('SET', KEYS[1], ARGV[1]); return 1
`)

func (p DatabaseProvider) ClaimHotlink(file models.File) error {
	if file.HotlinkId == "" || file.Id == "" {
		return uploadquota.ErrInvalid
	}
	conn := p.pool.Get()
	defer conn.Close()
	ok, err := redigo.Int(claimCapabilityHotlink.Do(conn, p.dbPrefix+prefixHotlinks+file.HotlinkId, file.Id))
	if err != nil {
		return err
	}
	if ok != 1 {
		return uploadquota.ErrConflict
	}
	return nil
}

var createCapabilityMetadata = redigo.NewScript(5, `
if redis.call('EXISTS', KEYS[1]) ~= 0 then return 0 end
local rt = redis.call('TYPE', KEYS[2]).ok
if rt ~= 'none' and rt ~= 'string' then return redis.error_reply('invalid revision type') end
if ARGV[2] ~= '' then
 local ht = redis.call('TYPE', KEYS[3]).ok
 if ht ~= 'none' and (ht ~= 'string' or redis.call('GET', KEYS[3]) ~= ARGV[3]) then return 0 end
end
local indexRequestFile = false
if ARGV[4] ~= '' then
 local version = redis.call('GET', KEYS[5])
 if version then
  if version ~= '1' then return redis.error_reply('unsupported request index version') end
  local ft = redis.call('TYPE', KEYS[4]).ok
  if ft ~= 'none' and ft ~= 'set' then return redis.error_reply('invalid request file index') end
  indexRequestFile = true
 end
end
redis.call('SET', KEYS[2], ARGV[1])
redis.call('HMSET', KEYS[1], unpack(ARGV, 5))
if ARGV[2] ~= '' then redis.call('SET', KEYS[3], ARGV[3]) end
if indexRequestFile then redis.call('SADD', KEYS[4], ARGV[3]) end
return 1
`)

func (p DatabaseProvider) CreateFileMetadata(file models.File) error {
	if file.Id == "" {
		return uploadquota.ErrInvalid
	}
	fields, err := marshalEncryptionInfo(file)
	if err != nil {
		return err
	}
	// N13 installs these indexes at upgrade. Standalone N15 deployments have
	// no marker, while combined deployments publish membership atomically.
	args := redigo.Args{}.Add(p.dbPrefix+prefixMetaData+file.Id, p.dbPrefix+keyMetadataRevision,
		p.dbPrefix+prefixHotlinks+file.HotlinkId, p.dbPrefix+"frqidx:files:"+file.UploadRequestId,
		p.dbPrefix+"frqidx:version", rand.Text(), file.HotlinkId, file.Id, file.UploadRequestId).AddFlat(fields)
	conn := p.pool.Get()
	defer conn.Close()
	ok, err := redigo.Int(createCapabilityMetadata.Do(conn, args...))
	if err != nil {
		return err
	}
	if ok != 1 {
		return uploadquota.ErrConflict
	}
	return nil
}
