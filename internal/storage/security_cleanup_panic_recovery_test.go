package storage

import (
	"net/http/httptest"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestCleanUpPanicRecovery(t *testing.T) {
	// CleanUp should never panic even if executed
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CleanUp panicked unexpectedly: %v", r)
		}
	}()
	CleanUp(false)
}

func TestServeAdmittedFileNegativeSizeTrafficProtection(t *testing.T) {
	// Negative file size must not cause integer underflow or crash
	file := models.File{
		Id:        "test-negative-size-protect",
		SizeBytes: -1,
		UserId:    1,
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/d/test-negative-size-protect", nil)
	// serveAdmittedFile should handle negative size safely without uint64 underflow
	_ = serveAdmittedFile(file, w, r, true, false)
}
