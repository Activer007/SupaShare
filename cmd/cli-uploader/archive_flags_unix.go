//go:build aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package main

import "syscall"

// Nonblocking prevents a concurrent regular-file-to-FIFO swap from hanging
// before f.Stat can reject it. O_NOFOLLOW requests link rejection; os.Root may
// resolve confined links, so post-open identity checks remain required.
const archiveOpenFlags = syscall.O_NONBLOCK | syscall.O_NOFOLLOW
