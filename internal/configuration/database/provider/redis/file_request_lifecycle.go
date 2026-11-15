package redis

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/uploadquota"
	redigo "github.com/gomodule/redigo/redis"
)

const frIndex = "frqidx:"
const keyFileRequestIndexes = frIndex + "version"

func frPattern(s string) string {
	return strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`, `[`, `\[`, `]`, `\]`).Replace(s) + "*"
}
func (p DatabaseProvider) frConnection(ctx context.Context) (redigo.Conn, error) {
	if ctx == nil {
		return nil, uploadquota.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, e := p.pool.GetContext(ctx)
	if e != nil {
		return nil, e
	}
	v, e := redigo.Int(redigo.DoContext(c, ctx, "GET", p.dbPrefix+keyFileRequestIndexes))
	if e != nil || v != 1 {
		c.Close()
		return nil, uploadquota.ErrSchema
	}
	return c, nil
}

// Offline startup/import only. Old writers must be stopped before this rebuild.
func (p DatabaseProvider) initFileRequestIndexes() error {
	c := p.pool.Get()
	defer c.Close()
	if v, e := redigo.Int(c.Do("GET", p.dbPrefix+keyFileRequestIndexes)); e == nil && v == 1 {
		return nil
	}
	// Clear incomplete indexes before retrying migration.
	cursor := 0
	var clearKeys []string
	for {
		a, e := redigo.Values(c.Do("SCAN", cursor, "MATCH", frPattern(p.dbPrefix+frIndex), "COUNT", 256))
		if e != nil {
			return e
		}
		var keys []string
		_, e = redigo.Scan(a, &cursor, &keys)
		if e != nil {
			return e
		}
		clearKeys = append(clearKeys, keys...)
		if cursor == 0 {
			break
		}
	}
	for _, k := range clearKeys {
		if _, e := c.Do("DEL", k); e != nil {
			return e
		}
	}
	for _, r := range p.GetAllFileRequests() {
		if e := p.writeFileRequestIndexed(context.Background(), r, false); e != nil {
			return e
		}
	}
	cursor = 0
	for {
		a, e := redigo.Values(c.Do("SCAN", cursor, "MATCH", frPattern(p.dbPrefix+prefixMetaData), "COUNT", 256))
		if e != nil {
			return e
		}
		var keys []string
		_, e = redigo.Scan(a, &cursor, &keys)
		if e != nil {
			return e
		}
		for _, k := range keys {
			rid, e := redigo.String(c.Do("HGET", k, "FileRequestId"))
			if e != nil && e != redigo.ErrNil {
				return e
			}
			if rid != "" {
				if _, e = c.Do("SADD", p.dbPrefix+frIndex+"files:"+rid, strings.TrimPrefix(k, p.dbPrefix+prefixMetaData)); e != nil {
					return e
				}
			}
		}
		if cursor == 0 {
			break
		}
	}
	for _, k := range p.GetAllApiKeys() {
		if k.UploadRequestId != "" {
			if _, e := c.Do("ZADD", p.dbPrefix+frIndex+"credentials", 0, strconv.Itoa(k.UserId)+":"+p.dbPrefix+prefixApiKeys+k.Id); e != nil {
				return e
			}
			if _, e := c.Do("ZADD", p.dbPrefix+frIndex+"credentials-owner:"+strconv.Itoa(k.UserId), 0, p.dbPrefix+prefixApiKeys+k.Id); e != nil {
				return e
			}
		}
	}
	_, e := c.Do("SET", p.dbPrefix+keyFileRequestIndexes, 1)
	return e
}

const frIndexWriteLua = `
local base=ARGV[1]
local oldowner=redis.call('HGET',KEYS[1],'userid')
local oldcreation=redis.call('HGET',KEYS[1],'creationdate')
local oldid=redis.call('HGET',KEYS[1],'id')
for _,k in ipairs({base..'all',base..'expiry',base..'owner:'..ARGV[3],base..'owner:'..(oldowner or '')}) do local t=redis.call('TYPE',k).ok;if t~='none' and t~='zset' then return redis.error_reply('invalid request index') end end
if oldowner and oldcreation and oldid then
 local member=string.format('%020d',tonumber(oldcreation))..'.'..oldid
 redis.call('ZREM',base..'all',member)
 redis.call('ZREM',base..'owner:'..oldowner,member)
end
redis.call('HSET',KEYS[1],unpack(ARGV,5))
redis.call('ZADD',base..'all',0,ARGV[2])
redis.call('ZADD',base..'owner:'..ARGV[3],0,ARGV[2])
if tonumber(ARGV[4])>0 then redis.call('ZADD',base..'expiry',ARGV[4],oldid or redis.call('HGET',KEYS[1],'id'))
else redis.call('ZREM',base..'expiry',redis.call('HGET',KEYS[1],'id')) end
return 1
`

var frIndexWrite = redigo.NewScript(1, frIndexWriteLua)

func (p DatabaseProvider) writeFileRequestIndexed(ctx context.Context, r models.FileRequest, online bool) error {
	var c redigo.Conn
	var e error
	if online {
		c, e = p.frConnection(ctx)
	} else {
		c, e = p.pool.GetContext(ctx)
	}
	if e != nil {
		return e
	}
	defer c.Close()
	_, e = frIndexWrite.DoContext(ctx, c, redigo.Args{}.Add(p.dbPrefix+prefixFileRequests+r.Id, p.dbPrefix+frIndex, models.FileRequestCursor(r), r.UserId, r.Expiry).AddFlat(r)...)
	return e
}

var createFileRequestPair = redigo.NewScript(2, `
local base=ARGV[1]
if redis.call('EXISTS',KEYS[1])~=0 or redis.call('EXISTS',KEYS[2])~=0 then return 0 end
if redis.call('ZCARD',base..'all')>=10000 or redis.call('ZCARD',base..'owner:'..ARGV[3])>=100 then return -1 end
if redis.call('ZCARD',base..'credentials')>=10000 or redis.call('ZCARD',base..'credentials-owner:'..ARGV[3])>=100 then return -1 end
local split=tonumber(ARGV[6])
for _,k in ipairs({base..'all',base..'owner:'..ARGV[3],base..'expiry',base..'credentials',base..'credentials-owner:'..ARGV[3]}) do local t=redis.call('TYPE',k).ok;if t~='none' and t~='zset' then return redis.error_reply('invalid request index') end end
redis.call('HSET',KEYS[1],unpack(ARGV,7,split))
redis.call('HSET',KEYS[2],unpack(ARGV,split+1))
redis.call('ZADD',base..'credentials',0,ARGV[3]..':'..KEYS[2]);redis.call('ZADD',base..'credentials-owner:'..ARGV[3],0,KEYS[2])
redis.call('ZADD',base..'all',0,ARGV[2]);redis.call('ZADD',base..'owner:'..ARGV[3],0,ARGV[2])
if tonumber(ARGV[4])>0 then redis.call('ZADD',base..'expiry',ARGV[4],redis.call('HGET',KEYS[1],'id')) end
return 1
`)

func (p DatabaseProvider) CreateFileRequestLimited(ctx context.Context, r models.FileRequest, k models.ApiKey) (bool, error) {
	if !r.ValidLifecycle() || r.CreationDate <= 0 || k.PublicId == "" || k.Id != r.ApiKey || k.UserId != r.UserId || k.UploadRequestId != r.Id || k.Permissions != models.ApiPermNone || k.IsSystemKey {
		return false, uploadquota.ErrInvalid
	}
	c, e := p.frConnection(ctx)
	if e != nil {
		return false, e
	}
	defer c.Close()
	fields := redigo.Args{}.AddFlat(r)
	k.Expiry = r.Expiry
	args := redigo.Args{}.Add(p.dbPrefix+prefixFileRequests+r.Id, p.dbPrefix+prefixApiKeys+k.Id, p.dbPrefix+frIndex, models.FileRequestCursor(r), r.UserId, r.Expiry, frPattern(p.dbPrefix+prefixApiKeys), 6+len(fields))
	args = append(args, fields...)
	args = args.AddFlat(k)
	n, e := redigo.Int(createFileRequestPair.DoContext(ctx, c, args...))
	if e != nil {
		return false, e
	}
	if n == -1 {
		return false, models.ErrFileRequestQuota
	}
	if n == -2 {
		return false, uploadquota.ErrFull
	}
	return n == 1, nil
}
func (p DatabaseProvider) ListFileRequestsPage(ctx context.Context, owner int, before string, limit int) ([]models.FileRequest, error) {
	if owner < 0 || limit < 1 || limit > 101 {
		return nil, uploadquota.ErrInvalid
	}
	bound := "+"
	if before != "" {
		if _, _, e := models.ParseFileRequestCursor(before); e != nil {
			return nil, uploadquota.ErrInvalid
		}
		bound = "(" + before
	}
	c, e := p.frConnection(ctx)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	idx := p.dbPrefix + frIndex + "all"
	if owner > 0 {
		idx = p.dbPrefix + frIndex + "owner:" + strconv.Itoa(owner)
	}
	members, e := redigo.Strings(redigo.DoContext(c, ctx, "ZREVRANGEBYLEX", idx, bound, "-", "LIMIT", 0, limit))
	if e != nil {
		return nil, e
	}
	out := make([]models.FileRequest, 0, len(members))
	for _, m := range members {
		_, id, e := models.ParseFileRequestCursor(m)
		if e != nil {
			return nil, uploadquota.ErrInvalid
		}
		v, e := redigo.Values(redigo.DoContext(c, ctx, "HGETALL", p.dbPrefix+prefixFileRequests+id))
		if e != nil {
			return nil, e
		}
		if len(v) == 0 {
			return nil, uploadquota.ErrSchema
		}
		r, e := dbToFileRequest(v)
		if e != nil {
			return nil, e
		}
		if owner > 0 && r.UserId != owner {
			return nil, uploadquota.ErrSchema
		}
		out = append(out, r)
	}
	return out, nil
}
func (p DatabaseProvider) ReadFileRequestFiles(ctx context.Context, id string) (map[string]models.File, error) {
	if !models.ValidFileRequestID(id) {
		return nil, uploadquota.ErrInvalid
	}
	c, e := p.frConnection(ctx)
	if e != nil {
		return nil, e
	}
	defer c.Close()
	key := p.dbPrefix + frIndex + "files:" + id
	out := make(map[string]models.File)
	cursor := 0
	for {
		a, e := redigo.Values(redigo.DoContext(c, ctx, "SSCAN", key, cursor, "COUNT", 256))
		if e != nil {
			return nil, e
		}
		var ids []string
		_, e = redigo.Scan(a, &cursor, &ids)
		if e != nil {
			return nil, e
		}
		for _, fid := range ids {
			v, e := redigo.Values(redigo.DoContext(c, ctx, "HGETALL", p.dbPrefix+prefixMetaData+fid))
			if e != nil {
				return nil, e
			}
			if len(v) == 0 {
				continue
			}
			f, e := dbToMetadata(fid, v)
			if e != nil {
				return nil, e
			}
			if f.UploadRequestId == id {
				out[fid] = f
			}
		}
		if cursor == 0 {
			break
		}
	}
	return out, nil
}

// Compare-and-swap the full persistent request, never overwrite a deleted row.
func (p DatabaseProvider) UpdateFileRequest(ctx context.Context, expected, r models.FileRequest) (bool, error) {
	if !r.ValidLifecycle() || r.Id != expected.Id || r.UserId != expected.UserId || r.ApiKey != expected.ApiKey || r.CreationDate != expected.CreationDate {
		return false, uploadquota.ErrInvalid
	}
	c, e := p.frConnection(ctx)
	if e != nil {
		return false, e
	}
	defer c.Close()
	key := p.dbPrefix + prefixFileRequests + r.Id
	ak := p.dbPrefix + prefixApiKeys + r.ApiKey
	if _, e = redigo.DoContext(c, ctx, "WATCH", key, ak); e != nil {
		return false, e
	}
	defer c.Do("UNWATCH")
	v, e := redigo.Values(redigo.DoContext(c, ctx, "HGETALL", key))
	if e != nil {
		return false, e
	}
	if len(v) == 0 {
		return false, nil
	}
	old, e := dbToFileRequest(v)
	if e != nil {
		return false, e
	}
	expected.UploadedFiles = 0
	expected.CombinedMaxSize = 0
	expected.ReservedUploads = 0
	expected.LastUpload = 0
	expected.TotalFileSize = 0
	expected.FileIdList = nil
	expected.Files = nil
	if !reflect.DeepEqual(old, expected) {
		return false, nil
	}
	k, e := redigo.Strings(redigo.DoContext(c, ctx, "HMGET", ak, "UploadRequestId", "UserId"))
	if e != nil {
		return false, e
	}
	hasKey := len(k) == 2 && k[0] == r.Id && k[1] == strconv.Itoa(r.UserId)
	if r.Expiry != expected.Expiry && (!hasKey || (expected.Expiry > 0 && expected.Expiry <= time.Now().Unix())) {
		return false, uploadquota.ErrConflict
	}
	for _, idx := range []string{p.dbPrefix + frIndex + "all", p.dbPrefix + frIndex + "expiry", p.dbPrefix + frIndex + "owner:" + strconv.Itoa(r.UserId)} {
		if _, e = redigo.DoContext(c, ctx, "WATCH", idx); e != nil {
			return false, e
		}
		kind, e := redigo.String(redigo.DoContext(c, ctx, "TYPE", idx))
		if e != nil {
			return false, e
		}
		if kind != "none" && kind != "zset" {
			return false, uploadquota.ErrSchema
		}
	}
	if _, e = c.Do("MULTI"); e != nil {
		return false, e
	}
	defer c.Do("DISCARD")
	args := redigo.Args{frIndexWriteLua, 1, key, p.dbPrefix + frIndex, models.FileRequestCursor(r), r.UserId, r.Expiry}
	args = args.AddFlat(r)
	c.Send("EVAL", args...)
	if hasKey {
		c.Send("HSET", ak, "Expiry", r.Expiry)
		c.Send("PERSIST", ak)
	}
	results, e := redigo.Values(redigo.DoContext(c, ctx, "EXEC"))
	if e == redigo.ErrNil {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	for _, res := range results {
		if err, ok := res.(error); ok {
			return false, err
		}
	}
	return true, nil
}

const pruneFileRequestLua = `
local base=ARGV[1];local rid=ARGV[2];local now=tonumber(ARGV[3]);local expiry=tonumber(redis.call('HGET',KEYS[1],'expiry') or '0')
if expiry==0 or expiry>now then return 0 end
local api=redis.call('HGET',KEYS[1],'apikey');local owner=redis.call('HGET',KEYS[1],'userid');local created=redis.call('HGET',KEYS[1],'creationdate')
local apiKey=ARGV[4]..(api or '')
if redis.call('HGET',apiKey,'UploadRequestId')==rid then redis.call('DEL',apiKey);redis.call('ZREM',base..'credentials',owner..':'..apiKey);redis.call('ZREM',base..'credentials-owner:'..owner,apiKey) end
-- Large sets conservatively retain the request; never block Redis on full metadata.
if redis.call('SCARD',base..'files:'..rid)>256 then
 local cursor=redis.call('GET',base..'files-cursor:'..rid) or '0'
 local page=redis.call('SSCAN',base..'files:'..rid,cursor,'COUNT',128)
 for _,fid in ipairs(page[2]) do if redis.call('HGET',ARGV[5]..fid,'FileRequestId')~=rid then redis.call('SREM',base..'files:'..rid,fid) end end
 redis.call('SET',base..'files-cursor:'..rid,page[1]);redis.call('ZADD',base..'expiry',now+3600,rid);return 0
end
local files=redis.call('SMEMBERS',base..'files:'..rid)
local hasFiles=false
for _,fid in ipairs(files) do
 if redis.call('HGET',ARGV[5]..fid,'FileRequestId')==rid then hasFiles=true else redis.call('SREM',base..'files:'..rid,fid) end
end
if not hasFiles and ARGV[7]~='1' and expiry<=now-tonumber(ARGV[6]) then
 redis.call('DEL',KEYS[1],base..'files:'..rid,base..'files-cursor:'..rid)
 local member=string.format('%020d',tonumber(created))..'.'..rid
 redis.call('ZREM',base..'all',member);redis.call('ZREM',base..'owner:'..owner,member);redis.call('ZREM',base..'expiry',rid)
 return 1
end
redis.call('ZADD',base..'expiry',now+3600,rid)
return 0
`

func (p DatabaseProvider) PruneFileRequests(ctx context.Context, now int64, limit int) (int, error) {
	if now <= 0 || limit < 1 || limit > 1000 {
		return 0, uploadquota.ErrInvalid
	}
	c, e := p.frConnection(ctx)
	if e != nil {
		return 0, e
	}
	defer c.Close()
	ids, e := redigo.Strings(redigo.DoContext(c, ctx, "ZRANGEBYSCORE", p.dbPrefix+frIndex+"expiry", "-inf", now, "LIMIT", 0, limit))
	if e != nil {
		return 0, e
	}
	// Snapshot reservations and WATCH the ledger: pruning never races a live upload.
	if _, e = redigo.DoContext(c, ctx, "WATCH", p.dbPrefix+keyUploadQuota); e != nil {
		return 0, e
	}
	defer c.Do("UNWATCH")
	ledger, e := p.readUploadQuota(ctx, c)
	if e != nil {
		return 0, e
	}
	active := map[string]bool{}
	for _, r := range ledger.Records {
		if r.State == uploadquota.Reserved || r.State == uploadquota.Uploading || !r.StagingReleased {
			active[r.RequestID] = true
		}
	}
	if _, e = redigo.DoContext(c, ctx, "MULTI"); e != nil {
		return 0, e
	}
	defer c.Do("DISCARD")
	for _, id := range ids {
		flag := 0
		if active[id] {
			flag = 1
		}
		if _, e = redigo.DoContext(c, ctx, "EVAL", pruneFileRequestLua, 1, p.dbPrefix+prefixFileRequests+id, p.dbPrefix+frIndex, id, now, p.dbPrefix+prefixApiKeys, p.dbPrefix+prefixMetaData, models.FileRequestRetentionSeconds, flag); e != nil {
			return 0, e
		}
	}
	results, e := redigo.Values(redigo.DoContext(c, ctx, "EXEC"))
	if e == redigo.ErrNil {
		return 0, nil
	}
	if e != nil {
		return 0, e
	}
	count := 0
	for _, r := range results {
		n, e := redigo.Int(r, nil)
		if e != nil {
			return count, e
		}
		count += n
	}
	if e := p.pruneFileRequestCredentials(ctx, c, now, limit); e != nil {
		return count, e
	}
	return count, nil
}

// Credential indexes intentionally outlive Redis TTL. A bounded maintenance pass
// removes stale members; temporary over-counting fails closed rather than admitting
// more credentials than the configured budget.
const indexedCredentialLua = `
local base=ARGV[1];local mode=ARGV[2];local owner=ARGV[3];local rid=ARGV[4]
if mode=='insert' and redis.call('EXISTS',KEYS[1])~=0 then return 0 end
local oldowner=redis.call('HGET',KEYS[1],'UserId');local oldrid=redis.call('HGET',KEYS[1],'UploadRequestId')
for _,k in ipairs({base..'credentials',base..'credentials-owner:'..owner,base..'credentials-owner:'..(oldowner or '')}) do
 local t=redis.call('TYPE',k).ok;if t~='none' and t~='zset' then return redis.error_reply('invalid credential index') end
end
if oldowner and oldrid and oldrid~='' then redis.call('ZREM',base..'credentials',oldowner..':'..KEYS[1]);redis.call('ZREM',base..'credentials-owner:'..oldowner,KEYS[1]) end
if mode=='delete' then redis.call('DEL',KEYS[1]);return 1 end
redis.call('HSET',KEYS[1],unpack(ARGV,6))
if rid~='' then redis.call('ZADD',base..'credentials',0,owner..':'..KEYS[1]);redis.call('ZADD',base..'credentials-owner:'..owner,0,KEYS[1]) end
if tonumber(ARGV[5])>0 then redis.call('EXPIREAT',KEYS[1],ARGV[5]) else redis.call('PERSIST',KEYS[1]) end
return 1
`

var indexedCredential = redigo.NewScript(1, indexedCredentialLua)
var pruneCredential = redigo.NewScript(1, `
local member=ARGV[1];local split=string.find(member,':');if not split then return 0 end
local owner=string.sub(member,1,split-1);local k=string.sub(member,split+1)
local rid=redis.call('HGET',k,'UploadRequestId');local expiry=tonumber(redis.call('HGET',k,'Expiry') or '0')
local currentowner=redis.call('HGET',k,'UserId')
local remove=not rid or rid=='' or currentowner~=owner
if rid and rid~='' then
 if redis.call('EXISTS',ARGV[2]..rid)==0 or (expiry>0 and expiry<=tonumber(ARGV[3])) then redis.call('DEL',k);remove=true end
end
if remove then redis.call('ZREM',KEYS[1],member);redis.call('ZREM',ARGV[4]..owner,k) end
return 1
`)

func (p DatabaseProvider) pruneFileRequestCredentials(ctx context.Context, c redigo.Conn, now int64, limit int) error {
	// Lexicographic cursor gives bounded work even when all entries are live.
	cursorKey := p.dbPrefix + frIndex + "credential-cursor"
	cursor, e := redigo.String(redigo.DoContext(c, ctx, "GET", cursorKey))
	if e != nil && e != redigo.ErrNil {
		return e
	}
	bound := "-"
	if cursor != "" {
		bound = "(" + cursor
	}
	idx := p.dbPrefix + frIndex + "credentials"
	members, e := redigo.Strings(redigo.DoContext(c, ctx, "ZRANGEBYLEX", idx, bound, "+", "LIMIT", 0, limit))
	if e != nil {
		return e
	}
	for _, m := range members {
		if _, e = pruneCredential.DoContext(ctx, c, idx, m, p.dbPrefix+prefixFileRequests, now, p.dbPrefix+frIndex+"credentials-owner:"); e != nil {
			return e
		}
	}
	next := ""
	if len(members) == limit {
		next = members[len(members)-1]
	}
	_, e = redigo.DoContext(c, ctx, "SET", cursorKey, next)
	return e
}
