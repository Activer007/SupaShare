//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package cliconfig

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCredentialsRejectFIFO(t *testing.T) {
	path := filepath.Join(privateTestDir(t), "fifo")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeCredentials(path, []byte("synthetic")); err == nil {
		t.Fatal("FIFO accepted")
	}
	if _, err := readCredentials(path); err == nil {
		t.Fatal("FIFO accepted")
	}
}
func TestCredentialsRejectForeignOwner(t *testing.T) {
	info, err := os.Stat(privateTestDir(t))
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the owner policy without privileged chown or real credentials.
	stat := *info.Sys().(*syscall.Stat_t)
	stat.Uid = uint32(os.Geteuid()) + 1
	fake := foreignInfo{FileInfo: info, stat: &stat}
	if err := platformCheck("synthetic-owner", fake, false); err == nil {
		t.Fatal("foreign owner accepted")
	}
}

type foreignInfo struct {
	os.FileInfo
	stat *syscall.Stat_t
}

func (i foreignInfo) Sys() any { return i.stat }
