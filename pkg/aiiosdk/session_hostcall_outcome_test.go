//go:build !wasm_unknown

// A host call's outcome is told apart by errors.Is and errors.As — not
// sent (ErrNotSent), outcome unknown (ErrOutcomeUnknown), refused by the
// host (*Denied) — and its message reads as it always has. The writer is
// held at exact frames with the flush proofs' host seam, never with sleeps.
package aiiosdk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// secondWait is a context that says when its holder waits on it a second
// time: a host call waits first for its write, then for its reply.
type secondWait struct {
	context.Context
	calls   atomic.Int32
	waiting chan struct{}
}

func (c *secondWait) Done() <-chan struct{} {
	if c.calls.Add(1) == 2 {
		close(c.waiting)
	}
	return c.Context.Done()
}

// heldWait is a context that holds its holder at its first wait until the
// test lets it go: a host call meets its first verdicts only once they
// all stand.
type heldWait struct {
	context.Context
	once             sync.Once
	waiting, release chan struct{}
}

func (c *heldWait) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting); <-c.release })
	return c.Context.Done()
}

// outcomeOf names every class err is told apart as.
func outcomeOf(err error) []string {
	var got []string
	if errors.Is(err, ErrNotSent) {
		got = append(got, "not sent")
	}
	if errors.Is(err, ErrOutcomeUnknown) {
		got = append(got, "outcome unknown")
	}
	var d *Denied
	if errors.As(err, &d) {
		got = append(got, "refused")
	}
	return got
}

func hostCallOn(s *Session, ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() { _, err := s.HostCall(ctx, "voice.observe", map[string]any{}); done <- err }()
	return done
}

func TestAHostCallsOutcomeIsToldApartByErrorsIsAndAs(t *testing.T) {
	const refusal = `{"code":-32000,"message":"not granted","data":{"reasonCode":"POLICY_DENY","denied_at":"capability_evaluation"}}`
	const malformed = `{"code":"-32000","message":"not granted"}`
	// queuedBehindAnEvent queues a call while the writer is held at an
	// event ahead of it, then ends it with stop: the frame never left.
	queuedBehindAnEvent := func(t *testing.T, stop func(*Session, context.CancelFunc)) error {
		h, s := flushLane(t)
		emitFlushEvents(t, s, 0, 0)
		h.at(t, 1)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		nctx := waitNoticed(ctx)
		done := hostCallOn(s, nctx)
		<-nctx.waiting
		stop(s, cancel)
		return <-done
	}
	// beingWritten ends a call with stop while the writer is held at the
	// first byte of its frame.
	beingWritten := func(t *testing.T, stop func(*Session, context.CancelFunc)) error {
		h, s := flushLane(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := hostCallOn(s, ctx)
		h.at(t, 1)
		stop(s, cancel)
		return <-done
	}
	// awaitingTheReply ends a call with stop once its frame is written and
	// it waits for the reply.
	awaitingTheReply := func(t *testing.T, stop func(*Session, context.CancelFunc)) error {
		h, s := flushLane(t)
		h.release()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		sctx := &secondWait{Context: ctx, waiting: make(chan struct{})}
		done := hostCallOn(s, sctx)
		<-sctx.waiting
		stop(s, cancel)
		return <-done
	}
	// answered has the host answer a call with an error object.
	answered := func(t *testing.T, errObj string) error {
		h, s := flushLane(t)
		done := hostCallOn(s, context.Background())
		h.at(t, 1) // the call's id is pending
		s.deliverReply(json.RawMessage("1"), sessionReply{errObj: json.RawMessage(errObj)})
		h.pass()
		return <-done
	}
	byCancel := func(_ *Session, cancel context.CancelFunc) { cancel() }
	byEnd := func(s *Session, _ context.CancelFunc) { s.end() }

	for _, tc := range []struct {
		name    string
		outcome []string
		text    string // the message, as it has always read
		run     func(t *testing.T) error
	}{
		{"never queued", []string{"not sent"},
			"aiiosdk: hostcall not sent: the lane ended, or the call was cancelled, before the frame was queued",
			func(t *testing.T) error {
				_, s := flushLane(t)
				_, err := s.HostCall(cancelled(), "voice.observe", map[string]any{})
				return err
			}},
		{"withdrawn by cancellation", []string{"not sent"},
			"aiiosdk: hostcall not sent: cancelled before the transport took the frame",
			func(t *testing.T) error { return queuedBehindAnEvent(t, byCancel) }},
		{"queued when the lane ended", []string{"not sent"},
			"aiiosdk: hostcall not sent: the lane ended before the frame was written",
			func(t *testing.T) error { return queuedBehindAnEvent(t, byEnd) }},
		{"cancelled while written", []string{"outcome unknown"},
			"aiiosdk: hostcall outcome unknown: cancelled while the transport was writing the frame",
			func(t *testing.T) error { return beingWritten(t, byCancel) }},
		{"the lane ended while written", []string{"outcome unknown"},
			"aiiosdk: hostcall outcome unknown: the lane ended while the transport was writing the frame",
			func(t *testing.T) error { return beingWritten(t, byEnd) }},
		{"sent, cancelled before the reply", []string{"outcome unknown"},
			"aiiosdk: hostcall outcome unknown: sent, no reply before cancellation",
			func(t *testing.T) error { return awaitingTheReply(t, byCancel) }},
		{"sent, the lane ended before the reply", []string{"outcome unknown"},
			"aiiosdk: hostcall outcome unknown: sent, the session ended before the host answered",
			func(t *testing.T) error { return awaitingTheReply(t, byEnd) }},
		{"refused", []string{"refused"},
			"aiiosdk: host refused: " + refusal,
			func(t *testing.T) error {
				err := answered(t, refusal)
				if d, ok := AsDenied(err); !ok || d.Code != -32000 || d.Message != "not granted" || d.ReasonCode != "POLICY_DENY" || d.DeniedAt != "capability_evaluation" {
					t.Errorf("AsDenied = %+v, %v: want the host's error object, its reason code POLICY_DENY", d, ok)
				}
				return err
			}},
		// An error object outside its shape is a broken host, as InvokeCall
		// reads it: a plain error, no *Denied.
		{"answered with a malformed error object", nil,
			"aiiosdk: host refused: " + malformed,
			func(t *testing.T) error { return answered(t, malformed) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(t)
			if err == nil {
				t.Fatal("the call succeeded")
			}
			if err.Error() != tc.text {
				t.Errorf("message %q, want %q", err, tc.text)
			}
			if got := outcomeOf(err); fmt.Sprint(got) != fmt.Sprint(tc.outcome) {
				t.Errorf("errors.Is/As tell %q apart as %v, want %v", err, got, tc.outcome)
			}
		})
	}
}

// A write that fails after it began faults the lane before the writer
// reports the failure, so a call already waiting is woken by the lane's
// end. A call that meets both verdicts at once may say either; it is held
// until both stand and asked again until it has said each. Both are an
// unknown outcome; said as the failed write, that failure is its cause, on
// the one Unwrap chain it always had.
func TestAHostCallWhoseWriteFailedIsAnUnknownOutcomeWithItsCause(t *testing.T) {
	injected := errors.New("injected write failure")
	const failed = "aiiosdk: hostcall outcome unknown: the write failed after it began: injected write failure"
	const ended = "aiiosdk: hostcall outcome unknown: the lane ended while the transport was writing the frame"
	said := map[string]bool{}
	for i := 0; i < 64 && len(said) < 2; i++ {
		h, s := flushLane(t)
		ctx := &heldWait{Context: context.Background(), waiting: make(chan struct{}), release: make(chan struct{})}
		done := hostCallOn(s, ctx)
		<-ctx.waiting // queued, and held before its first wait
		h.at(t, 1)
		h.fail(injected)
		<-s.writerDone // the lane's end and the writer's verdict both stand
		close(ctx.release)
		err := <-done
		if err == nil || (err.Error() != failed && err.Error() != ended) {
			t.Fatalf("message %v, want %q or %q", err, failed, ended)
		}
		said[err.Error()] = true
		if got := outcomeOf(err); fmt.Sprint(got) != "[outcome unknown]" {
			t.Fatalf("errors.Is/As tell %q apart as %v, want [outcome unknown]", err, got)
		}
		if err.Error() == failed && errors.Unwrap(err) != injected {
			t.Fatalf("%q unwraps to %v, want the failed write", err, errors.Unwrap(err))
		}
	}
	if !said[failed] || !said[ended] {
		t.Fatalf("said %v in 64 calls, want both", said)
	}
}
