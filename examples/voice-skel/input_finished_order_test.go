package main

import (
	"testing"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// A drain waits for the input to be finished and then ends the session,
// and the host may retire this process on session_end. So "finished", the
// condition the drain reads, may be published only after the tail's last
// final and input_finished are. While the state was set before them, a
// drain that looked in between ended the session ahead of both: a host's
// compatibility run heard session_ready, three finals and session_end, in
// a contiguous sequence, and never input_finished (3 runs of 400).
//
// The test stands where the wire is and holds each publication for six of
// the drain's polls. It reads the exact condition the drain reads, and
// what the drain does about it.
// heldWire stands where the wire is: each event the engine writes is
// handed to it by name, and it answers with the event's sequence. As the
// engine's own emit does, it runs what the event announces after the
// write, under the engine's lock.
type heldWire struct {
	e     *engine
	write func(typ string) int64
	// wrote, if set, is called as an emit returns, the lock released.
	wrote func(typ string)
}

func (h heldWire) emit(typ string, fields map[string]any, then ...func(seq int64)) int64 {
	seq := h.write(typ)
	h.e.mu.Lock()
	for _, f := range then {
		f(seq)
	}
	h.e.mu.Unlock()
	if h.wrote != nil {
		h.wrote(typ)
	}
	return seq
}

func TestInputIsSaidFinishedBeforeItCanReleaseDrain(t *testing.T) {
	e := newEngine()
	e.lifecycle, e.sessionID = "open", "s1"
	// finish_input was admitted: the cutoff is fixed and the tail pends.
	e.admittedEnd, e.inputState, e.finished = 16000, "finishing", true
	if _, err := e.close(aiiosdk.Object(`{"mode":"drain","reason":"done"}`)); err != nil {
		t.Fatalf("a drain after the cutoff must be admitted: %v", err)
	}

	var said []string
	e.finishTail("mic", 16000, heldWire{e: e, write: func(typ string) int64 {
		time.Sleep(30 * time.Millisecond) // the drain polls every 5 ms
		e.mu.Lock()
		state, lifecycle, completion := e.inputState, e.lifecycle, e.completion
		e.mu.Unlock()
		if state == "finished" || completion != nil {
			t.Errorf("%s is being published and the input already reads %q (completion %v): a drain may end the session ahead of it", typ, state, completion)
		}
		if lifecycle != "draining" {
			t.Errorf("%s is being published and the session is already %q", typ, lifecycle)
		}
		said = append(said, typ)
		return int64(len(said))
	}})
	if len(said) != 2 || said[0] != "transcript_final" || said[1] != "input_finished" {
		t.Fatalf("the tail said %v; want its last final, then input_finished", said)
	}

	e.mu.Lock()
	state, completion, processed := e.inputState, e.completion, e.processedEnd
	e.mu.Unlock()
	if state != "finished" || processed != 16000 {
		t.Fatalf("after input_finished the input reads %q through %d; want finished through 16000", state, processed)
	}
	if completion == nil || completion["sequence"] != int64(2) || completion["end_sample"] != int64(16000) {
		t.Fatalf("status must carry the completion the engine said, with its sequence: %v", completion)
	}
	// And the drain, released, closes the session.
	deadline := time.Now().Add(5 * time.Second)
	for {
		e.mu.Lock()
		closed := e.lifecycle == "closed"
		e.mu.Unlock()
		if closed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the drain never closed the session after its input finished")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// The host that has just read input_finished may ask for status at once.
// The state and the completion are therefore set with the event, under the
// wire's hold of the lock, and not by finishTail after emit has returned:
// in between, status still said "finishing", with no completion, of an
// input the host had been told was finished.
func TestInputReadsFinishedAsSoonAsItIsSaid(t *testing.T) {
	e := newEngine()
	e.lifecycle, e.sessionID = "open", "s1"
	e.admittedEnd, e.inputState, e.finished = 16000, "finishing", true
	var written int64
	returned := map[string]string{}
	e.finishTail("mic", 16000, heldWire{e: e, write: func(typ string) int64 {
		written++
		return written
	}, wrote: func(typ string) {
		// The event is on the wire and the lock is free: any reader runs now.
		st := e.status().(map[string]any)
		state := st["input"].(map[string]any)["state"].(string)
		completion, _ := st["input_completion"].(map[string]any)
		returned[typ] = state
		if typ == "input_finished" && (state != "finished" || completion == nil || completion["sequence"] != int64(2)) {
			t.Errorf("input_finished is written and status reads %q with completion %v", state, completion)
		}
	}})
	if returned["transcript_final"] != "finishing" || returned["input_finished"] != "finished" {
		t.Fatalf("status read as each event was written: %v; want finishing after the tail's final, finished after input_finished", returned)
	}
}

// The engine's own emit runs what an event announces under its lock, and
// does so even with no session to write to.
func TestEmitRunsWhatTheEventAnnouncesUnderTheLock(t *testing.T) {
	e := newEngine()
	ran := 0
	seq := e.emit("input_finished", nil, func(seq int64) {
		ran++
		if e.mu.TryLock() {
			e.mu.Unlock()
			t.Error("emit ran the event's state change without holding the engine's lock")
		}
		if seq != 0 {
			t.Errorf("no session is open and the event was numbered %d", seq)
		}
	})
	if ran != 1 || seq != 0 {
		t.Fatalf("emit ran the state change %d times and returned %d; want once and 0", ran, seq)
	}
	if !e.mu.TryLock() {
		t.Fatal("emit returned with the engine's lock held")
	}
	e.mu.Unlock()
}
