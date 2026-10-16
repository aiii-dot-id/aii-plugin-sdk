//go:build windows

package fileperm

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

func WritePrivate(path string, data []byte) error {
	user, err := processUser()
	if err != nil {
		return err
	}
	sd, err := securityDescriptor("D:P(A;;FA;;;" + user + ")")
	if err != nil {
		return err
	}
	defer syscall.LocalFree(syscall.Handle(sd))
	sa := syscall.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+"."+hex.EncodeToString(suffix[:])+".tmp")
	name, err := syscall.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, 0, &sa, syscall.CREATE_NEW, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return &os.PathError{Op: "create", Path: tmp, Err: err}
	}
	f := os.NewFile(uintptr(h), tmp)
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}
