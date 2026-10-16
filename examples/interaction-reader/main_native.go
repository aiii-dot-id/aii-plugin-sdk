//go:build !wasm_unknown

package main

import (
	"fmt"
	"os"
)

func main() {
	if err := readerPlugin().Serve("child-ready"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
