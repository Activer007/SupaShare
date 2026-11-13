//go:build linux || freebsd || openbsd || netbsd || dragonfly

package cliconfig

import "os"

func checkUnixACL(path string, ancestor bool) error { return nil }

func checkUnixFileACL(file *os.File) error { return nil }
