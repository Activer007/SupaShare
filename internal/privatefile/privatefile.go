package privatefile

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// credentialRoot pins the parent directory for every subsequent operation.
// Never chmod an existing path: it may be a link or shared with other programs.
func credentialRoot(path string) (*os.Root, string, error) {
	return credentialRootWithStat(path, os.Lstat)
}

// The stat dependency allows regression testing unavailable filesystem roots.
func credentialRootWithStat(path string, lstat func(string) (os.FileInfo, error)) (*os.Root, string, error) {
	if path == "" {
		return nil, "", fmt.Errorf("private file path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	parent := filepath.Dir(absolute)
	// Check ancestors as well as the immediate directory on Unix. An attacker
	// must not be able to replace the parent while it is opened. Sticky ancestors
	// (e.g. /tmp) are allowed, but never a writable credential directory itself.
	var missing error
	for dir := parent; ; dir = filepath.Dir(dir) {
		info, err := lstat(dir)
		if os.IsNotExist(err) {
			missing = err
			// Missing Windows volumes/UNC roots must terminate traversal too.
			if filepath.Dir(dir) == dir {
				return nil, "", err
			}
			continue
		}
		if err != nil {
			return nil, "", err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, "", fmt.Errorf("credential directory must not be a symlink: %s", dir)
		}
		check := platformCheck
		if dir != parent {
			check = platformCheckAncestor
		}
		if err := check(dir, info, true); err != nil {
			return nil, "", err
		}
		if dir == parent && runtime.GOOS != "windows" && info.Mode().Perm()&0022 != 0 {
			return nil, "", fmt.Errorf("credential directory is writable by other users: %s", dir)
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	if missing != nil {
		return nil, "", missing
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, "", err
	}
	return root, filepath.Base(absolute), nil
}

func checkCredential(root *os.Root, name string, reading bool) error {
	info, err := root.Lstat(name)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("credential file must be a regular file, not a symlink or device")
	}
	// On save, an owned but overly permissive old file can be replaced safely.
	// No secret bytes are ever written to that old inode (including hard links).
	if err := platformCheck(filepath.Join(root.Name(), name), info, false); err != nil {
		if reading || !isPermissionOnlyError(err) {
			return err
		}
	}
	return nil
}

// Write atomically replaces an owned regular file without modifying its old inode.
func Write(path string, data []byte) error {
	root, name, err := credentialRoot(path)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := checkCredential(root, name, false); err != nil {
		return err
	}
	temporary, err := createTemp(root, ".supashare-credentials-*.tmp", data)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	if err := checkCredential(root, name, false); err != nil {
		return err
	}
	// Rename replaces the directory entry, never follows a final symlink. On
	// failure the previous file is preserved; do not remove it as a fallback.
	if err := root.Rename(temporary, name); err != nil {
		return err
	}
	return syncDirectory(root)
}

func randomBytes() []byte {
	b := make([]byte, 16)
	// crypto/rand.Read is documented to fill b or terminate on entropy failure.
	_, _ = rand.Read(b)
	return b
}

// Read refuses unsafe files and directories before reading secret bytes.
func Read(path string) ([]byte, error) {
	root, name, err := credentialRoot(path)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err := checkCredential(root, name, true); err != nil {
		return nil, err
	}
	before, err := root.Lstat(name)
	if err != nil {
		return nil, err
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	after, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !os.SameFile(before, after) {
		return nil, fmt.Errorf("credential file changed while opening")
	}
	return io.ReadAll(f)
}

type permissionError struct{ path string }

func (e *permissionError) Error() string {
	return "credential permissions are unsafe: " + e.path + "; replace the file securely"
}
func isPermissionOnlyError(err error) bool { _, ok := err.(*permissionError); return ok }

// CreateTemp creates a new owner-private file, writes and syncs data, and returns
// its absolute path. The caller owns removal. dir must already be safe; no
// directories are created or permission-repaired. Pattern is a basename with
// at most one * placeholder; otherwise a random suffix is appended.
func CreateTemp(dir, pattern string, data []byte) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("private temporary directory is required")
	}
	root, _, err := credentialRoot(filepath.Join(dir, ".privatefile-parent-check"))
	if err != nil {
		return "", err
	}
	defer root.Close()
	name, err := createTemp(root, pattern, data)
	if err != nil {
		return "", err
	}
	if err := syncDirectory(root); err != nil {
		_ = root.Remove(name)
		return "", err
	}
	return filepath.Join(root.Name(), name), nil
}

func createTemp(root *os.Root, pattern string, data []byte) (string, error) {
	if strings.ContainsAny(pattern, "/\\") || strings.Count(pattern, "*") > 1 || strings.IndexByte(pattern, 0) >= 0 {
		return "", fmt.Errorf("temporary pattern must be a basename")
	}
	suffix := fmt.Sprintf("%x", randomBytes())
	name := pattern + suffix
	if strings.Contains(pattern, "*") {
		name = strings.Replace(pattern, "*", suffix, 1)
	}
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	success := false
	defer func() {
		_ = f.Close()
		if !success {
			_ = root.Remove(name)
		}
	}()
	if err := platformSecureFile(f); err != nil {
		return "", err
	}
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if err := platformCheck(filepath.Join(root.Name(), name), info, false); err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	if err := f.Sync(); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	success = true
	return name, nil
}

func syncDirectory(root *os.Root) error {
	// Windows does not offer portable directory fsync through os.File.Sync.
	// File contents are synced before rename; directory crash durability is OS-dependent.
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

// Exists validates existing ancestors and any final file before reporting absence.
// In particular a dangling symlink is an error, not a missing credential.
func Exists(path string) (bool, error) {
	root, name, err := credentialRoot(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer root.Close()
	if err := checkCredential(root, name, true); err != nil {
		return false, err
	}
	_, err = root.Lstat(name)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// Remove unlinks only a checked regular-file entry. Missing files are harmless;
// symlinks and unsafe parents are rejected. An owned old permissive file can be removed.
func Remove(path string) error {
	root, name, err := credentialRoot(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	if err := checkCredential(root, name, false); err != nil {
		return err
	}
	if err := root.Remove(name); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return syncDirectory(root)
}
