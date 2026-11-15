package environment

import (
	"testing"
)

func TestStagingShareEnvironment(t *testing.T) {
	for _, tc := range []struct {
		name, user, request string
		valid               bool
	}{
		{"auto", "0", "0", true}, {"large-file", "9000", "9000", true},
		{"owner-too-large", "12289", "0", false}, {"request-too-large", "100", "101", false},
		{"negative", "-1", "0", false}, {"overflow", "0", "9223372036854775807", false},
		{"invalid", "garbage", "0", false}, {"request-auto", "100", "0", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GOKAPI_UPLOAD_STAGING_MB", "12288")
			t.Setenv("GOKAPI_UPLOAD_USER_STAGING_MB", tc.user)
			t.Setenv("GOKAPI_UPLOAD_REQUEST_STAGING_MB", tc.request)
			e, err := parseWholeUploadEnvironment(Environment{})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
			if tc.name == "large-file" && (e.UploadUserStagingMB != 9000 || e.UploadRequestStagingMB != 9000) {
				t.Fatal("overrides lost", e)
			}
		})
	}
}
