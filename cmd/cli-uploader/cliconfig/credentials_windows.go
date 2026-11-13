//go:build windows

package cliconfig

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

func currentSID() (*windows.SID, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return user.User.Sid, nil
}

func platformCheck(path string, info os.FileInfo, directory bool) error {
	return checkWindowsACL(path, directory, false)
}
func platformCheckAncestor(path string, info os.FileInfo, directory bool) error {
	return checkWindowsACL(path, true, true)
}
func checkWindowsACL(path string, directory, ancestor bool) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	sid, err := currentSID()
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	trustedOwner := owner != nil && owner.Equals(sid)
	if ancestor && owner != nil {
		trustedOwner = trustedOwner || owner.IsWellKnown(windows.WinLocalSystemSid) || owner.IsWellKnown(windows.WinBuiltinAdministratorsSid)
		if installer, _, _, err := windows.LookupSID("", "NT SERVICE\\TrustedInstaller"); err == nil {
			trustedOwner = trustedOwner || owner.Equals(installer)
		}
	}
	if !trustedOwner {
		return fmt.Errorf("credential path belongs to another Windows user: %s", path)
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	if acl == nil {
		return &permissionError{path}
	}
	// SYSTEM and Administrators are trusted OS principals; all other grants
	// that can expose credentials or alter this directory are rejected.
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			return err
		}
		if ancestor && ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if ace.Header.AceType == windows.ACCESS_DENIED_ACE_TYPE {
			continue
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			return &permissionError{path}
		}
		trustee := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if trustee.Equals(sid) || trustee.IsWellKnown(windows.WinLocalSystemSid) || trustee.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			continue
		}
		mask := uint32(ace.Mask)
		// File/directory write data, append/create, delete child, write EA/attrs,
		// delete, write DAC/owner and generic write/all. Reads additionally protect
		// file contents. The immediate parent must also have no other-reader
		// grants, including inherit-only ACEs: a newly created temp file must
		// never inherit an ACL that permits opening it before it is hardened.
		forbidden := uint32(0x40000000 | 0x10000000 | 0x000d0156)
		if ancestor {
			forbidden = 0x10000000 | 0x000d0040
		}
		if !ancestor {
			forbidden |= 0x80000000 | 0x00000001
		}
		if mask&forbidden != 0 {
			return &permissionError{path}
		}
	}
	return nil
}

func platformSecureFile(file *os.File) error {
	sid, err := currentSID()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;" + sid.String() + ")(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	// The Go file handle is opened for data writes, not WRITE_DAC. The random
	// exclusive-created name is inside a checked, non-shared directory.
	return windows.SetNamedSecurityInfo(file.Name(), windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
