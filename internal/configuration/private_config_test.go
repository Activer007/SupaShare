package configuration

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Activer007/SupaShare/internal/models"
)

func TestN06MainConfigPrivateReadAndReplace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode fixture; Windows ACL tests cover native protection")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	old := []byte(`{"ServerUrl":"old","Authentication":{"SaltFiles":"12345678901234567890","SaltAdmin":"12345678901234567890"}}`)
	if err = os.WriteFile(path, old, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = loadFromFile(path); err == nil {
		t.Fatal("unsafe file was read")
	}
	// Owned permissive inode is replaced, not chmodded/truncated in place.
	hardlink := filepath.Join(dir, "old-inode")
	if err = os.Link(path, hardlink); err != nil {
		t.Fatal(err)
	}
	cfg := models.Configuration{ServerUrl: "new"}
	if err = saveToFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("replacement not private", err)
	}
	data, _ := os.ReadFile(hardlink)
	if string(data) != string(old) {
		t.Fatal("old inode mutated")
	}
	if _, err = loadFromFile(path); err != nil {
		t.Fatal(err)
	}
	// Links and writable parent directories must never receive secrets.
	target := filepath.Join(dir, "target")
	if err = os.WriteFile(target, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked-config")
	if err = os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err = saveToFile(link, cfg); err == nil {
		t.Fatal("followed final link")
	}
	if _, err = loadFromFile(link); err == nil {
		t.Fatal("read final link")
	}
	data, _ = os.ReadFile(target)
	if string(data) != "unchanged" {
		t.Fatal("link target changed")
	}
	if err = os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0700)
	if err = saveToFile(path, cfg); err == nil {
		t.Fatal("accepted writable parent")
	}
}

func TestN06ExistsRejectsDanglingLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires symlink privilege")
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOKAPI_CONFIG_DIR", dir)
	t.Setenv("GOKAPI_CONFIG_FILE", "config.json")
	if err = os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "config.json")); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if recover() == nil {
			t.Error("unsafe config treated as first setup")
		}
	}()
	Exists()
}
