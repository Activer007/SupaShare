//go:build !(aix || darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package main

// os.Root confines Windows opens; post-open identity/type checks still apply.
const archiveOpenFlags = 0
