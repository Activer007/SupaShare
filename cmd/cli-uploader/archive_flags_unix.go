//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import "syscall"

// Nonblocking prevents a concurrent regular-file-to-FIFO swap from hanging
// before f.Stat can reject it. No-follow rejects final-component link swaps.
const archiveOpenFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW
