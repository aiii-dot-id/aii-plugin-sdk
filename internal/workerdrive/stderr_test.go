package workerdrive

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain lets this test binary stand in for a worker that is ready and
// goes on writing diagnostics: StartNative passes a child no arguments, so
// the child is chosen here, before any test of the package runs in it.
func TestMain(m *testing.M) {
	if os.Getenv("AII_WORKERDRIVE_LAST_WORDS_CHILD") == "1" {
		// Ready; at stdin's end, a burst of diagnostics, the last line,
		// and a failed exit: what a report on it must carry whole.
		fmt.Fprintln(os.Stderr, "event=ready")
		_, _ = io.Copy(io.Discard, os.Stdin)
		var burst strings.Builder
		for n := 0; n < lastWordsBurst; n++ {
			fmt.Fprintf(&burst, "farewell %d\n", n)
		}
		burst.WriteString("the last words\n")
		_, _ = os.Stderr.WriteString(burst.String()) // one write: the pipe still full of it at the exit
		os.Exit(3)
	}
	if os.Getenv("AII_WORKERDRIVE_STDERR_CHILD") == "1" {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			os.Exit(0) // stdin closed: the clean end Close waits for
		}()
		fmt.Fprintln(os.Stderr, "event=ready")
		for n := 0; ; n++ {
			fmt.Fprintf(os.Stderr, "diagnostic %d\n", n)
			time.Sleep(time.Millisecond)
		}
	}
	os.Exit(m.Run())
}

// A WORKER'S STDERR HAS ONE OWNER: the goroutine that scans a live worker's stderr appends to the buffer
// while Stderr and every failure report read it, so an unguarded buffer is
// a data race in the diagnostic path itself. Under the race detector this
// fails at the first read that overlaps a write; without it, it still holds
// that what the worker wrote after its ready line is what Stderr returns.
func TestStderrIsReadWhileTheWorkerStillWrites(t *testing.T) {
	if os.Getenv("AII_WORKERDRIVE_EXIT_CHILD") == "1" {
		t.Skip("the exit test's child runs only the exit test")
	}
	w, err := StartNative(os.Args[0], append(os.Environ(), "AII_WORKERDRIVE_STDERR_CHILD=1"), 30*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for !strings.Contains(w.Stderr(), "diagnostic 200\n") {
		if time.Now().After(deadline) {
			t.Fatalf("the worker's later lines never reached Stderr:\n%s", w.Stderr())
		}
		time.Sleep(time.Millisecond)
	}
	if err := w.Close(10 * time.Second); err != nil {
		t.Fatal(err)
	}
}

// lastWordsBurst is how many lines the last-words child writes, in one
// write, after its stdin ends: well past a pipe's buffer, so the pipe
// still holds the end of it when the child exits.
const lastWordsBurst = 20000

// A REPORT ON A CHILD THAT HAS ENDED CARRIES ITS LAST LINES. Close waited
// for the process and read the buffer at once, while the goroutine that
// scans stderr could still be behind, and Wait closes the pipe under it:
// the report could end before the child's final lines. The scan is now
// joined first, in both drivers.
func TestAWorkersLastStderrLinesAreInItsReport(t *testing.T) {
	if os.Getenv("AII_WORKERDRIVE_EXIT_CHILD") == "1" {
		t.Skip("the exit test's child runs only the exit test")
	}
	env := append(os.Environ(), "AII_WORKERDRIVE_LAST_WORDS_CHILD=1")
	for run := 0; run < 3; run++ {
		w, err := StartNative(os.Args[0], env, 30*time.Second, nil)
		if err != nil {
			t.Fatal(err)
		}
		checkLastWords(t, "the ordinary worker", w.Close(30*time.Second))
		sw, err := StartNativeSession(os.Args[0], env, 30*time.Second, nil)
		if err != nil {
			t.Fatal(err)
		}
		checkLastWords(t, "the session carrier", sw.Close(30*time.Second))
	}
}

func checkLastWords(t *testing.T, who string, err error) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), "exited 3") {
		t.Fatalf("%s: Close answered %v, want its exit 3", who, err)
	}
	msg := err.Error()
	if !strings.Contains(msg, fmt.Sprintf("farewell %d\n", lastWordsBurst-1)) || !strings.HasSuffix(msg, "the last words\n") {
		tail := msg
		if len(tail) > 200 {
			tail = tail[len(tail)-200:]
		}
		t.Fatalf("%s: the report lacks the child's last lines; it ends:\n%s", who, tail)
	}
}
