//go:build !windows

package fileperm

import (
	"fmt"
	"os"
)

func CheckPrivate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		return fmt.Errorf("mode %04o, want 0600", perm)
	}
	return nil
}

func Access(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	return info.Mode().Perm().String(), nil
}
