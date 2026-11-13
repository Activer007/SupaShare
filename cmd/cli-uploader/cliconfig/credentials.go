package cliconfig

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/Activer007/SupaShare/cmd/cli-uploader/cliflags"
	"github.com/Activer007/SupaShare/internal/environment"
)

// credentialRoot pins the parent directory for every subsequent operation.
// Never chmod an existing path: it may be a link or shared with other programs.
func credentialRoot(path string, create bool) (*os.Root, string, error) {
	if path == "" {
		return nil, "", fmt.Errorf("no user configuration directory; specify -c inside a private directory")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	parent := filepath.Dir(absolute)
	_, isDefault := cliflags.GetConfigLocation()
	if create && isDefault && !environment.IsDockerInstance() {
		// MkdirAll creates only missing components; existing components are checked below.
		if err := os.MkdirAll(parent, 0700); err != nil {
			return nil, "", err
		}
	}
	// Check ancestors as well as the immediate directory on Unix. An attacker
	// must not be able to replace the parent while it is opened. Sticky ancestors
	// (e.g. /tmp) are allowed, but never a writable credential directory itself.
	for dir := parent; ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
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

func writeCredentials(path string, data []byte) error {
	root, name, err := credentialRoot(path, true)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := checkCredential(root, name, false); err != nil {
		return err
	}
	temporary := fmt.Sprintf(".supashare-credentials-%x.tmp", randomBytes())
	f, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	defer f.Close()
	if err := platformSecureFile(f); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := checkCredential(root, name, false); err != nil {
		return err
	}
	// Rename replaces the directory entry, never follows a final symlink. On
	// failure the previous file is preserved; do not remove it as a fallback.
	return root.Rename(temporary, name)
}

func randomBytes() []byte {
	b := make([]byte, 16)
	// crypto/rand.Read is documented to fill b or terminate on entropy failure.
	_, _ = rand.Read(b)
	return b
}

func readCredentials(path string) ([]byte, error) {
	root, name, err := credentialRoot(path, false)
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
	return "credential permissions are unsafe: " + e.path + "; log in again to replace the file securely"
}
func isPermissionOnlyError(err error) bool { _, ok := err.(*permissionError); return ok }
