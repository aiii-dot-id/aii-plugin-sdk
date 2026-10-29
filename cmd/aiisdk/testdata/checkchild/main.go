// checkchild is a native child for `aiisdk test`'s own tests: it reports
// itself ready, answers a source's embed with the two vectors the tests'
// checks expect (a little late when the text asks it to be), answers a
// search, and on "peek" asks the host for a stored value and says whether
// the host answered. A generic org.example plugin; nothing else.
//
// Its readiness follows the name it is run under, since a native child is
// given only the host's environment: a name with "-plain" prints a plain
// mark, "-nokit" an event=ready line with the profile's three fields and
// no sdk=, "-nofields" an event=ready line naming the kit and nothing else;
// any other name reports through ServeReady.
package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	var err error
	switch name := filepath.Base(os.Args[0]); {
	case strings.Contains(name, "-plain"):
		err = p.Serve("checkchild")
	case strings.Contains(name, "-nokit"):
		err = p.Serve("checkchild event=ready models_loaded=1 accelerator=cpu probe_ms=5")
	case strings.Contains(name, "-nofields"):
		err = p.Serve("checkchild event=ready sdk=v0.0.0")
	default:
		err = p.ServeReady("checkchild", aiiosdk.ReadyReport{ModelsLoaded: 1, Accelerator: "cpu", ProbeMS: 5})
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "checkchild: %v\n", err)
		os.Exit(1)
	}
}
