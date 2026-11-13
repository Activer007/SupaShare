package errorHandling

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Activer007/SupaShare/internal/helper"
)

var tokens = make(map[string]DisplayedError)
var mutex sync.RWMutex
var cleanupOnce sync.Once

const (
	ttl                 = 5 * time.Minute
	cleanupInterval     = time.Minute
	maxErrorEntries     = 256
	maxErrorEntryBytes  = 8 * 1024
	errorEntryBaseBytes = 128
)

const WidthDefault = "20rem"
const WidthWide = "30rem"
const WidthVeryWide = "65%"

const (
	TypeFileNotFound = iota
	TypeInvalidFileRequest
	TypeE2ECipher
	TypeOAuthNotAuthorised
	TypeOAuthNonGeneric
)

type DisplayedError struct {
	Title                string
	Message              string
	OAuthProviderMessage string
	CardWidth            string
	ErrorId              int
	IsGeneric            bool
	expiry               int64
}

func (d DisplayedError) IsExpired() bool {
	return d.expiry < time.Now().Unix()
}

func RedirectToErrorPage(w http.ResponseWriter, r *http.Request, errorTitle, errorMessage, cardWidth string) {
	result := DisplayedError{
		Title:     errorTitle,
		Message:   errorMessage,
		expiry:    time.Now().Add(ttl).Unix(),
		CardWidth: cardWidth,
	}
	redirectToError(w, r, result)
}

func RedirectGenericErrorPage(w http.ResponseWriter, r *http.Request, genericType int) {
	var cardWidth string
	switch genericType {
	case TypeFileNotFound:
		cardWidth = WidthDefault
	case TypeInvalidFileRequest:
		cardWidth = WidthWide
	case TypeE2ECipher:
		cardWidth = WidthVeryWide
	case TypeOAuthNotAuthorised:
		cardWidth = WidthWide
	default:
		redirectToError(w, r, DisplayedError{
			Title:     "error.unknown.title",
			Message:   "SupaShare cannot display this error (error code " + strconv.Itoa(genericType) + ")",
			CardWidth: WidthWide,
			expiry:    time.Now().Add(ttl).Unix(),
		})
		return
	}

	result := DisplayedError{
		expiry:    time.Now().Add(ttl).Unix(),
		ErrorId:   genericType,
		IsGeneric: true,
		CardWidth: cardWidth,
	}
	redirectToError(w, r, result)
}

func RedirectToOAuthErrorPage(w http.ResponseWriter, r *http.Request, errorMessage string, _ error) {
	if r.URL.Query().Get("error") == "access_denied" {
		result := DisplayedError{
			Title:     "error.accessDenied.title",
			Message:   "error.accessDenied.help",
			expiry:    time.Now().Add(ttl).Unix(),
			ErrorId:   TypeOAuthNonGeneric,
			IsGeneric: false,
		}
		redirectToError(w, r, result)
		return
	}
	// Provider diagnostics come from a public callback and may be arbitrarily
	// large. Keep only the server-selected context needed by the error page.
	result := DisplayedError{
		Title:     "error.unknown.title",
		Message:   errorMessage,
		expiry:    time.Now().Add(ttl).Unix(),
		ErrorId:   TypeOAuthNonGeneric,
		IsGeneric: false,
	}
	redirectToError(w, r, result)
}

func redirectToError(w http.ResponseWriter, r *http.Request, displayedError DisplayedError) {
	displayedError = boundedError(displayedError)
	token := helper.GenerateRandomString(30)
	mutex.Lock()
	cleanupExpiredLocked(time.Now().Unix())
	if len(tokens) >= maxErrorEntries {
		token = ""
	} else {
		tokens[token] = displayedError
	}
	mutex.Unlock()

	if token != "" {
		cleanupOnce.Do(func() {
			go cleanup(true)
		})
	}
	http.Redirect(w, r, "./error?e="+token, http.StatusTemporaryRedirect)
}

func Get(r *http.Request) DisplayedError {
	if !r.URL.Query().Has("e") {
		return DisplayedError{
			IsGeneric: true,
			ErrorId:   TypeFileNotFound,
			CardWidth: WidthDefault,
		}
	}
	mutex.Lock()
	displayedError, ok := tokens[r.URL.Query().Get("e")]
	if ok {
		delete(tokens, r.URL.Query().Get("e"))
	}
	mutex.Unlock()
	if !ok || displayedError.IsExpired() {
		return DisplayedError{
			Title:     "error.unknownID.title",
			Message:   "error.unknownID.help",
			CardWidth: WidthDefault,
		}
	}
	return displayedError
}

func boundedError(displayedError DisplayedError) DisplayedError {
	remaining := maxErrorEntryBytes - errorEntryBaseBytes
	for _, field := range []string{
		displayedError.Title,
		displayedError.Message,
		displayedError.OAuthProviderMessage,
		displayedError.CardWidth,
	} {
		if len(field) > remaining {
			return DisplayedError{
				Title:     "error.unknown.title",
				Message:   "error.unknownID.help",
				CardWidth: WidthWide,
				expiry:    displayedError.expiry,
			}
		}
		remaining -= len(field)
	}
	displayedError.Title = strings.Clone(displayedError.Title)
	displayedError.Message = strings.Clone(displayedError.Message)
	displayedError.OAuthProviderMessage = strings.Clone(displayedError.OAuthProviderMessage)
	displayedError.CardWidth = strings.Clone(displayedError.CardWidth)
	return displayedError
}

func cleanupExpiredLocked(now int64) {
	for id, token := range tokens {
		if token.expiry < now {
			delete(tokens, id)
		}
	}
}

func cleanup(periodic bool) {
	for {
		mutex.Lock()
		cleanupExpiredLocked(time.Now().Unix())
		mutex.Unlock()
		if !periodic {
			return
		}
		time.Sleep(cleanupInterval)
	}
}
