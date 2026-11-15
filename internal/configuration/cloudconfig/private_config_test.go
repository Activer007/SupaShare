package cloudconfig

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestN06CloudRejectsUnsafeWithoutFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode fixture")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cloudconfig.yml")
	if err = os.WriteFile(path, []byte("aws:\n  keysecret: synthetic\n"), 0644); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Error("unsafe cloud file silently fell back")
		}
	}()
	loadFromFile(path)
}

func TestN06CloudWriteDoesNotFollowLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlink privilege")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOKAPI_CONFIG_DIR", dir)
	target := filepath.Join(dir, "target")
	if err = os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "cloudconfig.yml")
	if err = os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err = Write(CloudConfig{}); err == nil {
		t.Fatal("followed cloud link")
	}
	got, _ := os.ReadFile(target)
	if string(got) != "unchanged" {
		t.Fatal("cloud link target changed")
	}
	if err = Delete(); err == nil {
		t.Fatal("unsafe link removal not rejected")
	}
}
