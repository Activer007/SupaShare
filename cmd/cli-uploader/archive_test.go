package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func putArchiveFile(t *testing.T, name, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func archiveContents(t *testing.T, name string) map[string]string {
	t.Helper()
	r, err := zip.OpenReader(name)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	result := map[string]string{}
	for _, f := range r.File {
		rd, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rd)
		rd.Close()
		if err != nil {
			t.Fatal(err)
		}
		result[f.Name] = string(b)
	}
	return result
}
func TestZipFolderNormalAndInternalTemp(t *testing.T) {
	for _, tmpLocation := range []string{"outside", "root", "nested"} {
		t.Run(tmpLocation, func(t *testing.T) {
			src := t.TempDir()
			tmp := t.TempDir()
			if tmpLocation == "root" {
				tmp = src
			}
			if tmpLocation == "nested" {
				tmp = filepath.Join(src, "temp")
				os.Mkdir(tmp, 0700)
			}
			putArchiveFile(t, filepath.Join(src, "nested", "中文.txt"), "hello")
			putArchiveFile(t, filepath.Join(src, "gokapi-cli-user", "keep.txt"), "keep")
			if err := os.Mkdir(filepath.Join(src, "empty"), 0700); err != nil {
				t.Fatal(err)
			}
			p, err := zipFolder(src, tmp, false)
			if err != nil {
				t.Fatal(err)
			}
			defer deleteTempFolder(p)
			got := archiveContents(t, p)
			if got["nested/中文.txt"] != "hello" || got["gokapi-cli-user/keep.txt"] != "keep" {
				t.Fatalf("missing content: %v", got)
			}
			if _, ok := got["empty/"]; !ok {
				t.Fatal("empty directory lost")
			}
			want := 5
			if tmpLocation == "nested" {
				want++
			}
			if len(got) != want {
				t.Fatalf("unexpected entries: %v", got)
			}
		})
	}
}
func TestZipFolderRejectsLinksAndCleansPartialOutput(t *testing.T) {
	for _, kind := range []string{"outside", "relative-outside", "inside", "dangling", "directory", "root", "root-trailing-separator"} {
		t.Run(kind, func(t *testing.T) {
			src := t.TempDir()
			tmp := t.TempDir()
			outside := t.TempDir()
			putArchiveFile(t, filepath.Join(src, "aaa.txt"), "allowed")
			putArchiveFile(t, filepath.Join(outside, "secret"), "SYNTHETIC OUTSIDE MARKER")
			target := filepath.Join(outside, "secret")
			link := filepath.Join(src, "zzz")
			switch kind {
			case "relative-outside":
				relative, err := filepath.Rel(src, target)
				target = relative
				if err != nil {
					t.Fatal(err)
				}
			case "inside":
				target = "aaa.txt"
			case "dangling":
				target = "missing"
			case "directory":
				target = outside
			case "root", "root-trailing-separator":
				target = src
				link = filepath.Join(t.TempDir(), "source")
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if kind == "root" {
				src = link
			}
			if kind == "root-trailing-separator" {
				src = link + string(os.PathSeparator)
			}
			p, err := zipFolder(src, tmp, false)
			if err == nil || p != "" {
				t.Fatalf("unsafe archive returned %q, %v", p, err)
			}
			entries, err := os.ReadDir(tmp)
			if err != nil || len(entries) != 0 {
				t.Fatalf("partial archive left behind: %v %v", entries, err)
			}
		})
	}
}
func TestArchiveRegularFileRejectsReplacedEntry(t *testing.T) {
	for _, kind := range []string{"file", "link", "directory"} {
		t.Run(kind, func(t *testing.T) {
			src := t.TempDir()
			name := filepath.Join(src, "file")
			putArchiveFile(t, name, "allowed")
			before, err := os.Lstat(name)
			if err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(src)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err = os.Rename(name, filepath.Join(src, "original")); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file":
				putArchiveFile(t, name, "replaced")
			case "directory":
				os.Mkdir(name, 0700)
			case "link":
				outside := filepath.Join(t.TempDir(), "secret")
				putArchiveFile(t, outside, "SYNTHETIC SECRET")
				if err = os.Symlink(outside, name); err != nil {
					t.Skip(err)
				}
			}
			var b bytes.Buffer
			w := zip.NewWriter(&b)
			if err = archiveRegularFile(root, w, "file", "file", before, func(string) {}); err == nil {
				t.Fatal("replacement accepted")
			}
			w.Close()
			r, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
			if err != nil {
				t.Fatal(err)
			}
			if len(r.File) != 0 {
				t.Fatal("created entry before validating handle")
			}
		})
	}
}
func TestArchiveDirectoryPinsRenamedAncestor(t *testing.T) {
	parent := t.TempDir()
	src := filepath.Join(parent, "src")
	putArchiveFile(t, filepath.Join(src, "child", "allowed"), "allowed")
	root, err := os.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err = os.Rename(src, filepath.Join(parent, "moved")); err != nil {
		t.Skipf("platform cannot rename open dir: %v", err)
	}
	outside := t.TempDir()
	putArchiveFile(t, filepath.Join(outside, "secret"), "outside")
	if err = os.Symlink(outside, src); err != nil {
		t.Skip(err)
	}
	excluded, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	if err = archiveDirectory(root, w, "", excluded, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(b.Bytes()), int64(b.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.File) != 2 || r.File[1].Name != "child/allowed" {
		t.Fatalf("followed replaced ancestor: %v", r.File)
	}
}
func TestZipFolderRejectsRegularSource(t *testing.T) {
	p := filepath.Join(t.TempDir(), "file")
	putArchiveFile(t, p, "x")
	if _, err := zipFolder(p, t.TempDir(), false); err == nil {
		t.Fatal("accepted file as directory")
	}
}

func TestArchiveRejectsChildDirectoryReplacement(t *testing.T) {
	for _, kind := range []string{"directory", "link"} {
		t.Run(kind, func(t *testing.T) {
			src := t.TempDir()
			childPath := filepath.Join(src, "child")
			putArchiveFile(t, filepath.Join(childPath, "allowed"), "allowed")
			before, err := os.Lstat(childPath)
			if err != nil {
				t.Fatal(err)
			}
			root, err := os.OpenRoot(src)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err = os.Rename(childPath, filepath.Join(src, "original")); err != nil {
				t.Fatal(err)
			}
			if kind == "directory" {
				if err = os.Mkdir(childPath, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				outside := t.TempDir()
				putArchiveFile(t, filepath.Join(outside, "secret"), "outside")
				if err = os.Symlink(outside, childPath); err != nil {
					t.Skip(err)
				}
			}
			child, err := openArchiveDirectory(root, "child", before)
			if child != nil {
				child.Close()
			}
			if err == nil {
				t.Fatal("accepted replaced child directory")
			}
		})
	}
}

type failingArchiveWriter struct{}

func (failingArchiveWriter) Write(p []byte) (int, error) { return 0, io.ErrClosedPipe }
func TestArchivePropagatesFinalizationFailure(t *testing.T) {
	// Empty output makes the first underlying write occur during ZIP Close.
	src := t.TempDir()
	root, err := os.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	excluded, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = writeDirectoryArchive(root, failingArchiveWriter{}, excluded, func(string) {}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("close error lost: %v", err)
	}
}
func TestArchivePropagatesWriteFailure(t *testing.T) {
	src := t.TempDir()
	data := make([]byte, 256*1024)
	if _, err := rand.Read(data); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "large"), data, 0600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(src)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	excluded, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err = writeDirectoryArchive(root, failingArchiveWriter{}, excluded, func(string) {}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write error lost: %v", err)
	}
}
