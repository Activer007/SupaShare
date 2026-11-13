package redis

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
)

const oidcBindings = "oidc:identities"
const oidcUsers = "oidc:users"

func oidcKey(issuer, subject string) string {
	b, _ := json.Marshal([2]string{issuer, subject})
	return string(b)
}

// Both directions are changed in one Redis operation. Existing bindings are immutable.
var bindOIDCScript = redigo.NewScript(3, `
 if redis.call('EXISTS', KEYS[3]) == 0 then return 0 end
 local old = redis.call('HGET',KEYS[1],ARGV[1])
 local inverse = redis.call('HGET',KEYS[2],ARGV[2])
 if old and old ~= ARGV[2] then return 0 end
 if inverse and inverse ~= ARGV[1] then return 0 end
 redis.call('HSET',KEYS[1],ARGV[1],ARGV[2])
 redis.call('HSET',KEYS[2],ARGV[2],ARGV[1])
 return 1`)

func (p DatabaseProvider) BindOIDCIdentity(issuer, subject string, userID int) error {
	if !(models.OIDCIdentity{Issuer: issuer, Subject: subject, UserID: userID}).Valid() {
		return models.ErrOIDCInvalidIdentity
	}
	conn := p.pool.Get()
	defer conn.Close()
	ok, err := redigo.Int(bindOIDCScript.Do(conn, p.dbPrefix+oidcBindings, p.dbPrefix+oidcUsers, p.dbPrefix+prefixUsers+strconv.Itoa(userID), oidcKey(issuer, subject), userID))
	if err != nil {
		return err
	}
	if ok != 1 {
		return models.ErrOIDCBindingConflict
	}
	return nil
}

var getOIDCScript = redigo.NewScript(2, `
 local id=redis.call('HGET',KEYS[1],ARGV[1])
 if not id then return {} end
 if redis.call('HGET',KEYS[2],id) ~= ARGV[1] then return redis.error_reply('inconsistent OIDC binding') end
 return redis.call('HGETALL',ARGV[2]..id)`)

func (p DatabaseProvider) GetOIDCUser(issuer, subject string) (models.User, bool, error) {
	if !(models.OIDCIdentity{Issuer: issuer, Subject: subject, UserID: 1}).Valid() {
		return models.User{}, false, models.ErrOIDCInvalidIdentity
	}
	conn := p.pool.Get()
	defer conn.Close()
	values, err := redigo.Values(getOIDCScript.Do(conn, p.dbPrefix+oidcBindings, p.dbPrefix+oidcUsers, oidcKey(issuer, subject), p.dbPrefix+prefixUsers))
	if err != nil {
		return models.User{}, false, err
	}
	if len(values) == 0 {
		return models.User{}, false, nil
	}
	u, err := dbToUser(values)
	if err != nil {
		return models.User{}, false, err
	}
	return u, true, nil
}

func (p DatabaseProvider) ListOIDCIdentities() ([]models.OIDCIdentity, error) {
	conn := p.pool.Get()
	defer conn.Close()
	entries, err := redigo.StringMap(conn.Do("HGETALL", p.dbPrefix+oidcBindings))
	if err != nil {
		return nil, err
	}
	var result []models.OIDCIdentity
	for key, value := range entries {
		var tuple [2]string
		if err = json.Unmarshal([]byte(key), &tuple); err != nil {
			return nil, err
		}
		id, err := strconv.Atoi(value)
		if err != nil {
			return nil, err
		}
		identity := models.OIDCIdentity{Issuer: tuple[0], Subject: tuple[1], UserID: id}
		if !identity.Valid() {
			return nil, models.ErrOIDCInvalidIdentity
		}
		result = append(result, identity)
	}
	return result, nil
}

var deleteOIDCUserScript = redigo.NewScript(3, `
 local identity=redis.call('HGET',KEYS[2],ARGV[1])
 if identity then redis.call('HDEL',KEYS[1],identity) end
 redis.call('HDEL',KEYS[2],ARGV[1])
 redis.call('DEL',KEYS[3])
 return 1`)

var createOIDCUserScript = redigo.NewScript(3, `
 if redis.call('HEXISTS',KEYS[1],ARGV[1]) == 1 then return 0 end
 -- Existing storage has no username index. Inspect exact prefixed user hashes;
 -- all decisions and writes remain one atomic operation, including other writers.
 local cursor='0'
 repeat
  local page=redis.call('SCAN',cursor)
  cursor=page[1]
  for _,key in ipairs(page[2]) do
   if string.sub(key,1,string.len(ARGV[2])) == ARGV[2] then
    if redis.call('HGET',key,'Name') == ARGV[3] then return 0 end
   end
  end
 until cursor == '0'
 local id=tostring(redis.call('INCR',KEYS[3]))
 local userkey=ARGV[2]..id
 if redis.call('EXISTS',userkey)==1 or redis.call('HEXISTS',KEYS[2],id)==1 then return 0 end
 redis.call('HSET',userkey,'id',id,'Name',ARGV[3],'Permissions',ARGV[4],'UserLevel','2','LastOnline','0','Password','','ResetPassword','0')
 redis.call('HSET',KEYS[1],ARGV[1],id)
 redis.call('HSET',KEYS[2],id,ARGV[1])
 return id`)

func (p DatabaseProvider) CreateOIDCUser(issuer, subject, name string, permissions models.UserPermission) (models.User, error) {
	name = strings.ToLower(name)
	if !(models.OIDCIdentity{Issuer: issuer, Subject: subject, UserID: 1}).Valid() || name == "" {
		return models.User{}, models.ErrOIDCInvalidIdentity
	}
	conn := p.pool.Get()
	defer conn.Close()
	id, err := redigo.Int(createOIDCUserScript.Do(conn, p.dbPrefix+oidcBindings, p.dbPrefix+oidcUsers, p.dbPrefix+prefixUserIdCounter, oidcKey(issuer, subject), p.dbPrefix+prefixUsers, name, permissions))
	if err != nil {
		return models.User{}, err
	}
	if id == 0 {
		return models.User{}, models.ErrOIDCBindingConflict
	}
	return models.User{Id: id, Name: name, Permissions: permissions, UserLevel: models.UserLevelUser}, nil
}
