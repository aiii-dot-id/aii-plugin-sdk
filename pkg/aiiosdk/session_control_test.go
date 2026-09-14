//go:build !wasm_unknown

package aiiosdk

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func controlLane(t *testing.T, admit SessionAdmit) (*os.File, *os.File, <-chan error) {
	t.Helper()
	in, send, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	read, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- New("test.control").serveSession(in, out, admit) }()
	t.Cleanup(func() { send.Close(); read.Close(); in.Close(); out.Close() })
	return send, read, done
}

func controlSend(t *testing.T, send *os.File, id int) {
	t.Helper()
	send.SetWriteDeadline(time.Now().Add(time.Second))
	frame := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"invoke.call","params":{"operation":"%d","arguments":{}}}`, id, id))
	if err := WriteFrame(send, frame, MaxControlFrameBytes); err != nil {
		t.Fatal(err)
	}
}

func controlRead(t *testing.T, read *os.File) map[string]json.RawMessage {
	t.Helper()
	read.SetReadDeadline(time.Now().Add(time.Second))
	raw, err := ReadFrame(read, MaxControlFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// .
// .
// .
// .
// .
func TestControlsAreCalledInOrderAndNeverWaitForAnEarlierAnswer(t *testing.T) {
	first := make(chan *Control, 1)
	order := make(chan string, 3)
	send, read, done := controlLane(t, func(c *Control) {
		order <- c.Op
		if c.Op == "1" {
			first <- c
			return
		}
		c.Answer(map[string]bool{"fenced": true}, nil)
	})
	controlSend(t, send, 1)
	controlSend(t, send, 2)
	controlSend(t, send, 3)
	for _, want := range []string{"2", "3"} {
		r := controlRead(t, read)
		if string(r["id"]) != want || !hasField(r["result"], "fenced") {
			t.Fatalf("a delayed answer blocked or miscorrelated a later control: %s", r)
		}
	}
	for _, want := range []string{"1", "2", "3"} {
		if got := <-order; got != want {
			t.Fatalf("handlers were called out of order: %s want %s", got, want)
		}
	}
	(<-first).Answer(nil, fmt.Errorf("engine refused exact cause"))
	r := controlRead(t, read)
	if string(r["id"]) != "1" || !strings.Contains(string(r["error"]), "engine refused exact cause") {
		t.Fatalf("the engine's own refusal was lost or fabricated: %s", r)
	}
	send.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("lane did not retire")
	}
}

// .
// .
// .
func TestAnUnansweredControlDoesNotHoldTheLaneOpen(t *testing.T) {
	taken := make(chan *Control, 1)
	send, _, done := controlLane(t, func(c *Control) { taken <- c })
	controlSend(t, send, 1)
	<-taken
	send.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("an unanswered control at EOF is the host's news, not the guest's failure: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("an unanswered control held the lane open")
	}
}

// .
// .
func TestAHandlerThatNeverReturnsEndsTheLaneWithItsReason(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	send, _, done := controlLane(t, func(c *Control) { <-release })
	controlSend(t, send, 1)
	send.Close()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "never returned") {
			t.Fatalf("a stuck handler must end the lane with its reason: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a stuck handler was waited on without a bound")
	}
}

// .
func TestAControlIsAnsweredExactlyOnce(t *testing.T) {
	send, read, done := controlLane(t, func(c *Control) {
		c.Answer(map[string]bool{"first": true}, nil)
		c.Answer(map[string]bool{"second": true}, nil)
		c.Answer(nil, fmt.Errorf("and a refusal for good measure"))
	})
	controlSend(t, send, 1)
	r := controlRead(t, read)
	if !hasField(r["result"], "first") {
		t.Fatalf("the first answer stands: %s", r)
	}
	send.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("lane did not retire")
	}
	if _, err := ReadFrame(read, MaxControlFrameBytes); err == nil {
		t.Fatal("a control answered twice sent two responses")
	}
}

// .
// .
func TestUnansweredControlsAreBounded(t *testing.T) {
	taken := make(chan struct{}, sessionAdmitQueue+1)
	send, _, done := controlLane(t, func(c *Control) { taken <- struct{}{} })
	for i := 1; i <= sessionAdmitQueue; i++ {
		controlSend(t, send, i)
		<-taken
	}
	controlSend(t, send, sessionAdmitQueue+1)
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "awaiting their answer") {
			t.Fatalf("unbounded admission: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("overflow did not end the lane")
	}
	if len(taken) != 0 {
		t.Fatal("the overflowing control reached the engine")
	}
}

// .
// .
func TestABlockedWriterDoesNotBlockTheNextControl(t *testing.T) {
	in, send, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w := &reviewBlockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	admitted := make(chan string, 2)
	done := make(chan error, 1)
	go func() {
		done <- New("test.control").serveSession(in, w, func(c *Control) {
			admitted <- c.Op
			c.Answer(true, nil)
		})
	}()
	t.Cleanup(func() { send.Close(); in.Close() })
	controlSend(t, send, 1)
	<-w.entered
	<-admitted
	controlSend(t, send, 2)
	select {
	case op := <-admitted:
		if op != "2" {
			t.Fatal(op)
		}
	case <-time.After(time.Second):
		close(w.release)
		t.Fatal("a blocked response write blocked the next control")
	}
	close(w.release)
	send.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writer did not retire")
	}
}
