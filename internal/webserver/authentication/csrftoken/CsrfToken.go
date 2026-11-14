package csrftoken

import (
	"crypto/subtle"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/helper"
)

var tokens = make(map[string]csrfToken)
var mutex sync.Mutex
var cleanupOnce sync.Once

const ttl = 5 * time.Minute
const maxPerPurpose = 4096
const maxPerBinding = 16
const maxPerClient = 64

const (
	TypeLogin    = iota
	TypeApiToken // currently not used
	TypePasswordChange
	TypeLogout
)

type csrfToken struct {
	Type    int
	Expiry  int64
	Binding string
	Client  string
}

func Generate(tokenType int) string {
	return generate(tokenType, "", "")
}

// GenerateBound issues a token for one browser context. Empty bindings fail closed.
func GenerateBound(tokenType int, binding string) string {
	if binding == "" {
		return ""
	}
	return generate(tokenType, binding, "")
}

// GenerateBoundForClient additionally partitions anonymous capacity by trusted client IP.
func GenerateBoundForClient(tokenType int, binding, client string) string {
	if binding == "" || client == "" {
		return ""
	}
	return generate(tokenType, binding, client)
}

func generate(tokenType int, binding, client string) string {
	if tokenType < TypeLogin || tokenType > TypeLogout {
		return ""
	}
	token := helper.GenerateRandomString(20)
	mutex.Lock()
	now := time.Now().Unix()
	purposeCount, bindingCount, clientCount := 0, 0, 0
	for id, existing := range tokens {
		if existing.Expiry <= now {
			delete(tokens, id)
			continue
		}
		if existing.Type != tokenType {
			continue
		}
		purposeCount++
		if binding != "" && existing.Binding == binding {
			bindingCount++
		}
		if client != "" && existing.Client == client {
			clientCount++
		}
	}
	if purposeCount >= maxPerPurpose || bindingCount >= maxPerBinding || clientCount >= maxPerClient {
		mutex.Unlock()
		return ""
	}
	tokens[token] = csrfToken{
		Type:    tokenType,
		Expiry:  time.Now().Add(ttl).Unix(),
		Binding: binding,
		Client:  client,
	}
	mutex.Unlock()

	cleanupOnce.Do(func() {
		go cleanup(true)
	})
	return token
}

func IsValid(tokenType int, tokenId string) bool {
	return isValid(tokenType, tokenId, "")
}

func IsValidBound(tokenType int, tokenId, binding string) bool {
	if binding == "" {
		return false
	}
	return isValid(tokenType, tokenId, binding)
}

func isValid(tokenType int, tokenId, binding string) bool {
	mutex.Lock()
	defer mutex.Unlock()
	token, ok := tokens[tokenId]
	if !ok {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(token.Binding), []byte(binding)) != 1 {
		return false
	}
	delete(tokens, tokenId) //always deletes token, even if it was wrong type
	if token.Type != tokenType {
		return false
	}
	return token.Expiry > time.Now().Unix()
}

func cleanup(periodic bool) {
	mutex.Lock()
	for tokenId, token := range tokens {
		if token.Expiry <= time.Now().Unix() {
			delete(tokens, tokenId)
		}
	}
	mutex.Unlock()
	if periodic {
		time.Sleep(time.Minute)
		go cleanup(true)
	}

}
