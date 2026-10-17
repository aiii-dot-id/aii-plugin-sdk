package main

import (
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

func playbackReportForTest(e *engine, args aiiosdk.Object) (result any, err error) {
	e.playbackReport(args, func(r any, cause error) { result, err = r, cause })
	return
}

// The last receipt is the drain's release condition. Hold its reply at the
// publisher seam: publishing the resolved bit before Answer enqueues that
// reply permits session_end to overtake it and the host to retire the child.
// This test observes the exact condition the drain reads, not a sleep/loop.
func TestPlaybackReceiptIsAnsweredBeforeItCanReleaseDrain(t *testing.T) {
	e := newEngine()
	g := &generation{id: "reply", stream: 7}
	g.ended.Store(true)
	g.out.Store(80)
	e.lifecycle, e.sessionID = "draining", "s1"
	e.byID[g.id] = g
	report := aiiosdk.Object(`{"synthesis_id":"reply","output_stream":7,"rendered_samples":80,"terminal":true}`)
	answers := 0
	e.playbackReport(report, func(result any, err error) {
		answers++
		if err != nil || result == nil {
			t.Fatalf("valid receipt refused: %v", err)
		}
		e.mu.Lock()
		resolved, closed := g.resolved, e.lifecycle == "closed"
		e.mu.Unlock()
		if resolved || closed {
			t.Error("drain became eligible before the receipt reply was published")
		}
	})
	if answers != 1 || !g.resolved {
		t.Fatalf("reply/resolve did not finish exactly once: answers=%d resolved=%v", answers, g.resolved)
	}
	if _, err := playbackReportForTest(e, report); err != nil {
		t.Fatalf("exact retry changed: %v", err)
	}
	if _, err := playbackReportForTest(e, aiiosdk.Object(`{"synthesis_id":"reply","output_stream":7,"rendered_samples":81,"terminal":true}`)); err == nil {
		t.Fatal("contradictory receipt admitted")
	}
}
