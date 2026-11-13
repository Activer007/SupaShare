//go:build !windows && !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package cliconfig

import (
	"fmt"
	"os"
)

func platformCheck(path string, info os.FileInfo, directory bool) error {
	return fmt.Errorf("secure credential storage is unsupported on this operating system")
}
func platformSecureFile(file *os.File) error {
	return fmt.Errorf("secure credential storage is unsupported on this operating system")
}

func platformCheckAncestor(path string, info os.FileInfo, directory bool) error {
	return platformCheck(path, info, directory)
}
