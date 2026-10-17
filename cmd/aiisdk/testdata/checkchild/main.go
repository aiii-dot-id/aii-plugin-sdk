// checkchild is a native child for `aiisdk test`'s own tests: it reports
// itself ready, answers a source's embed with the two vectors the tests'
// checks expect (a little late when the text asks it to be), answers a
// search, and on "peek" asks the host for a stored value and says whether
// the host answered. A generic org.example plugin; nothing else.
package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

func main() {
	p := aiiosdk.New("org.example.checked")
	p.Handle("embed", func(c aiiosdk.Call) (any, error) {
		kind, _ := c.Args().String("kind")
		text, _ := c.Args().String("text")
		if strings.Contains(text, "slowly") {
			time.Sleep(300 * time.Millisecond)
		}
		if kind == "cue" {
			return map[string]any{"vector": []float64{0.6, 0.8}}, nil
		}
		return map[string]any{"vector": []float64{1, 0}}, nil
	})
	p.Handle("search", func(c aiiosdk.Call) (any, error) {
		return map[string]any{"text": "the harbor at dusk", "hits": 3}, nil
	})
	p.Handle("peek", func(c aiiosdk.Call) (any, error) {
		_, _, err := aiiosdk.KV.Get("k")
		return map[string]any{"refused": err != nil}, nil
	})
	if err := p.ServeReady("checkchild", aiiosdk.ReadyReport{ModelsLoaded: 1, Accelerator: "cpu", ProbeMS: 5}); err != nil {
		fmt.Fprintf(os.Stderr, "checkchild: %v\n", err)
		os.Exit(1)
	}
}
