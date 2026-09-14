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
	if err := syscall.SetNonblock(0, true); err != nil {
		return os.Stdin
	}
	return os.NewFile(0, "/dev/stdin")
}
