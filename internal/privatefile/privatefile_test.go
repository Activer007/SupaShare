package privatefile

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
	if _, err := Read(path); err == nil {
		t.Fatal("read accepted unsafe permissions")
	}
	alias := filepath.Join(dir, "old-hardlink")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	want := []byte(`{"Apikey":"synthetic-regression-key"}`)
	if err := Write(path, want); err != nil {
		t.Fatal(err)
	}
	actual, err := Read(path)
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
			if err := Write(path, []byte("must-not-be-written")); err == nil {
				t.Fatal("unsafe write succeeded")
			}
			if _, err := Read(path); err == nil {
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
	if err := Write(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("new")); err == nil {
		t.Fatal("unsafe parent accepted")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != "old" {
		t.Fatal("existing file changed")
	}
}

func TestTemporaryAndLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses native ACL fixtures")
	}
	dir := privateTestDir(t)
	path, err := CreateTemp(dir, "secret-*.json", []byte("synthetic"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Read(path)
	if err != nil || string(data) != "synthetic" {
		t.Fatal("temporary roundtrip", err)
	}
	if exists, err := Exists(path); err != nil || !exists {
		t.Fatal("exists", exists, err)
	}
	if err := Remove(path); err != nil {
		t.Fatal(err)
	}
	if exists, err := Exists(path); err != nil || exists {
		t.Fatal("removed", exists, err)
	}
	if err := Remove(path); err != nil {
		t.Fatal("idempotent remove", err)
	}
	for _, pattern := range []string{"../escape", "sub/file", "sub\\file", "a**b"} {
		if _, err := CreateTemp(dir, pattern, []byte("synthetic")); err == nil {
			t.Fatal("unsafe pattern", pattern)
		}
	}
	missing := filepath.Join(dir, "missing", "secret")
	if exists, err := Exists(missing); err != nil || exists {
		t.Fatal("safe missing parent", exists, err)
	}
	if _, err := os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatal("created missing directory")
	}
	link := filepath.Join(dir, "dangling")
	if err := os.Symlink(missing, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Exists(link); err == nil {
		t.Fatal("dangling link became absence")
	}
	if err := Remove(link); err == nil {
		t.Fatal("removed symlink")
	}
	if _, err := Exists(filepath.Join(link, "file")); err == nil {
		t.Fatal("unsafe ancestor became absence")
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := Exists(missing); err == nil {
		t.Fatal("unsafe ancestor became absence")
	}
	if _, err := CreateTemp(dir, "test", []byte("synthetic")); err == nil {
		t.Fatal("unsafe temporary parent")
	}
}

func TestUnavailableRootTerminates(t *testing.T) {
	calls := 0
	root, name, err := credentialRootWithStat(filepath.Join(string(filepath.Separator), "unavailable", "secret"), func(path string) (os.FileInfo, error) {
		calls++
		if calls > 10 {
			t.Fatal("ancestor traversal failed to terminate at missing root")
		}
		return nil, &os.PathError{Op: "lstat", Path: path, Err: os.ErrNotExist}
	})
	if root != nil || name != "" || !os.IsNotExist(err) || calls == 0 {
		t.Fatalf("unexpected missing-root result: %v %q %v (%d calls)", root, name, err, calls)
	}
}
