//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package cliconfig

import (
	"fmt"
	"os"
	"syscall"
)

func platformCheck(path string, info os.FileInfo, directory bool) error {
	if err := checkUnixOwnerAndMode(path, info, directory); err != nil {
		return err
	}
	return checkUnixACL(path, false)
}

func checkUnixOwnerAndMode(path string, info os.FileInfo, directory bool) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot verify credential owner: %s", path)
	}
	uid := uint32(os.Geteuid())
	if stat.Uid != uid && !(directory && stat.Uid == 0) {
		return fmt.Errorf("credential path belongs to another user: %s", path)
	}
	if directory {
		if info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return fmt.Errorf("credential directory is writable by other users: %s", path)
		}
	} else if info.Mode().Perm()&0077 != 0 {
		return &permissionError{path}
	}
	return nil
}
func platformSecureFile(file *os.File) error {
	if err := file.Chmod(0600); err != nil {
		return err
	}
	return checkUnixFileACL(file)
}

func platformCheckAncestor(path string, info os.FileInfo, directory bool) error {
	if err := checkUnixOwnerAndMode(path, info, directory); err != nil {
		return err
	}
	return checkUnixACL(path, true)
}
