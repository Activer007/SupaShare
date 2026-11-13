//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package main

import (
	"archive/zip"
	"bytes"
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestArchiveRejectsSpecialFiles(t *testing.T) {
	for _, kind := range []string{"fifo", "socket"} {
		t.Run(kind, func(t *testing.T) {
			src := t.TempDir()
			if kind == "socket" {
				// Darwin socket paths are limited to 104 bytes. Avoid the full
				// test name in t.TempDir's path while retaining the OS temp root.
				shortDir, err := os.MkdirTemp("", "ss-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(shortDir) })
				src = shortDir
			}
			p := filepath.Join(src, "special")
			if kind == "fifo" {
				if err := syscall.Mkfifo(p, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				l, err := net.Listen("unix", p)
				if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
					t.Skipf("sandbox prohibits Unix socket creation: %v", err)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
			}
			if _, err := zipFolder(src, t.TempDir(), false); err == nil {
				t.Fatal("accepted special file")
			}
		})
	}
}
func TestArchiveRejectsFIFOSwapWithoutBlocking(t *testing.T) {
	src := t.TempDir()
	p := filepath.Join(src, "file")
	putArchiveFile(t, p, "allowed")
	before, _ := os.Lstat(p)
	root, err := os.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err = syscall.Mkfifo(p, 0600); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	defer w.Close()
	if err = archiveRegularFile(root, w, "file", "file", before, func(string) {}); err == nil {
		t.Fatal("accepted swapped FIFO")
	}
}
func TestArchiveRejectsBackslashName(t *testing.T) {
	src := t.TempDir()
	putArchiveFile(t, filepath.Join(src, `..\outside`), "data")
	if _, err := zipFolder(src, t.TempDir(), false); err == nil {
		t.Fatal("accepted nonportable name")
	}
}
