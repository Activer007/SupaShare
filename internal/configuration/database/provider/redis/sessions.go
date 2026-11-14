package redis

import (
	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
	"strconv"
	"strings"
)

const (
	prefixSessions = "se:"
)

// GetSession returns the session with the given ID or false if not a valid ID
func (p DatabaseProvider) GetSession(id string) (models.Session, bool) {
	hashmapEntry, ok := p.getHashMap(prefixSessions + id)
	if !ok {
		return models.Session{}, false
	}
	var result models.Session
	err := redigo.ScanStruct(hashmapEntry, &result)
	helper.Check(err)
	return result, true
}

// SaveSession stores the given session. After the expiry passed, it will be deleted automatically
func (p DatabaseProvider) SaveSession(id string, session models.Session) {
	p.setHashMap(p.buildArgs(prefixSessions + id).AddFlat(session))
	p.setExpiryAt(prefixSessions+id, session.ValidUntil)
}

// DeleteSession deletes a session with the given ID
func (p DatabaseProvider) DeleteSession(id string) {
	p.deleteKey(prefixSessions + id)
}

// DeleteAllSessions logs all users out
func (p DatabaseProvider) DeleteAllSessions() {
	p.deleteAllWithPrefix(prefixSessions)
}

// DeleteAllSessionsByUser logs the specific users out
func (p DatabaseProvider) DeleteAllSessionsByUser(userId int) {
	maps := p.getAllHashesWithPrefix(prefixSessions)
	for k, v := range maps {
		var result models.Session
		err := redigo.ScanStruct(v, &result)
		helper.Check(err)
		if result.UserId == userId {
			p.DeleteSession(strings.Replace(k, prefixSessions, "", 1))
		}
	}
}

var saveSessionIfCredentialsScript = redigo.NewScript(2, `
 if redis.call('EXISTS',KEYS[1])==0 or redis.call('EXISTS',KEYS[2])==1 then return 0 end
 if (redis.call('HGET',KEYS[1],'Password') or '') ~= ARGV[1] or (redis.call('HGET',KEYS[1],'ResetPassword') or '0') ~= ARGV[2] or (redis.call('HGET',KEYS[1],'AuthGeneration') or '0') ~= ARGV[3] then return 0 end
 redis.call('HSET',KEYS[2],'renew_at',ARGV[4],'valid_until',ARGV[5],'user_id',ARGV[6],'successor',ARGV[7],'auth_generation',ARGV[3])
 redis.call('EXPIREAT',KEYS[2],ARGV[5]); return 1`)

func (p DatabaseProvider) SaveSessionIfCredentials(id string, session models.Session, expected models.User) (bool, error) {
	if expected.Id <= 0 || session.UserId != expected.Id || session.AuthGeneration != expected.AuthGeneration {
		return false, nil
	}
	conn := p.pool.Get()
	defer conn.Close()
	n, err := redigo.Int(saveSessionIfCredentialsScript.Do(conn, p.dbPrefix+prefixUsers+strconv.Itoa(expected.Id), p.dbPrefix+prefixSessions+id, expected.Password, expected.ResetPassword, expected.AuthGeneration, session.RenewAt, session.ValidUntil, session.UserId, session.Successor))
	return n == 1, err
}
