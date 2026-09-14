//go:build !wasm_unknown

package aiiosdk

// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .

import (
	"fmt"
	"os"
)

// .
// .
// .
// .
// .
// .
// .
// .
func MainDescribe() {
	if current == nil {
		fmt.Fprintln(os.Stderr, "aiiosdk: no plugin registered; call Run() from a package init function")
		os.Exit(1)
	}
	out, err := current.DescriptorsJSON()
	if err != nil {
		fmt.Fprintf(os.Stderr, "aiiosdk: %v\n", err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(out); err != nil {
		fmt.Fprintf(os.Stderr, "aiiosdk: %v\n", err)
		os.Exit(1)
	}
}
