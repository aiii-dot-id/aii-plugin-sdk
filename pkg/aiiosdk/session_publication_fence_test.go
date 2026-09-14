//go:build !wasm_unknown

// .
// .
// .
// .
package aiiosdk

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type slowVerdict struct{ entered, release chan struct{} }

func (v slowVerdict) MarshalJSON() ([]byte, error) {
	close(v.entered)
	<-v.release
	return []byte(`{"original":true}`), nil
}

func TestACopiedControlNeitherWaitsForTheOriginalsAnswerNorRetargetsIt(t *testing.T) {
	taken := make(chan *Control, 1)
	send, read, done := controlLane(t, func(c *Control) { taken <- c })
	controlSend(t, send, 1)
	c := <-taken
	copy := *c
	c.Session, c.Op, c.Args = nil, "changed", Object(`{}`)
	v := slowVerdict{make(chan struct{}), make(chan struct{})}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(v.release) }) }
	defer release()
	first := make(chan struct{})
	go func() { c.Answer(v, nil); close(first) }()
	<-v.entered
	second := make(chan struct{})
	go func() { copy.Answer(false, nil); close(second) }()
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("copy waited for a verdict already claimed by the original")
	}
	release()
	<-first
	r := controlRead(t, read)
	if string(r["id"]) != "1" || string(r["result"]) != `{"original":true}` {
		t.Fatalf("exported fields/copy changed the original reply: %s", r)
	}
	if len(copy.reply.s.admissionSlots) != 0 {
		t.Fatal("the answered control retained its slot")
	}
	send.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("copy proof did not retire")
	}
}

func TestLateEventsAndHostCallsCannotPublishAfterRetirement(t *testing.T) {
	taken := make(chan *Control, 1)
	send, _, done := controlLane(t, func(c *Control) { taken <- c })
	controlSend(t, send, 1)
	c := <-taken
	s := c.Session
	send.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retirement blocked")
	}
	<-s.writerDone
	for i := 0; i < 64; i++ {
		if err := s.Emit(map[string]bool{"late": true}); err == nil {
			t.Fatal("late event claimed admission after writer retirement")
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_, err := s.HostCall(ctx, "late", nil)
		cancel()
		if err == nil || !strings.Contains(err.Error(), "not sent") {
			t.Fatalf("late host call has wrong custody: %v", err)
		}
		c.Answer(true, nil)
		if len(s.outQ) != 0 || s.faultErr() != nil {
			t.Fatal("a late publisher changed a retired lane")
		}
	}
}

func fullQueueLane(t *testing.T) (*Session, func()) {
	t.Helper()
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
	var once sync.Once
	release := func() { once.Do(func() { close(w.release) }) }
	t.Cleanup(func() {
		s.end()
		release()
		select {
		case <-s.writerDone:
		case <-time.After(time.Second):
			t.Error("queue writer leaked")
		}
	})
	return s, release
}

func TestAnOrdinaryEnqueueStillWaitsForCapacityUnderItsContext(t *testing.T) {
	s, release := fullQueueLane(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	ob, err := s.enqueue(ctx, []byte(`{"cancelled":true}`))
	if ob != nil || !errors.Is(err, errNotSent) || s.faultErr() != nil {
		t.Fatalf("context cancellation became publication or a lane fault: ob=%v err=%v", ob, err)
	}
	ready := make(chan *outbound, 1)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	go func() {
		ob, err := s.enqueue(ctx2, []byte(`{"waited":true}`))
		if err != nil {
			ready <- nil
			return
		}
		ready <- ob
	}()
	select {
	case <-ready:
		t.Fatal("a full queue neither waited nor preserved its caller")
	case <-time.After(20 * time.Millisecond):
	}
	release()
	select {
	case ob := <-ready:
		if ob == nil {
			t.Fatal("capacity release did not wake the pending enqueue")
		}
		select {
		case err := <-ob.done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx2.Done():
			t.Fatal("woken frame was not delivered")
		}
	case <-ctx2.Done():
		t.Fatal("capacity release was lost")
	}
}

func TestAFullReplyQueueFaultsTheLaneAndReleasesExactlyItsSlot(t *testing.T) {
	s, _ := fullQueueLane(t)
	s.admissionSlots <- struct{}{}
	c := &Control{reply: &controlReply{s: s, id: json.RawMessage("1")}}
	returned := make(chan struct{})
	go func() { c.Answer(true, nil); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("reply waited for queue capacity")
	}
	if err := s.faultErr(); err == nil || !strings.Contains(err.Error(), "outbound queue saturated") {
		t.Fatalf("undeliverable reply did not fault explicitly: %v", err)
	}
	if len(s.admissionSlots) != 0 {
		t.Fatal("faulted reply retained its slot")
	}
	copy := *c
	copy.Answer(false, nil)
}
