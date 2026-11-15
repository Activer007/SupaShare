package fileupload

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Activer007/SupaShare/internal/storage/uploadreceive"
)

func TestReceiveTimeoutStatus(t *testing.T) {
	if status := ReceiveErrorStatus(errors.Join(uploadreceive.ErrTimeout, uploadreceive.ErrInvalid)); status != http.StatusRequestTimeout {
		t.Fatalf("timeout hidden by multipart parse error: status=%d", status)
	}
}
