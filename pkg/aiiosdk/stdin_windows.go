//go:build windows && !wasm_unknown

package aiiosdk

import "os"

// .
// .
func pollableStdin() *os.File { return os.Stdin }
