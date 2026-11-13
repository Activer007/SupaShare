package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/schollz/progressbar/v3"
)

// zipFolder returns a complete archive only on success. Links and special files
// fail the entire operation, rather than silently omitting user-selected data.
func zipFolder(srcDir, tmpFolder string, showOutput bool) (result string, err error) {
	if runtime.GOOS == "js" || runtime.GOOS == "plan9" {
		return "", fmt.Errorf("secure directory archiving is not supported on %s", runtime.GOOS)
	}
	srcDir, err = filepath.Abs(srcDir)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(srcDir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("archive source must be a directory, not a link or file: %s", srcDir)
	}
	root, err := os.OpenRoot(srcDir)
	if err != nil {
		return "", err
	}
	defer root.Close()
	actual, err := root.Stat(".")
	if err != nil {
		return "", err
	}
	if !os.SameFile(info, actual) {
		return "", fmt.Errorf("archive source changed while opening")
	}
	folder, err := os.MkdirTemp(tmpFolder, "gokapi-cli-")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(folder)
		}
	}()
	excluded, err := os.Stat(folder)
	if err != nil {
		return "", err
	}
	zipPath := filepath.Join(folder, filepath.Base(srcDir)+".zip")
	file, err := os.OpenFile(zipPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var progress *progressbar.ProgressBar
	if showOutput {
		progress = progressbar.NewOptions(-1, progressbar.OptionSetDescription("Compressing files..."), progressbar.OptionClearOnFinish())
		defer progress.Finish()
	}
	err = writeDirectoryArchive(root, file, excluded, func(name string) {
		if progress != nil {
			progress.Describe("Compressing: " + path.Base(name))
		}
	})
	if err != nil {
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	return zipPath, nil
}

// Each recursive root pins the inspected directory, even if an ancestor is
// renamed/replaced. Never reopen source entries through absolute paths.
func archiveDirectory(root *os.Root, writer *zip.Writer, prefix string, excluded os.FileInfo, progress func(string)) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	closeErr := dir.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		name := entry.Name()
		// ZIP names are portable slash-separated paths; reject Unix names which a
		// Windows extractor could reinterpret as a path separator.
		if !filepath.IsLocal(name) || name == "." || name == ".." || path.Base(name) != name || strings.Contains(name, `\`) {
			return fmt.Errorf("unsafe archive entry name: %q", name)
		}
		info, err := root.Lstat(name)
		if err != nil {
			return err
		}
		archiveName := path.Join(prefix, name)
		if info.IsDir() {
			if os.SameFile(info, excluded) {
				continue
			} // never archive our own temporary ZIP
			child, err := openArchiveDirectory(root, name, info)
			if err != nil {
				return err
			}
			if err == nil {
				_, err = writer.Create(archiveName + "/")
			}
			if err == nil {
				err = archiveDirectory(child, writer, archiveName, excluded, progress)
			}
			closeErr := child.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		} else {
			if !info.Mode().IsRegular() {
				return fmt.Errorf("cannot archive link or special file: %s", archiveName)
			}
			if err := archiveRegularFile(root, writer, name, archiveName, info, progress); err != nil {
				return err
			}
		}
	}
	return nil
}

func archiveRegularFile(root *os.Root, writer *zip.Writer, name, archiveName string, expected os.FileInfo, progress func(string)) error {
	file, err := root.OpenFile(name, os.O_RDONLY|archiveOpenFlags, 0)
	if err != nil {
		return err
	}
	defer file.Close()
	actual, err := file.Stat()
	if err != nil {
		return err
	}
	if !actual.Mode().IsRegular() || !os.SameFile(expected, actual) {
		return fmt.Errorf("file changed while opening: %s", archiveName)
	}
	dest, err := writer.Create(archiveName)
	if err != nil {
		return err
	}
	progress(archiveName)
	_, err = io.Copy(dest, file)
	return err
}

// Always finalize the ZIP, but never suppress a traversal or finalization error.
func writeDirectoryArchive(root *os.Root, output io.Writer, excluded os.FileInfo, progress func(string)) error {
	writer := zip.NewWriter(output)
	err := archiveDirectory(root, writer, "", excluded, progress)
	closeErr := writer.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func openArchiveDirectory(root *os.Root, name string, expected os.FileInfo) (*os.Root, error) {
	child, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	actual, err := child.Stat(".")
	if err == nil && !os.SameFile(expected, actual) {
		err = fmt.Errorf("directory changed while opening: %s", name)
	}
	if err != nil {
		child.Close()
		return nil, err
	}
	return child, nil
}
