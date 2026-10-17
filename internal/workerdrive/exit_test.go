package workerdrive

import (
	"os"
	"strings"
	"testing"
	"time"
)

// A worker that exits before it is ready is reported at once, with its exit
// status and what it wrote, never after the whole start timeout (seen with a
// carrier that refused its runtime at startup: it exited 1 in milliseconds
// and the run waited five minutes for a ready line).
func TestAWorkerThatExitsBeforeReadyFailsAtOnce(t *testing.T) {
	if os.Getenv("AII_WORKERDRIVE_EXIT_CHILD") == "1" {
		os.Stderr.WriteString("the runtime does not verify: an unbound file\n")
		os.Exit(3)
	}
	// The child is this test binary: every other test of this package
	// skips in it, so it runs only this one, which exits at once.
	env := append(os.Environ(), "AII_WORKERDRIVE_EXIT_CHILD=1")
	for _, start := range []struct {
		name string
		run  func() error
	}{
		{"StartNative", func() error {
			w, err := StartNative(os.Args[0], env, 30*time.Second, nil)
			if w != nil {
				w.Close(time.Second)
			}
			return err
		}},
		{"StartNativeSession", func() error {
			w, err := StartNativeSession(os.Args[0], env, 30*time.Second, nil)
			if w != nil {
				w.Close(time.Second)
			}
			return err
		}},
	} {
		began := time.Now()
		err := start.run()
		took := time.Since(began)
		if err == nil {
			t.Fatalf("%s: a worker that exited was taken as ready", start.name)
		}
		if took > 10*time.Second {
			t.Fatalf("%s: waited %v for a worker that had already exited: %v", start.name, took, err)
		}
		for _, want := range []string{"exited before it was ready", "exit status 3", "an unbound file"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("%s: the refusal does not say %q: %v", start.name, want, err)
			}
		}
	}
}
