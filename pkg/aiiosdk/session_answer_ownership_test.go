//go:build !wasm_unknown

// .
// .
// .
// .
// .
package aiiosdk

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestACopiedControlCannotConsumeAnotherControlsSlot(t *testing.T) {
	taken := make(chan *Control, 2)
	send, _, done := controlLane(t, func(c *Control) { taken <- c })
	controlSend(t, send, 1)
	first := <-taken
	controlSend(t, send, 2)
	second := <-taken
	// .
	// .
	var copied Control
	reflect.ValueOf(&copied).Elem().Set(reflect.ValueOf(first).Elem())
	first.Answer(true, nil)
	copied.Answer(false, nil)
	remaining := len(second.reply.s.admissionSlots)
	send.Close()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("retirement blocked")
	}
	if remaining != 1 {
		t.Fatalf("copy answered twice and consumed another control's slot: remaining=%d want=1", remaining)
	}
}

func TestALateAnswerCannotEnqueueBehindTheRetiredWriter(t *testing.T) {
	for i := 0; i < 40; i++ {
		taken := make(chan *Control, 1)
		send, _, done := controlLane(t, func(c *Control) { taken <- c })
		controlSend(t, send, 1)
		c := <-taken
		send.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("retirement blocked")
		}
		<-c.reply.s.writerDone
		c.Answer(true, nil)
		if len(c.reply.s.outQ) != 0 {
			t.Fatalf("round %d: Answer enqueued %d frame(s) behind an already retired writer", i, len(c.reply.s.outQ))
		}
	}
}

func TestAFullResponseQueueCannotBlockAnswer(t *testing.T) {
	w := &reviewBlockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	s := &Session{out: w, closed: make(chan struct{}), admissionSlots: make(chan struct{}, 1)}
	s.startWriter()
	if _, err := s.enqueue(context.Background(), []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	<-w.entered
	for i := 0; i < sessionOutQueue; i++ {
		s.outQ <- &outbound{frame: []byte(`{}`), done: make(chan error, 1)}
	}
	s.admissionSlots <- struct{}{}
	c := &Control{reply: &controlReply{s: s, id: json.RawMessage("1")}}
	answered := make(chan struct{})
	go func() { c.Answer(true, nil); close(answered) }()
	blocked := false
	select {
	case <-answered:
	case <-time.After(100 * time.Millisecond):
		blocked = true
	}
	s.end()
	close(w.release)
	select {
	case <-answered:
	case <-time.After(time.Second):
		t.Fatal("Answer did not retire")
	}
	select {
	case <-s.writerDone:
	case <-time.After(time.Second):
		t.Fatal("writer did not retire")
	}
	if blocked {
		t.Fatal("Answer blocked behind a full response queue; an inline handler would stop subsequent cancellation admission")
	}
}
