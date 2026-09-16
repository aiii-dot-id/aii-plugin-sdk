//go:build !windows && !wasm_unknown

package aiiosdk

import (
	"os"
	"syscall"
)

// .
// .
// .
// .
// .
// .
func pollableStdin() *os.File {
	// .
	// .
	// .
	// .
	if fi, err := os.Stdin.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
		return os.Stdin
	}
	if err := syscall.SetNonblock(0, true); err != nil {
		return os.Stdin
	}
	return os.NewFile(0, "/dev/stdin")
}
