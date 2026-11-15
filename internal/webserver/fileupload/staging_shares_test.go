package fileupload

import (
	"github.com/Activer007/SupaShare/internal/environment"
	"testing"
)

func TestWholeUploadStagingOverrides(t *testing.T) {
	e := environment.Environment{UploadUserTotalMB: 5120, UploadUserMaxFiles: 1000, UploadUserActive: 4, UploadInstanceTotalMB: 20480, UploadInstanceMaxFiles: 5000, UploadInstanceActive: 8, UploadStagingMB: 12288, UploadUserStagingMB: 9000, UploadRequestStagingMB: 8500}
	l, err := wholeUploadLimits(e)
	if err != nil {
		t.Fatal(err)
	}
	u, r, err := l.StagingLimits()
	if err != nil || u != 9000<<20 || r != 8500<<20 || l.StagingBytes != 12288<<20 {
		t.Fatal("runtime overrides not propagated", l, err)
	}
}
