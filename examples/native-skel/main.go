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
package main

import (
	"fmt"
	"os"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

func main() {
	p := aiiosdk.New("org.example.native-skel")

	// .
	// .
	// .
	p.Handle("describe", func(c aiiosdk.Call) (any, error) {
		return map[string]any{
			"plugin":   "native-skel",
			"runtime":  "native_t3_component",
			"platform": fmt.Sprintf("%s/%s", os.Getenv("GOOS"), os.Getenv("GOARCH")),
		}, nil
	})

	// .
	// .
	// .
	// .
	p.Handle("remember", func(c aiiosdk.Call) (any, error) {
		key, _ := c.Args().String("key")
		value, _ := c.Args().String("value")
		if key == "" {
			return nil, fmt.Errorf("remember needs a key")
		}
		if _, err := aiiosdk.KV.Put(key, value); err != nil {
			return nil, err
		}
		return map[string]any{"stored": key}, nil
	})

	// .
	// .
	// .
	if err := p.Serve("child-ready"); err != nil {
		fmt.Fprintf(os.Stderr, "native-skel: %v\n", err)
		os.Exit(1)
	}
}
