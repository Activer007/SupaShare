//go:build darwin

package cliconfig

import (
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

// Darwin mode bits do not restrict extended ACL grants. Request only the
// extended-security attribute, whose documented maximum is 128 24-byte ACEs.
const darwinACLBufferSize = 4096

type darwinAttrlist struct {
	bitmapCount uint16
	reserved    uint16
	common      uint32
	volume      uint32
	directory   uint32
	file        uint32
	fork        uint32
}

func checkUnixACL(path string, ancestor bool) error {
	name, err := syscall.BytePtrFromString(path)
	if err != nil {
		return err
	}
	attributes := darwinAttrlist{bitmapCount: 5, common: 0x00400000} // ATTR_CMN_EXTENDED_SECURITY
	buffer := make([]byte, darwinACLBufferSize)
	_, _, errno := syscall.Syscall6(syscall.SYS_GETATTRLIST,
		uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 1, 0) // FSOPT_NOFOLLOW
	runtime.KeepAlive(name)
	if errno != 0 {
		return fmt.Errorf("cannot inspect credential ACL for %s: %w", path, errno)
	}
	return checkDarwinACL(path, buffer, ancestor)
}

func checkUnixFileACL(file *os.File) error {
	attributes := darwinAttrlist{bitmapCount: 5, common: 0x00400000}
	buffer := make([]byte, darwinACLBufferSize)
	_, _, errno := syscall.Syscall6(syscall.SYS_FGETATTRLIST,
		file.Fd(), uintptr(unsafe.Pointer(&attributes)),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0, 0)
	runtime.KeepAlive(file)
	if errno != 0 {
		return fmt.Errorf("cannot inspect credential file ACL: %w", errno)
	}
	return checkDarwinACL(file.Name(), buffer, false)
}

func checkDarwinACL(path string, buffer []byte, ancestor bool) error {
	// getattrlist returns a length and an attrreference. Its relative offset
	// starts at the reference itself, followed by a variable kauth_filesec.
	if len(buffer) < 12 {
		return fmt.Errorf("invalid credential ACL metadata: %s", path)
	}
	length := int(binary.LittleEndian.Uint32(buffer[:4]))
	offset := int(int32(binary.LittleEndian.Uint32(buffer[4:8])))
	size := int(binary.LittleEndian.Uint32(buffer[8:12]))
	start := 4 + offset
	if length < 12 || length > len(buffer) || start < 12 || start > length || size > length-start {
		return fmt.Errorf("invalid credential ACL metadata: %s", path)
	}
	if size == 0 {
		return nil
	}
	security := buffer[start : start+size]
	if len(security) < 44 || binary.LittleEndian.Uint32(security[:4]) != 0x012cc16d {
		return fmt.Errorf("invalid credential ACL metadata: %s", path)
	}
	count := binary.LittleEndian.Uint32(security[36:40])
	if count == 0xffffffff { // KAUTH_FILESEC_NOACL
		if len(security) != 44 {
			return fmt.Errorf("invalid credential ACL metadata: %s", path)
		}
		return nil
	}
	if count > 128 || len(security) != 44+int(count)*24 {
		return fmt.Errorf("invalid credential ACL metadata: %s", path)
	}
	// Conservatively reject dangerous allow ACEs without resolving GUID group
	// membership. Deny-only ACLs, including the default home deny-delete ACE,
	// do not widen credential access and remain supported.
	const (
		allow             = 1
		deny              = 2
		inheritOnly       = 1 << 8
		readData          = 1 << 1
		writeData         = 1 << 2
		deleteObject      = 1 << 4
		appendData        = 1 << 5
		deleteChild       = 1 << 6
		writeAttributes   = 1 << 8
		readExtAttributes = 1 << 9
		writeExtAttrs     = 1 << 10
		writeSecurity     = 1 << 12
		changeOwner       = 1 << 13
		genericAll        = 1 << 21
		genericWrite      = 1 << 23
		genericRead       = 1 << 24
	)
	for n := 0; n < int(count); n++ {
		entry := security[44+n*24 : 44+(n+1)*24]
		flags := binary.LittleEndian.Uint32(entry[16:20])
		rights := binary.LittleEndian.Uint32(entry[20:24])
		if flags&0xf == deny || ancestor && flags&inheritOnly != 0 {
			continue
		}
		if flags&0xf != allow {
			return &permissionError{path}
		}
		forbidden := uint32(deleteObject | deleteChild | writeSecurity | changeOwner | genericAll | genericWrite)
		if !ancestor {
			forbidden |= readData | writeData | appendData | writeAttributes | readExtAttributes | writeExtAttrs | genericRead
		}
		if rights&forbidden != 0 {
			return &permissionError{path}
		}
	}
	return nil
}
