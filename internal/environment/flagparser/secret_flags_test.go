//go:build test

package flagparser

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Activer007/SupaShare/internal/privatefile"
)

func TestN07ParserChild(t *testing.T) {
	if os.Getenv("N07_PARSER_CHILD") != "1" {
		return
	}
	DisableParsing = false
	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			os.Args = append([]string{args[0]}, args[i+1:]...)
			break
		}
	}
	var got MainFlags
	if os.Getenv("N07_ENV_PROJECTION") == "1" {
		got = ParseEnvironmentFlags()
	} else {
		got = ParseFlags()
	}
	if got.DeploymentPassword != os.Getenv("N07_WANT_PASSWORD") || got.Migration.Source != os.Getenv("N07_WANT_SOURCE") || got.Migration.Destination != os.Getenv("N07_WANT_DESTINATION") {
		t.Fatal("resolved values did not match expected synthetic inputs")
	}
	fmt.Println("N07_PARSED")
}

func TestN07ProtectedServerFlags(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secret := "N07-synthetic-secret-marker"
	file := filepath.Join(dir, "password")
	if err = privatefile.Write(file, []byte(secret+"\r\n")); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty")
	if err = privatefile.Write(empty, nil); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "source")
	destination := filepath.Join(dir, "destination")
	sourceURL := "redis://user:" + secret + "@localhost:6379"
	destURL := "sqlite://./synthetic-destination"
	if err = privatefile.Write(source, []byte(sourceURL)); err != nil {
		t.Fatal(err)
	}
	if err = privatefile.Write(destination, []byte(destURL)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name               string
		args               []string
		pass, projection   bool
		password, src, dst string
	}{
		{"password-file", []string{"--deployment-password-file", file}, true, false, secret, "", ""},
		{"legacy", []string{"--deployment-password", secret}, true, false, secret, "", ""},
		{"mixed", []string{"--deployment-password=", "--deployment-password-file", file}, false, false, "", "", ""},
		{"duplicate", []string{"--deployment-password-file", file, "--deployment-password-file", file}, false, false, "", "", ""},
		{"duplicate-prompt", []string{"--deployment-password-prompt", "--deployment-password-prompt"}, false, true, "", "", ""},
		{"prompt-false", []string{"--deployment-password-prompt=false"}, false, true, "", "", ""},
		{"migration-duplicate-prompt", []string{"migrate-database", "--source-prompt", "--source-prompt", "--destination-prompt"}, false, true, "", "", ""},
		{"empty", []string{"--deployment-password-file", empty}, false, false, "", "", ""},
		{"bad-file", []string{"--deployment-password-file", filepath.Join(dir, secret)}, false, false, "", "", ""},
		{"no-tty", []string{"--deployment-password-prompt"}, false, false, "", "", ""},
		{"invalid-value-redacted", []string{"--port=" + secret}, false, false, "", "", ""},
		{"unknown-redacted", []string{"--" + secret}, false, false, "", "", ""},
		{"environment-no-file-read", []string{"--deployment-password-file", filepath.Join(dir, secret)}, true, true, "", "", ""},
		{"environment-no-prompt", []string{"--deployment-password-prompt"}, true, true, "", "", ""},
		{"migration-files", []string{"migrate-database", "--source-file", source, "--destination-file", destination}, true, false, "", sourceURL, destURL},
		{"migration-mixed", []string{"migrate-database", "--source-prompt", "--source", secret, "--destination-file", destination}, false, false, "", "", ""},
		{"migration-same", []string{"migrate-database", "--source-file", source, "--destination-file", source}, false, false, "", "", ""},
		{"migration-projection", []string{"migrate-database", "--source-prompt", "--destination-prompt"}, true, true, "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append([]string{"-test.run=^TestN07ParserChild$", "--"}, tc.args...)
			cmd := exec.Command(os.Args[0], args...)
			cmd.Env = append(os.Environ(), "N07_PARSER_CHILD=1", "N07_WANT_PASSWORD="+tc.password, "N07_WANT_SOURCE="+tc.src, "N07_WANT_DESTINATION="+tc.dst)
			if tc.projection {
				cmd.Env = append(cmd.Env, "N07_ENV_PROJECTION=1")
			}
			output, err := cmd.CombinedOutput()
			if (err == nil) != tc.pass {
				t.Fatalf("pass=%t error=%v output=%s", tc.pass, err, output)
			}
			if strings.Contains(string(output), secret) {
				t.Fatal("secret leaked in diagnostics")
			}
			if tc.pass && !strings.Contains(string(output), "N07_PARSED") {
				t.Fatal("child did not parse")
			}
			if tc.name == "legacy" && !strings.Contains(string(output), "Warning:") {
				t.Fatal("missing legacy warning")
			}
		})
	}
}
