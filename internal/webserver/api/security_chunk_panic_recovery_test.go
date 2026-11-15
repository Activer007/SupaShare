package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
	"github.com/Activer007/SupaShare/internal/webserver/fileupload"
)

func TestCompletePreparedChunkNilTaskSafe(t *testing.T) {
	// Calling completePreparedChunk with nil task must safely return without panicking
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("completePreparedChunk panicked on nil task: %v", r)
		}
	}()
	completePreparedChunk(nil, nil, models.User{}, models.UploadParameters{}, true)
	completePreparedChunk(nil, nil, models.User{}, models.UploadParameters{}, false)
}

func TestCompletePreparedChunkSyncPanicReturns500(t *testing.T) {
	rec := httptest.NewRecorder()
	// A task that panics during run or accounting should be recovered and write 500 in sync mode
	task := &fileupload.PreparedCompletion{}
	completePreparedChunk(rec, task, models.User{}, models.UploadParameters{}, false)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected HTTP 500 on panic recovery in sync mode, got %d", rec.Code)
	}
}
