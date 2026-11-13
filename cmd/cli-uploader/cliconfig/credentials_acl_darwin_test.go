//go:build darwin

package cliconfig

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func addDarwinTestACL(t *testing.T, path, entry string) {
	t.Helper()
	if output, err := exec.Command("/bin/chmod", "+a", entry, path).CombinedOutput(); err != nil {
		t.Fatalf("setting synthetic ACL failed: %v, %s", err, output)
	}
	t.Cleanup(func() { exec.Command("/bin/chmod", "-RN", path).Run() })
}

func TestDarwinInheritedReadersRejectedBeforeTemporaryCreation(t *testing.T) {
	dir := privateTestDir(t)
	addDarwinTestACL(t, dir, "everyone allow read,file_inherit,only_inherit")
	if err := writeCredentials(filepath.Join(dir, "config.json"), []byte("synthetic-only")); err == nil {
		t.Fatal("credential parent with inherit-only reader was accepted")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("unsafe inheritance was detected only after creating a temporary file", err)
	}
}

func TestDarwinCredentialAncestorCannotGrantReplacement(t *testing.T) {
	dir := privateTestDir(t)
	parent := filepath.Join(dir, "private")
	if err := os.Mkdir(parent, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "config.json")
	old := []byte("old-synthetic-only")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	addDarwinTestACL(t, dir, "everyone allow delete_child")
	if err := writeCredentials(path, []byte("new-synthetic-only")); err == nil {
		t.Fatal("ancestor replacement grant was accepted")
	}
	if actual, err := os.ReadFile(path); err != nil || !bytes.Equal(actual, old) {
		t.Fatal("failed save changed the original file", err)
	}
}

func TestDarwinUnsafeOldFileReadRejectedAndReplacedPrivately(t *testing.T) {
	dir := privateTestDir(t)
	path := filepath.Join(dir, "config.json")
	old := []byte("old-synthetic-only")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "old-alias")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	addDarwinTestACL(t, alias, "everyone allow read")
	if _, err := readCredentials(path); err == nil {
		t.Fatal("0600 mode masked a readable extended ACL")
	}
	want := []byte("new-synthetic-only")
	if err := writeCredentials(path, want); err != nil {
		t.Fatal(err)
	}
	actual, err := readCredentials(path)
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatal("replacement did not create private readable credentials", err)
	}
	if actual, err := os.ReadFile(alias); err != nil || !bytes.Equal(actual, old) {
		t.Fatal("replacement overwrote the unsafe original inode", err)
	}
}

func TestDarwinTemporaryFileACLCheckedBeforeContentWrite(t *testing.T) {
	dir := privateTestDir(t)
	addDarwinTestACL(t, dir, "everyone allow read,file_inherit,only_inherit")
	f, err := os.OpenFile(filepath.Join(dir, "synthetic-empty.tmp"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := platformSecureFile(f); err == nil {
		t.Fatal("chmod accepted an inherited reader on the new temporary file")
	}
	info, err := f.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatal("temporary file contained data before its ACL was secured", err)
	}
}

func TestDarwinDenyOnlyACLRemainsSupported(t *testing.T) {
	dir := privateTestDir(t)
	addDarwinTestACL(t, dir, "everyone deny delete")
	path := filepath.Join(dir, "config.json")
	want := []byte("synthetic-only")
	if err := writeCredentials(path, want); err != nil {
		t.Fatal("safe deny-only directory rejected", err)
	}
	actual, err := readCredentials(path)
	if err != nil || !bytes.Equal(actual, want) {
		t.Fatal("safe deny-only credential read failed", err)
	}
}
