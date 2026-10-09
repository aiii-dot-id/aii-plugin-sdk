//go:build windows

package fileperm

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32                       = syscall.NewLazyDLL("advapi32.dll")
	procStringToSecurityDescriptor = advapi32.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
	procSecurityDescriptorToString = advapi32.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW")
	procGetNamedSecurityInfo       = advapi32.NewProc("GetNamedSecurityInfoW")
)

const (
	sddlRevision1   = 1
	seFileObject    = 1
	ownerSecurity   = 0x1
	groupSecurity   = 0x2
	daclSecurity    = 0x4
	sddlNoDACL      = "NO_ACCESS_CONTROL"
	aceTypeAllowed  = "A"
	aceTypeDenied   = "D"
	aceFlagInherits = "ID"
)

func CheckPrivate(path string) error {
	protected, entries, err := DACL(path)
	if err != nil {
		return err
	}
	user, err := processUser()
	if err != nil {
		return err
	}
	switch {
	case !protected:
		return fmt.Errorf("its DACL is not protected, so its folder's grants reach it: %v", entries)
	case len(entries) != 1 || !entries[0].Allow || entries[0].SID != user:
		return fmt.Errorf("its DACL is %v, want one allow for this account (%s) alone", entries, user)
	}
	return nil
}

func Access(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	sddl, err := securityOf(path, ownerSecurity|groupSecurity|daclSecurity)
	if err != nil {
		return "", err
	}
	return info.Mode().Perm().String() + " " + sddl, nil
}

type Entry struct {
	Allow     bool
	SID       string
	Inherited bool
}

func (e Entry) String() string {
	s := "deny " + e.SID
	if e.Allow {
		s = "allow " + e.SID
	}
	if e.Inherited {
		s += " (inherited)"
	}
	return s
}

func DACL(path string) (protected bool, entries []Entry, err error) {
	sddl, err := securityOf(path, daclSecurity)
	if err != nil {
		return false, nil, err
	}
	rest, ok := strings.CutPrefix(sddl, "D:")
	if !ok || strings.HasPrefix(rest, sddlNoDACL) {
		return false, nil, fmt.Errorf("%s has no DACL, so every account may open it (%s)", path, sddl)
	}
	open := strings.IndexByte(rest, '(')
	if open < 0 {
		open = len(rest)
	}
	protected = strings.Contains(rest[:open], "P")
	for rest = rest[open:]; rest != ""; {
		end := strings.IndexByte(rest, ')')
		if rest[0] != '(' || end < 0 {
			return false, nil, fmt.Errorf("%s: the DACL %s is not read here", path, sddl)
		}
		f := strings.Split(rest[1:end], ";")
		rest = rest[end+1:]
		if len(f) != 6 || (f[0] != aceTypeAllowed && f[0] != aceTypeDenied) {
			return false, nil, fmt.Errorf("%s: the DACL entry (%s) is of a kind not read here", path, strings.Join(f, ";"))
		}
		sid, err := syscall.StringToSid(f[5])
		if err != nil {
			return false, nil, fmt.Errorf("%s: the DACL entry names %q: %w", path, f[5], err)
		}
		canonical, err := sid.String()
		if err != nil {
			return false, nil, err
		}
		entries = append(entries, Entry{Allow: f[0] == aceTypeAllowed, SID: canonical, Inherited: strings.Contains(f[1], aceFlagInherits)})
	}
	return protected, entries, nil
}

func MkdirSDDL(path, sddl string) error {
	sd, err := securityDescriptor(sddl)
	if err != nil {
		return err
	}
	defer syscall.LocalFree(syscall.Handle(sd))
	sa := syscall.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	if err := syscall.CreateDirectory(name, &sa); err != nil {
		return &os.PathError{Op: "mkdir", Path: path, Err: err}
	}
	return nil
}

func processUser() (string, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return "", fmt.Errorf("open the process token: %w", err)
	}
	defer token.Close()
	user, err := token.GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("read the process user: %w", err)
	}
	return user.User.Sid.String()
}

func securityDescriptor(sddl string) (uintptr, error) {
	s, err := syscall.UTF16PtrFromString(sddl)
	if err != nil {
		return 0, err
	}
	var sd uintptr
	if r, _, e := procStringToSecurityDescriptor.Call(uintptr(unsafe.Pointer(s)), sddlRevision1, uintptr(unsafe.Pointer(&sd)), 0); r == 0 {
		return 0, fmt.Errorf("security descriptor %s: %w", sddl, e)
	}
	return sd, nil
}

func securityOf(path string, info uint32) (string, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	var sd uintptr

	if r, _, _ := procGetNamedSecurityInfo.Call(uintptr(unsafe.Pointer(name)), seFileObject, uintptr(info), 0, 0, 0, 0, uintptr(unsafe.Pointer(&sd))); r != 0 {
		return "", &os.PathError{Op: "GetNamedSecurityInfo", Path: path, Err: syscall.Errno(r)}
	}
	defer syscall.LocalFree(syscall.Handle(sd))
	var str *uint16
	var n uint32
	if r, _, e := procSecurityDescriptorToString.Call(sd, sddlRevision1, uintptr(info), uintptr(unsafe.Pointer(&str)), uintptr(unsafe.Pointer(&n))); r == 0 {
		return "", fmt.Errorf("%s: the security descriptor as SDDL: %w", path, e)
	}
	defer syscall.LocalFree(syscall.Handle(unsafe.Pointer(str)))
	return syscall.UTF16ToString(unsafe.Slice(str, n)), nil
}
