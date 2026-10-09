//go:build !windows

package fileperm

import "os"

func WritePrivate(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
