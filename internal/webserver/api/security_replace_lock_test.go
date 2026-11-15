package api

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/api/mutex/apimutex"
)

func TestReplaceFileLocksSourceOnNonDelete(t *testing.T) {
	sourceID := "source-locked-file-test"
	targetID := "target-locked-file-test"

	// Lock the source file under TypeMetaData
	unlockSource := apimutex.LockMany(apimutex.TypeMetaData, sourceID)

	done := make(chan struct{})
	go func() {
		w := httptest.NewRecorder()
		param := &paramFilesReplace{
			Id:            targetID,
			IdNewContent:  sourceID,
			DeleteNewFile: false,
		}
		// This must block trying to acquire lock on sourceID even when DeleteNewFile is false
		apiReplaceFile(w, param, models.User{Id: 1}, models.ApiKey{})
		close(done)
	}()

	select {
	case <-done:
		unlockSource()
		t.Fatal("apiReplaceFile did not lock IdNewContent when DeleteNewFile is false")
	case <-time.After(100 * time.Millisecond):
		// Expected: apiReplaceFile is blocked waiting for sourceID lock
	}

	// Release source lock, allowing apiReplaceFile to proceed
	unlockSource()

	select {
	case <-done:
		// Successfully unblocked and finished after lock release
	case <-time.After(2 * time.Second):
		t.Fatal("apiReplaceFile timed out after releasing source lock")
	}
}
