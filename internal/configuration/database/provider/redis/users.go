package redis

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	"github.com/Activer007/SupaShare/internal/helper"
	"github.com/Activer007/SupaShare/internal/models"
	redigo "github.com/gomodule/redigo/redis"
)

const (
	prefixUsers         = "users:"
	prefixUserIdCounter = "userid_max"
)

func dbToUser(input []any) (models.User, error) {
	var result models.User
	err := redigo.ScanStruct(input, &result)
	if err != nil {
		return models.User{}, err
	}
	return result, nil
}

// GetAllUsers returns a map with all users
func (p DatabaseProvider) GetAllUsers() []models.User {
	var result []models.User
	maps := p.getAllHashesWithPrefix(prefixUsers)
	for _, v := range maps {
		user, err := dbToUser(v)
		helper.Check(err)
		result = append(result, user)
	}
	return sortUsers(result)
}

func sortUsers(users []models.User) []models.User {
	slices.SortFunc(users, func(a, b models.User) int {
		return cmp.Or(
			cmp.Compare(a.UserLevel, b.UserLevel),
			cmp.Compare(b.LastOnline, a.LastOnline),
			cmp.Compare(a.Name, b.Name),
		)
	})
	return users
}

// GetUserByName returns a models.User if valid or false if the email is not valid
func (p DatabaseProvider) GetUserByName(username string) (models.User, bool) {
	users := p.GetAllUsers()
	for _, user := range users {
		if user.Name == username {
			return user, true
		}
	}
	return models.User{}, false
}

// GetUser returns a models.User if valid or false if the ID is not valid
func (p DatabaseProvider) GetUser(id int) (models.User, bool) {
	result, ok := p.getHashMap(prefixUsers + strconv.Itoa(id))
	if !ok {
		return models.User{}, false
	}
	user, err := dbToUser(result)
	helper.Check(err)
	return user, true
}

// SaveUser is a trusted creation/import upsert, not an authentication mutation API.
// Use narrow compare-and-update methods for live credential and profile changes.
// If isNewUser is true, a new Id will be generated.
func (p DatabaseProvider) SaveUser(user models.User, isNewUser bool) {
	if isNewUser {
		id := p.getIncreasedInt(prefixUserIdCounter)
		user.Id = id
	} else {
		counter, _ := p.getKeyInt(prefixUserIdCounter)
		if counter < user.Id {
			p.setKey(prefixUserIdCounter, user.Id)
		}
	}
	conn := p.pool.Get()
	defer conn.Close()
	_, err := saveUserGenerationScript.Do(conn, p.dbPrefix+prefixUsers+strconv.Itoa(user.Id), user.Id, user.Name, user.Password, user.Permissions, user.UserLevel, user.LastOnline, user.ResetPassword, user.AuthGeneration)
	helper.Check(err)
}

// UpdateUserLastOnline writes the last online time to the database
func (p DatabaseProvider) UpdateUserLastOnline(id int) {
	p.updateUsage(prefixUsers+strconv.Itoa(id), "LastOnline", time.Now().Unix())
}

// DeleteUser deletes a user with the given ID
func (p DatabaseProvider) DeleteUser(id int) {
	conn := p.pool.Get()
	defer conn.Close()
	_, err := deleteOIDCUserScript.Do(conn, p.dbPrefix+oidcBindings, p.dbPrefix+oidcUsers, p.dbPrefix+prefixUsers+strconv.Itoa(id), id)
	helper.Check(err)
}

// Compare and update in one Redis operation; never recreate a deleted user or
// overwrite unrelated fields (such as Name and LastOnline) from an old snapshot.
var updateUserResetCredentialsScript = redigo.NewScript(1, `
 if redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
 if (redis.call('HGET', KEYS[1], 'Password') or '') ~= ARGV[1] then return 0 end
 if (redis.call('HGET', KEYS[1], 'ResetPassword') or '0') ~= ARGV[2] then return 0 end
 if redis.call('HGET', KEYS[1], 'UserLevel') ~= ARGV[3] then return 0 end
 if redis.call('HGET', KEYS[1], 'Permissions') ~= ARGV[4] then return 0 end
 if (redis.call('HGET', KEYS[1], 'AuthGeneration') or '0') ~= ARGV[7] then return 0 end
 redis.call('HSET', KEYS[1], 'Password', ARGV[5], 'ResetPassword', ARGV[6])
 redis.call('HINCRBY', KEYS[1], 'AuthGeneration', 1)
 return 1`)

func (p DatabaseProvider) UpdateUserResetCredentials(expected models.User, password string, reset bool) (bool, error) {
	if expected.Id <= 0 {
		return false, nil
	}
	conn := p.pool.Get()
	defer conn.Close()
	updated, err := redigo.Int(updateUserResetCredentialsScript.Do(conn,
		p.dbPrefix+prefixUsers+strconv.Itoa(expected.Id), expected.Password,
		expected.ResetPassword, expected.UserLevel, expected.Permissions, password, reset, expected.AuthGeneration))
	return updated == 1, err
}

// Trusted imports preserve the current generation unless credentials change.
var saveUserGenerationScript = redigo.NewScript(1, `
 local generation = ARGV[8]
 if redis.call('EXISTS',KEYS[1]) == 1 then
  generation = redis.call('HGET',KEYS[1],'AuthGeneration') or '0'
  if (redis.call('HGET',KEYS[1],'Password') or '') ~= ARGV[3] or (redis.call('HGET',KEYS[1],'ResetPassword') or '0') ~= ARGV[7] then generation=tonumber(generation)+1 end
 end
 redis.call('HSET',KEYS[1],'id',ARGV[1],'Name',ARGV[2],'Password',ARGV[3],'Permissions',ARGV[4],'UserLevel',ARGV[5],'LastOnline',ARGV[6],'ResetPassword',ARGV[7],'AuthGeneration',generation)
 return 1`)

var rehashPasswordScript = redigo.NewScript(1, `
 if redis.call('EXISTS',KEYS[1])==0 then return 0 end
 if (redis.call('HGET',KEYS[1],'Password') or '') ~= ARGV[1] or (redis.call('HGET',KEYS[1],'ResetPassword') or '0') ~= ARGV[2] or (redis.call('HGET',KEYS[1],'AuthGeneration') or '0') ~= ARGV[3] then return 0 end
 redis.call('HSET',KEYS[1],'Password',ARGV[4]); return 1`)

func (p DatabaseProvider) RehashPassword(expected models.User, password string) (bool, error) {
	conn := p.pool.Get()
	defer conn.Close()
	n, err := redigo.Int(rehashPasswordScript.Do(conn, p.dbPrefix+prefixUsers+strconv.Itoa(expected.Id), expected.Password, expected.ResetPassword, expected.AuthGeneration, password))
	return n == 1, err
}

var updateUserProfileScript = redigo.NewScript(1, `
 if redis.call('EXISTS',KEYS[1])==0 then return 0 end
 if (redis.call('HGET',KEYS[1],'Password') or '') ~= ARGV[1] or (redis.call('HGET',KEYS[1],'ResetPassword') or '0') ~= ARGV[2] or (redis.call('HGET',KEYS[1],'AuthGeneration') or '0') ~= ARGV[3] or redis.call('HGET',KEYS[1],'Permissions') ~= ARGV[4] or redis.call('HGET',KEYS[1],'UserLevel') ~= ARGV[5] then return 0 end
 redis.call('HSET',KEYS[1],'Name',ARGV[6],'Permissions',ARGV[7],'UserLevel',ARGV[8]);return 1`)

func (p DatabaseProvider) UpdateUserProfile(expected, updated models.User) (bool, error) {
	if expected.Id <= 0 || expected.Id != updated.Id {
		return false, nil
	}
	conn := p.pool.Get()
	defer conn.Close()
	n, err := redigo.Int(updateUserProfileScript.Do(conn, p.dbPrefix+prefixUsers+strconv.Itoa(expected.Id), expected.Password, expected.ResetPassword, expected.AuthGeneration, expected.Permissions, expected.UserLevel, updated.Name, updated.Permissions, updated.UserLevel))
	return n == 1, err
}
