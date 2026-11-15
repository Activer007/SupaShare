package cliflags

import (
	"os"
	"testing"
)

func TestPasswordSources(t *testing.T) {
	for _, args := range [][]string{{"-p", "", "--password-file", "missing"}, {"--password-prompt", "--password-prompt"}, {"--password=x", "-p", "y"}, {"--password-file"}, {"--password-prompt=false"}} {
		if _, err := passwordSource(args); err == nil {
			t.Fatal("conflicting/missing source accepted")
		}
	}
	for _, args := range [][]string{{"-p", "synthetic"}, {"--password", "synthetic"}, {"--password=synthetic"}, {"-p=synthetic"}} {
		source, err := passwordSource(args)
		if err != nil || !source.InlineSet || source.Inline != "synthetic" {
			t.Fatal("legacy source mismatch")
		}
	}
	source, err := passwordSource([]string{"--password-file=private-file"})
	if err != nil || !source.FileSet || source.File != "private-file" {
		t.Fatal("file source mismatch")
	}
	// A consumed value must not become another flag or appear in parsing errors.
	source, err = passwordSource([]string{"--password", "--password-prompt"})
	if err != nil || source.Prompt || source.Inline != "--password-prompt" {
		t.Fatal("password value reinterpreted")
	}
}

func TestPasswordModesAndConsumedValues(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()
	for _, tc := range []struct {
		mode int
		args []string
	}{
		{ModeUpload, []string{"upload", "-f", "fixture"}},
		{ModeArchive, []string{"upload-dir", "-D", "fixture"}},
		{ModeDownload, []string{"download", "-i", "fixture"}},
	} {
		os.Args = append(append([]string{"cli"}, tc.args...), "--password", "--expiry-days")
		result := GetUploadParameters(tc.mode)
		if result.Password != "--expiry-days" || result.ExpiryDays != 0 {
			t.Fatal("mode password or value consumption changed")
		}
	}
}
