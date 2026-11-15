package cliflags

import (
	"os"
	"testing"
)

func TestDownloadLimitFlagAndSourceScan(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"cli", "download", "--max-download-bytes", "1234", "--id", "test"}
	if got := GetUploadParameters(ModeDownload).MaxDownloadBytes; got != 1234 {
		t.Fatal(got)
	}
	os.Args = []string{"cli", "download", "--max-download-bytes", "-c", "--id", "test"}
	if _, def := GetConfigLocation(); !def {
		t.Fatal("limit value interpreted as config")
	}
	src, err := passwordSource([]string{"--max-download-bytes", "--password-prompt"})
	if err != nil || src.Prompt {
		t.Fatal("limit value interpreted as prompt")
	}
}
