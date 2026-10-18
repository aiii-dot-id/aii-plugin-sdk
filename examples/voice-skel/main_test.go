package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// The engine's contract, driven over the real lane against the built
// binary: admission is not completion; a drain close is refused before
// the cutoff and outlives its acknowledgement after it; an interruption
// resolves the synthesis with a cancellation, not an end; a synthesis id
// is never reused; and every event carries the session's identity in a
// contiguous sequence.
func TestEngineKeepsTheResidentContract(t *testing.T) {
	bin := filepath.Join(t.TempDir(), exeName("voice-skel"))
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	type frame struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
		Params json.RawMessage `json:"params"`
	}
	frames := make(chan frame, 4096)
	go func() {
		defer close(frames)
		for {
			raw, err := aiiosdk.ReadFrame(stdout, aiiosdk.MaxControlFrameBytes)
			if err != nil {
				return
			}
			var f frame
			_ = json.Unmarshal(raw, &f)
			frames <- f
		}
	}()
	var events []map[string]any
	next := func() (frame, bool) {
		select {
		case f, ok := <-frames:
			return f, ok
		case <-time.After(5 * time.Second):
			return frame{}, false
		}
	}
	nextID := 0
	send := func(op string, args map[string]any) int {
		t.Helper()
		nextID++
		a, _ := json.Marshal(args)
		req := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"invoke.call","params":{"operation":%q,"arguments":%s}}`, nextID, op, a))
		if err := aiiosdk.WriteFrame(stdin, req, aiiosdk.MaxControlFrameBytes); err != nil {
			t.Fatalf("send %s: %v", op, err)
		}
		return nextID
	}
	// reply waits for the response to id, banking events seen meanwhile.
	reply := func(id int) (map[string]any, string) {
		t.Helper()
		for {
			f, ok := next()
			if !ok {
				t.Fatalf("no reply to request %d within its bound", id)
			}
			if f.Method == "session.event" {
				var ev map[string]any
				_ = json.Unmarshal(f.Params, &ev)
				events = append(events, ev)
				continue
			}
			if string(f.ID) != fmt.Sprint(id) {
				t.Fatalf("reply out of order: got id %s, want %d", f.ID, id)
			}
			if len(f.Error) > 0 {
				var e struct {
					Message string `json:"message"`
				}
				_ = json.Unmarshal(f.Error, &e)
				return nil, e.Message
			}
			var res map[string]any
			_ = json.Unmarshal(f.Result, &res)
			return res, ""
		}
	}
	// event waits for the next event of a type, within a bound.
	event := func(typ string, within time.Duration) map[string]any {
		t.Helper()
		for i := range events {
			if events[i]["type"] == typ && events[i]["_seen"] == nil {
				events[i]["_seen"] = true
				return events[i]
			}
		}
		deadline := time.Now().Add(within)
		for time.Now().Before(deadline) {
			select {
			case f, ok := <-frames:
				if !ok {
					t.Fatalf("the lane ended while waiting for %s", typ)
				}
				if f.Method != "session.event" {
					t.Fatalf("unexpected non-event frame while waiting for %s: %s", typ, f.ID)
				}
				var ev map[string]any
				_ = json.Unmarshal(f.Params, &ev)
				events = append(events, ev)
				if ev["type"] == typ {
					ev["_seen"] = true
					return ev
				}
			case <-time.After(time.Until(deadline)):
			}
		}
		return nil
	}
	admitted := func(op string, args map[string]any) map[string]any {
		t.Helper()
		res, msg := reply(send(op, args))
		if msg != "" {
			t.Fatalf("%s refused: %s", op, msg)
		}
		if res["accepted"] != true {
			t.Fatalf("%s: not an admission: %v", op, res)
		}
		return res
	}
	refused := func(op string, args map[string]any) string {
		t.Helper()
		res, msg := reply(send(op, args))
		if msg == "" {
			t.Fatalf("%s must be refused, was admitted: %v", op, res)
		}
		return msg
	}
	status := func() map[string]any {
		t.Helper()
		res, msg := reply(send(aiiosdk.OpSessionStatus, map[string]any{"session_id": "s1"}))
		if msg != "" {
			t.Fatalf("status: %s", msg)
		}
		return res
	}
	sub := func(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }

	// 1. Open: admitted into opening; readiness is an event.
	if got := admitted(aiiosdk.OpSessionOpen, map[string]any{"session_id": "s1", "test_close_delay_ms": 400, "test_synth_ms": 5000})["state"]; got != "opening" {
		t.Fatalf("open admitted as %v, want opening", got)
	}
	if event("session_ready", 5*time.Second) == nil {
		t.Fatal("no session_ready event")
	}
	// 2. Synthesize: admitted; the snapshot shows it generating and playing.
	admitted(aiiosdk.OpSessionSynthesize, map[string]any{"session_id": "s1", "synthesis_id": "g1", "text": "hello there"})
	if ev := event("synthesis_start", 5*time.Second); ev == nil || ev["output_stream"] != float64(1) {
		t.Fatalf("synthesis_start names the reply's output stream at admission: %v", ev)
	}
	s := status()
	if sub(s, "playback")["state"] != "playing" || sub(s, "synthesis")["state"] != "generating" {
		t.Fatalf("after synthesize: %v", s)
	}
	// 3. A drain close before any cutoff is refused.
	if msg := refused(aiiosdk.OpSessionClose, map[string]any{"session_id": "s1", "mode": "drain", "reason": "early"}); msg == "" {
		t.Fatal("drain close without cutoff must carry a reason")
	}
	// 4. Interruption: BOTH controls, admitted on their own; the synthesis
	// resolves as cancelled — never as an end — and nothing more plays.
	admitted(aiiosdk.OpSessionStopPlayback, map[string]any{"session_id": "s1", "synthesis_id": "g1", "reason": "operator"})
	admitted(aiiosdk.OpSessionCancelSynth, map[string]any{"session_id": "s1", "synthesis_id": "g1", "reason": "operator"})
	if event("playback_stop", 5*time.Second) == nil {
		t.Fatal("no playback_stop")
	}
	if event("synthesis_cancelled", 5*time.Second) == nil {
		t.Fatal("no synthesis_cancelled")
	}
	if ev := event("synthesis_end", 200*time.Millisecond); ev != nil {
		t.Fatalf("a cancelled synthesis must not also end: %v", ev)
	}
	// 5. Its id is never reused.
	refused(aiiosdk.OpSessionSynthesize, map[string]any{"session_id": "s1", "synthesis_id": "g1", "text": "again"})
	// 6. The cutoff, then its finalized tail.
	admitted(aiiosdk.OpSessionFinishInput, map[string]any{"session_id": "s1", "stream_id": "in1", "end_sample": 16000})
	if ev := event("transcript_final", 5*time.Second); ev == nil || ev["end_sample"] != float64(16000) {
		t.Fatalf("tail transcript: %v", ev)
	}
	// 6b. INPUT COMPLETION is the engine's word, after the last final: once,
	// naming the handle and the exact cutoff; status carries the same;
	// an identical retry re-arms nothing and a changed cutoff is refused.
	if ev := event("input_finished", 5*time.Second); ev == nil || ev["end_sample"] != float64(16000) || ev["processed_end_sample"] != float64(16000) || ev["stream_id"] != "in1" {
		t.Fatalf("input_finished: %v", ev)
	}
	if c := sub(status(), "input_completion"); c == nil || c["end_sample"] != float64(16000) || c["stream_id"] != "in1" || c["sequence"] == nil {
		t.Fatalf("status must carry the completion descriptor: %v", status())
	}
	admitted(aiiosdk.OpSessionFinishInput, map[string]any{"session_id": "s1", "stream_id": "in1", "end_sample": 16000})
	if ev := event("input_finished", 150*time.Millisecond); ev != nil {
		t.Fatalf("an identical-cutoff retry must not complete twice: %v", ev)
	}
	refused(aiiosdk.OpSessionFinishInput, map[string]any{"session_id": "s1", "stream_id": "in1", "end_sample": 16001})
	// 6c. PLAYBACK REPORTS: g1 was fenced with nothing delivered, so its
	// terminal report is a stop at zero; an exact retry is idempotent, a
	// changed one refused, impossible progress and a non-boolean terminal
	// refused, an unknown synthesis refused.
	rep := admitted(aiiosdk.OpSessionPlaybackReport, map[string]any{"session_id": "s1", "synthesis_id": "g1", "output_stream": 1, "rendered_samples": 0, "terminal": true})
	if rep["terminal"] != true {
		t.Fatalf("playback_report admission: %v", rep)
	}
	if ev := event("playback_observation", 5*time.Second); ev == nil || ev["outcome"] != "stopped" || ev["playback_verified"] != false || ev["evidence"] != "host_validated_client_report" {
		t.Fatalf("playback_observation: %v", ev)
	}
	admitted(aiiosdk.OpSessionPlaybackReport, map[string]any{"session_id": "s1", "synthesis_id": "g1", "output_stream": 1, "rendered_samples": 0, "terminal": true})
	if ev := event("playback_observation", 150*time.Millisecond); ev != nil {
		t.Fatalf("an exact terminal retry mints no second observation: %v", ev)
	}
	refused(aiiosdk.OpSessionPlaybackReport, map[string]any{"session_id": "s1", "synthesis_id": "g1", "output_stream": 1, "rendered_samples": 0, "terminal": false})
	refused(aiiosdk.OpSessionPlaybackReport, map[string]any{"session_id": "s1", "synthesis_id": "g1", "output_stream": 1, "rendered_samples": 1, "terminal": true})
	refused(aiiosdk.OpSessionPlaybackReport, map[string]any{"session_id": "s1", "synthesis_id": "g1", "output_stream": 1, "rendered_samples": 0, "terminal": 1})
	refused(aiiosdk.OpSessionPlaybackReport, map[string]any{"session_id": "s1", "synthesis_id": "nobody", "output_stream": 1, "rendered_samples": 0, "terminal": true})
	// 7. A second reply, fenced, still awaits ITS receipt. Drain close:
	// admitted into draining; the session is NOT closed on that
	// acknowledgement, and not after the engine's delay either — not until
	// every generation has the page's terminal report; then the engine
	// says so.
	admitted(aiiosdk.OpSessionSynthesize, map[string]any{"session_id": "s1", "synthesis_id": "g2", "text": "the final reply"})
	if ev := event("synthesis_start", 5*time.Second); ev == nil || ev["output_stream"] != float64(2) {
		t.Fatalf("synthesis_start names the reply's output stream: %v", ev)
	}
	admitted(aiiosdk.OpSessionCancelSynth, map[string]any{"session_id": "s1", "synthesis_id": "g2", "reason": "operator"})
	if ev := event("synthesis_cancelled", 5*time.Second); ev == nil || ev["output_stream"] != float64(2) {
		t.Fatalf("the cancelled synthesis names its stream: %v", ev)
	}
	if got := admitted(aiiosdk.OpSessionClose, map[string]any{"session_id": "s1", "mode": "drain", "reason": "done"})["state"]; got != "draining" {
		t.Fatalf("close admitted as %v, want draining", got)
	}
	if got := status()["lifecycle"]; got != "draining" {
		t.Fatalf("lifecycle after close admission = %v, want draining", got)
	}
	if ev := event("session_end", 700*time.Millisecond); ev != nil {
		t.Fatalf("session_end arrived without the reply's playback receipt: %v", ev)
	}
	admitted(aiiosdk.OpSessionPlaybackReport, map[string]any{"session_id": "s1", "synthesis_id": "g2", "output_stream": 2, "rendered_samples": 0, "terminal": true})
	if event("session_end", 5*time.Second) == nil {
		t.Fatal("the engine never reported session_end")
	}
	if got := status()["lifecycle"]; got != "closed" {
		t.Fatalf("lifecycle after session_end = %v, want closed", got)
	}
	// 8. Every event carried the session and a contiguous sequence with a
	// unique id.
	ids := map[string]bool{}
	for i, ev := range events {
		if ev["session_id"] != "s1" {
			t.Fatalf("event %d without the session's identity: %v", i, ev)
		}
		if ev["sequence"] != float64(i+1) {
			t.Fatalf("event %d has sequence %v, want %d (contiguous)", i, ev["sequence"], i+1)
		}
		id, _ := ev["id"].(string)
		if id == "" || ids[id] {
			t.Fatalf("event %d id %q is empty or reused", i, id)
		}
		ids[id] = true
	}
	// 9. The host ends the lane; the engine exits cleanly.
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the engine must exit cleanly when the host ends the lane: %v", err)
	}
}

// Session audio reaches the engine on inherited descriptors and comes
// back with every span intact; the input's end ends the output.
func TestEngineEchoesSessionAudioWithSpansIntact(t *testing.T) {
	bin := filepath.Join(t.TempDir(), exeName("voice-skel"))
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	cmd := exec.Command(bin)
	cmd.Env = []string{} // nothing of this process's environment: the engine gets its audio pair alone
	hostIn, hostOut, started := giveAudioPair(t, cmd)
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	started()
	// The audio loop starts at the first admitted control: open.
	open := []byte(`{"jsonrpc":"2.0","id":1,"method":"invoke.call","params":{"operation":"speech.session.open","arguments":{"session_id":"s1","input_handle":"in:s1:mic","output_handle":"out:s1:spk","audio":{"format":"s16le","rate":16000,"channels":1}}}}`)
	if err := aiiosdk.WriteFrame(stdin, open, aiiosdk.MaxControlFrameBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := aiiosdk.ReadFrame(stdout, aiiosdk.MaxControlFrameBytes); err != nil {
		t.Fatalf("open reply: %v", err)
	}
	pcm := make([]byte, 640)
	for i := range pcm {
		pcm[i] = byte(i)
	}
	want := []aiiosdk.AudioFrame{
		{Kind: aiiosdk.AudioPCM, Stream: 3, Seq: 1, Start: 0, PCM: pcm},
		{Kind: aiiosdk.AudioPCM, Stream: 3, Seq: 2, Start: 320, PCM: pcm},
		{Kind: aiiosdk.AudioDiscontinuity, Stream: 3, Seq: 3, Start: 1000},
		{Kind: aiiosdk.AudioEnd, Stream: 3, Seq: 4, Start: 1000},
	}
	for _, fr := range want {
		if err := aiiosdk.WriteAudioFrame(hostIn, fr); err != nil {
			t.Fatal(err)
		}
	}
	// The echo comes back with every span intact, under ONE output stream
	// id of the engine's own choosing — never the host's input id.
	var outStream uint32
	for i, w := range want {
		got, err := aiiosdk.ReadAudioFrame(hostOut)
		if err != nil {
			t.Fatalf("echo %d: %v", i, err)
		}
		if i == 0 {
			outStream = got.Stream
			if outStream == 0 {
				t.Fatalf("the echo's stream id is the engine's, never zero: %+v", got)
			}
		}
		if got.Kind != w.Kind || got.Stream != outStream || got.Seq != w.Seq || got.Start != w.Start || string(got.PCM) != string(w.PCM) {
			t.Fatalf("echo %d: got %+v want %+v under stream %d", i, got, w, outStream)
		}
	}
	// The snapshot carries the audio's end samples.
	status := []byte(`{"jsonrpc":"2.0","id":2,"method":"invoke.call","params":{"operation":"speech.session.status","arguments":{"session_id":"s1"}}}`)
	if err := aiiosdk.WriteFrame(stdin, status, aiiosdk.MaxControlFrameBytes); err != nil {
		t.Fatal(err)
	}
	// The engine's notifications interleave with replies: read to the
	// reply that answers the status request.
	//
	// AND THE ECHO'S STREAM WAS NAMED ON THIS LANE. The host delivers
	// audio only on an output stream the control lane introduced — it
	// routes by that word, never by which session happens to be listening
	// — so an engine that speaks on a stream it never named is speaking
	// to nobody. The word is synthesis_start, as for a reply.
	var reply []byte
	namedEcho := false
	for {
		frame, err := aiiosdk.ReadFrame(stdout, aiiosdk.MaxControlFrameBytes)
		if err != nil {
			t.Fatal(err)
		}
		var head struct {
			ID     json.RawMessage `json:"id"`
			Params struct {
				Type         string `json:"type"`
				SynthesisID  string `json:"synthesis_id"`
				OutputStream *int64 `json:"output_stream"`
			} `json:"params"`
		}
		_ = json.Unmarshal(frame, &head)
		if head.Params.Type == "synthesis_start" && head.Params.OutputStream != nil && uint32(*head.Params.OutputStream) == outStream {
			if head.Params.SynthesisID == "" {
				t.Fatalf("the word that names a stream names what it carries: %s", frame)
			}
			namedEcho = true
		}
		if string(head.ID) == "2" {
			reply = frame
			break
		}
	}
	if !namedEcho {
		t.Fatalf("the engine echoed audio on output stream %d and never named it on the control lane", outStream)
	}
	var snap struct {
		Result struct {
			Audio struct {
				In  int64 `json:"input_end_sample"`
				Out int64 `json:"output_end_sample"`
			} `json:"audio"`
		} `json:"result"`
	}
	_ = json.Unmarshal(reply, &snap)
	if snap.Result.Audio.In != 1000 || snap.Result.Audio.Out != 1000 {
		t.Fatalf("status audio counters: %+v", snap.Result.Audio)
	}
	stdin.Close()
	hostIn.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("the engine must exit cleanly when the host ends the lane: %v", err)
	}
}

// A barge-in names no generation: an empty synthesis_id cancels the
// current one, and with nothing running it is admitted as resolved.
func TestEngineCancelsTheCurrentGenerationByDefault(t *testing.T) {
	e := newEngine()
	if _, err := e.open(aiiosdk.Object(`{"session_id":"s1","test_synth_ms":5000}`)); err != nil {
		t.Fatal(err)
	}
	res, err := e.cancel(aiiosdk.Object(`{}`))
	if err != nil || res.(map[string]any)["already_resolved"] != true {
		t.Fatalf("nothing running: %v %v", res, err)
	}
	if _, err := e.synthesize(aiiosdk.Object(`{"session_id":"s1","synthesis_id":"g1","text":"a long reply"}`)); err != nil {
		t.Fatal(err)
	}
	res, err = e.cancel(aiiosdk.Object(`{}`))
	if err != nil || res.(map[string]any)["synthesis_id"] != "g1" || res.(map[string]any)["already_resolved"] != false {
		t.Fatalf("the current generation: %v %v", res, err)
	}
}

// The reply's audio is its own output stream, contiguous from zero,
// ended exactly where the interruption lands; the next reply is a new
// stream. Driven in one process over pipes of the test's own.
func TestSynthesisIsItsOwnOutputStreamEndedAtTheFence(t *testing.T) {
	inR, _, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer inR.Close()
	defer outR.Close()
	e := newEngine()
	e.audio = aiiosdk.NewAudioPair(inR, outW)
	if _, err := e.open(aiiosdk.Object(`{"session_id":"s1","test_synth_ms":5000,"audio":{"rate":24000,"channels":1}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.open(aiiosdk.Object(`{"session_id":"s2","audio":{"rate":24000,"channels":2}}`)); err == nil {
		t.Fatal("stereo must be refused at the open")
	}
	next := func() aiiosdk.AudioFrame {
		t.Helper()
		fr, err := aiiosdk.ReadAudioFrame(outR)
		if err != nil {
			t.Fatal(err)
		}
		return fr
	}
	if _, err := e.synthesize(aiiosdk.Object(`{"session_id":"s1","synthesis_id":"g1","text":"a long reply"}`)); err != nil {
		t.Fatal(err)
	}
	first := next()
	if first.Kind != aiiosdk.AudioPCM || first.Start != 0 || first.Samples(1) != 120 {
		t.Fatalf("the reply starts at zero in five-millisecond frames at the host's rate: %+v", first)
	}
	var end int64 = first.Samples(1)
	for i := 0; i < 5; i++ {
		fr := next()
		if fr.Kind != aiiosdk.AudioPCM || fr.Stream != first.Stream || fr.Start != end {
			t.Fatalf("frame %d is not contiguous on the reply's stream: %+v (want start %d, stream %d)", i, fr, end, first.Stream)
		}
		end += fr.Samples(1)
	}
	if _, err := e.cancel(aiiosdk.Object(`{"session_id":"s1","synthesis_id":"g1"}`)); err != nil {
		t.Fatal(err)
	}
	// Whatever was in flight lands, then the end — at the exact sample.
	var last aiiosdk.AudioFrame
	for {
		fr := next()
		if fr.Stream != first.Stream {
			t.Fatalf("a frame of another stream before the reply's end: %+v", fr)
		}
		if fr.Kind == aiiosdk.AudioEnd {
			last = fr
			break
		}
		end += fr.Samples(1)
	}
	if last.Start != end {
		t.Fatalf("the end is at %d, the audio stood at %d", last.Start, end)
	}
	if _, err := e.synthesize(aiiosdk.Object(`{"session_id":"s1","synthesis_id":"g2","text":"the next reply"}`)); err != nil {
		t.Fatal(err)
	}
	fr := next()
	if fr.Stream == first.Stream || fr.Kind != aiiosdk.AudioPCM || fr.Start != 0 {
		t.Fatalf("the next reply is a new stream from zero, nothing of the old one after its end: %+v", fr)
	}
	// A playback stop ends the stream where it stands; the synthesis goes on.
	if _, err := e.stop(aiiosdk.Object(`{"session_id":"s1","synthesis_id":"g2"}`)); err != nil {
		t.Fatal(err)
	}
	for {
		fr := next()
		if fr.Kind == aiiosdk.AudioEnd {
			break
		}
	}
	if s := e.status().(map[string]any)["synthesis"].(map[string]any); s["state"] != "generating" {
		t.Fatalf("the synthesis must go on after a playback stop: %v", s)
	}
	if _, err := e.cancel(aiiosdk.Object(`{"session_id":"s1"}`)); err != nil {
		t.Fatal(err)
	}
}

// The host names both endpoint formats — each with its handle, as the
// shared topology rules require — and the engine answers with the one
// clock it speaks — the host's input rate, or the rate a test names —
// for both directions, and refuses a channel count it cannot speak.
func TestEngineAnswersTheOpenWithTheFormatsItSpeaks(t *testing.T) {
	e := newEngine()
	res, err := e.open(aiiosdk.Object(`{"session_id":"s1","input_handle":"in:s1:mic","output_handle":"out:s1:spk","audio":{"format":"s16le","input":{"rate":48000,"channels":1},"output":{"rate":48000,"channels":1}}}`))
	if err != nil {
		t.Fatal(err)
	}
	au := res.(map[string]any)["audio"].(map[string]any)
	if au["input"].(map[string]any)["rate"] != 48000 || au["output"].(map[string]any)["rate"] != 48000 {
		t.Fatalf("the host's rate, when the engine can speak it: %v", au)
	}
	e2 := newEngine()
	res, err = e2.open(aiiosdk.Object(`{"session_id":"s2","test_engine_rate":16000,"input_handle":"in:s2:mic","output_handle":"out:s2:spk","audio":{"input":{"rate":48000,"channels":1},"output":{"rate":48000,"channels":1}}}`))
	if err != nil {
		t.Fatal(err)
	}
	au = res.(map[string]any)["audio"].(map[string]any)
	if au["input"].(map[string]any)["rate"] != 16000 || au["output"].(map[string]any)["rate"] != 16000 {
		t.Fatalf("the rate a test names, for both directions: %v", au)
	}
	if _, err := newEngine().open(aiiosdk.Object(`{"session_id":"s3","input_handle":"in:s3:mic","output_handle":"out:s3:spk","audio":{"input":{"rate":48000,"channels":2},"output":{"rate":48000,"channels":2}}}`)); err == nil {
		t.Fatal("stereo input must be refused at the open")
	}
}

// A CHECK'S OPEN IS AN OPEN: the host's publisher's checks open sessions
// that say "check": true, the one sign that the engine keeps nothing of
// the session. This skeleton keeps nothing of any session, so it accepts
// the sign and ignores it — an engine that records or remembers what it
// hears must not, for such a session.
func TestACheckSessionsOpenIsAccepted(t *testing.T) {
	e := newEngine()
	res, err := e.open(aiiosdk.Object(`{"session_id":"check-1","check":true,"output_handle":"check-1-out","audio":{"format":"s16le","input":null,"output":{"rate":48000,"channels":1}}}`))
	if err != nil {
		t.Fatalf("a check's open is refused: %v", err)
	}
	if m, _ := res.(map[string]any); m["accepted"] != true || m["session_id"] != "check-1" {
		t.Fatalf("a check's open is admitted: %v", res)
	}
}
