//go:build !wasm_unknown

package aiiosdk

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"
)

type interruptFailWriter struct{ failed chan struct{} }

func (w *interruptFailWriter) Write([]byte) (int, error) {
	select {
	case <-w.failed:
	default:
		close(w.failed)
	}
	return 0, errors.New("injected response write failure")
}

// .
// .
// .
func TestFaultInterruptsAnIdlePipeRead(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var first bytes.Buffer
	if err := WriteFrame(&first, []byte(`{"jsonrpc":"2.0","id":1,"method":"invoke.call","params":{"operation":"speech.session.open","arguments":{}}}`), MaxControlFrameBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(first.Bytes()); err != nil {
		t.Fatal(err)
	}
	out := &interruptFailWriter{failed: make(chan struct{})}
	done := make(chan error, 1)
	start := time.Now()
	go func() {
		done <- New("interrupt.voice").serveSession(r, out, answerWith(func(*Session, string, Object) (any, error) { return map[string]bool{"accepted": true}, nil }))
	}()
	<-out.failed
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("the failed response must end the lane with an error")
		}
		if time.Since(start) > time.Second {
			t.Fatalf("the lane ended only after a wait (%v): the read was not interrupted", time.Since(start))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the lane never ended: the pending read was not interrupted")
	}
	// .
	if _, err := r.Read(make([]byte, 1)); err == nil {
		t.Fatal("the fault must have closed the input")
	}
}
