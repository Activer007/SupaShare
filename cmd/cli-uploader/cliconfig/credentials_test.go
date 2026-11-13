package cliconfig

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func privateTestDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCredentialsAtomicReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission/hardlink regression")
	}
	dir := privateTestDir(t)
	path := filepath.Join(dir, "config.json")
	old := []byte("old-nonsecret-test-data")
	if err := os.WriteFile(path, old, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readCredentials(path); err == nil {
		t.Fatal("read accepted unsafe permissions")
	}
	alias := filepath.Join(dir, "old-hardlink")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"Apikey":"synthetic-regression-key"}`)
	if err := writeCredentials(path, want); err != nil {
		t.Fatal(err)
	}
	actual, err := readCredentials(path)
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatalf("roundtrip: %s, %v", actual, err)
	}
	unchanged, _ := os.ReadFile(alias)
	if !bytes.Equal(unchanged, old) {
		t.Fatal("old inode was overwritten")
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	entries, _ := filepath.Glob(filepath.Join(dir, ".supashare-credentials-*"))
	if len(entries) != 0 {
		t.Fatal("temporary file leaked")
	}
}

func TestCredentialsRejectLinksAndNonRegular(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlink privilege on Windows")
	}
	for _, kind := range []string{"symlink", "dangling", "directory", "parent-link", "writable-parent"} {
		t.Run(kind, func(t *testing.T) {
			dir := privateTestDir(t)
			path := filepath.Join(dir, "config.json")
			target := filepath.Join(dir, "target")
			if err := os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "symlink":
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "dangling":
				if err := os.Symlink(filepath.Join(dir, "missing"), path); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			case "parent-link":
				link := filepath.Join(dir, "link")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(link, "config.json")
			case "writable-parent":
				if err := os.Chmod(dir, 0777); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeCredentials(path, []byte("must-not-be-written")); err == nil {
				t.Fatal("unsafe write succeeded")
			}
			if _, err := readCredentials(path); err == nil {
				t.Fatal("unsafe read succeeded")
			}
			actual, _ := os.ReadFile(target)
			if string(actual) != "unchanged" {
				t.Fatal("target modified")
			}
		})
	}
}

func TestCredentialWriteFailurePreservesOldFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix modes")
	}
	dir := privateTestDir(t)
	path := filepath.Join(dir, "config.json")
	if err := writeCredentials(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := writeCredentials(path, []byte("new")); err == nil {
		t.Fatal("unsafe parent accepted")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != "old" {
		t.Fatal("existing file changed")
	}
}

func TestCredentialDefaultIgnoresWorkingDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG-specific test")
	}
	dir := privateTestDir(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "xdg"))
	oldArgs := os.Args
	os.Args = []string{"cli", "login"}
	t.Cleanup(func() { os.Args = oldArgs; configPathLocation = "" })
	t.Chdir(dir)
	if err := os.WriteFile("gokapi-cli.json", []byte("untrusted"), 0644); err != nil {
		t.Fatal(err)
	}
	configPathLocation = ""
	expected := filepath.Join(dir, "xdg", "supashare", "gokapi-cli.json")
	if getConfigPath() != expected {
		t.Fatalf("unexpected default: %s", getConfigPath())
	}
	if err := save("https://example.invalid", "synthetic-key", nil); err != nil {
		t.Fatal(err)
	}
	contents, _ := os.ReadFile("gokapi-cli.json")
	if string(contents) != "untrusted" {
		t.Fatal("wrote CWD file")
	}
	info, _ := os.Stat(filepath.Dir(expected))
	if info.Mode().Perm() != 0700 {
		t.Fatal("default directory not private")
	}
	os.Args = []string{"cli", "login", "-c", filepath.Join(dir, "explicit.json")}
	configPathLocation = ""
	if getConfigPath() != os.Args[3] {
		t.Fatal("explicit path ignored")
	}
	if err := save("https://example.invalid", "synthetic-key", nil); err != nil {
		t.Fatal(err)
	}
}
