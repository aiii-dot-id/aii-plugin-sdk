package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// .
// .
// .
// .
// .
// .
func TestAnOutputOnlySessionHasNoInputAndNeedsNone(t *testing.T) {
	e := newEngine()
	open := aiiosdk.Object(`{"session_id":"typed","output_handle":"out:typed:spk","audio":{"format":"s16le","input":null,"output":{"rate":24000,"channels":1}},"test_synth_ms":30}`)
	res, err := e.open(open)
	if err != nil {
		t.Fatalf("the output-only open: %v", err)
	}
	wire, _ := json.Marshal(res)
	var admitted struct {
		Audio map[string]json.RawMessage `json:"audio"`
	}
	_ = json.Unmarshal(wire, &admitted)
	if in, present := admitted.Audio["input"]; !present || string(in) != "null" {
		t.Fatalf("the admission must say audio.input is null, not leave it out: %s", wire)
	}
	if !strings.Contains(string(admitted.Audio["output"]), `"rate":24000`) {
		t.Fatalf("the engine speaks the output's clock where there is no input: %s", wire)
	}

	snap, _ := json.Marshal(e.status())
	var st struct {
		Input struct {
			State string `json:"state"`
		} `json:"input"`
		Recognition struct {
			State string `json:"state"`
		} `json:"recognition"`
		Completion json.RawMessage `json:"input_completion"`
	}
	_ = json.Unmarshal(snap, &st)
	if st.Input.State != "absent" || st.Recognition.State != "inactive" || string(st.Completion) != "null" {
		t.Fatalf("status of a session that does not hear: %s", snap)
	}

	if _, err := e.finishInput(aiiosdk.Object(`{"session_id":"typed","stream_id":"in:typed:mic","end_sample":0}`)); err == nil || !strings.Contains(err.Error(), "no input direction") {
		t.Fatalf("finish_input must be refused, not answered with an invented cutoff: %v", err)
	}

	// .
	// .
	if _, err := e.synthesize(aiiosdk.Object(`{"session_id":"typed","synthesis_id":"reply-1","text":"typed words"}`)); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	g := e.byID["reply-1"]
	e.mu.Unlock()
	<-g.done
	if _, err := e.close(aiiosdk.Object(`{"session_id":"typed","mode":"drain","reason":"said"}`)); err != nil {
		t.Fatalf("a drain with no input boundary: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	e.mu.Lock()
	closedEarly := e.lifecycle == "closed"
	e.mu.Unlock()
	if closedEarly {
		t.Fatal("the drain ended without the reply's playback receipt")
	}
	report := aiiosdk.Object(`{"session_id":"typed","synthesis_id":"reply-1","output_stream":` + itoa(int64(g.stream)) + `,"rendered_samples":` + itoa(g.out.Load()) + `,"terminal":true}`)
	if _, err := e.playbackReport(report); err != nil {
		t.Fatalf("the reply's receipt: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e.mu.Lock()
		closed := e.lifecycle == "closed"
		e.mu.Unlock()
		if closed {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the drain never ended: it is waiting for an input that does not exist")
}

// .
// .
// .
func TestARefusedOpenLeavesTheEngineAnswering(t *testing.T) {
	e := newEngine()
	for _, bad := range []string{
		`{"session_id":"s","input_handle":"in:s:mic","output_handle":"out:s:spk","audio":{"format":"s16le","input":null,"output":{"rate":16000,"channels":1}}}`,
		`{"session_id":"s","output_handle":"out:s:spk","audio":{"format":"s16le","output":{"rate":16000,"channels":1}}}`,
		`{"session_id":"s","output_handle":"out:s:spk","audio":{"format":"s16le","input":null,"output":{"rate":16000,"channels":2}}}`,
		`{"session_id":"s","output_handle":"out:s:spk","audio":{"format":"s16le","input":null,"output":{"rate":4000,"channels":1}}}`,
	} {
		if _, err := e.open(aiiosdk.Object(bad)); err == nil || !strings.HasPrefix(err.Error(), "open refused") {
			t.Fatalf("not refused: %s (%v)", bad, err)
		}
	}
	answered := make(chan error, 1)
	go func() {
		_, err := e.open(aiiosdk.Object(`{"session_id":"s","output_handle":"out:s:spk","audio":{"format":"s16le","input":null,"output":{"rate":16000,"channels":1}}}`))
		answered <- err
	}()
	select {
	case err := <-answered:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a refused open left the engine holding its own lock")
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
