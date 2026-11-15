package api

import (
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
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
