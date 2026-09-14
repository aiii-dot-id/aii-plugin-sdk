// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// .
type generation struct {
	// .
	// .
	// .
	reported  int64
	resolved  bool
	id        string
	cancelled bool
	stopped   bool
	done      chan struct{}
	// .
	// .
	stream uint32
	seq    uint32
	out    atomic.Int64
	ended  bool
}

// .
type engine struct {
	mu sync.Mutex
	s  *aiiosdk.Session

	sessionID string
	lifecycle string
	seq       int64
	stateSeq  int64
	events    int64

	inputState   string
	admittedEnd  int64
	processedEnd int64
	finished     bool
	// .
	// .
	// .
	// .
	completion map[string]any
	// .
	byID map[string]*generation

	synth   *generation
	gens    map[string]bool
	playID  string
	playing bool
	queued  int64

	closeDelay time.Duration
	synthLen   time.Duration
	rate       int
	streams    uint32

	// .
	// .
	// .
	// .
	// .
	audio    *aiiosdk.AudioPair
	audioIn  int64
	audioOut int64
}

func newEngine() *engine {
	return &engine{gens: map[string]bool{}, byID: map[string]*generation{}, inputState: "accepting", synthLen: 200 * time.Millisecond, rate: 16000}
}

// .
// .
// .
func (e *engine) emit(typ string, fields map[string]any) int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.s == nil {
		return 0
	}
	e.seq++
	e.events++
	ev := map[string]any{"type": typ, "session_id": e.sessionID, "sequence": e.seq, "id": fmt.Sprintf("ev-%d", e.events)}
	for k, v := range fields {
		ev[k] = v
	}
	_ = e.s.Emit(ev)
	return e.seq
}

// .
// .
// .
// .
// .
// .
func (e *engine) admit(c *aiiosdk.Control) {
	result, err := e.control(c.Session, c.Op, c.Args)
	c.Answer(result, err)
}

func (e *engine) control(s *aiiosdk.Session, op string, args aiiosdk.Object) (any, error) {
	e.mu.Lock()
	e.s = s
	first := e.audio == nil
	e.mu.Unlock()
	if first {
		if pair, err := s.Audio(); err == nil {
			e.mu.Lock()
			e.audio = pair
			e.mu.Unlock()
			go e.echoAudio(pair)
		}
	}
	switch op {
	case aiiosdk.OpSessionOpen:
		return e.open(args)
	case aiiosdk.OpSessionSynthesize:
		return e.synthesize(args)
	case aiiosdk.OpSessionCancelSynth:
		return e.cancel(args)
	case aiiosdk.OpSessionStopPlayback:
		return e.stop(args)
	case aiiosdk.OpSessionFinishInput:
		return e.finishInput(args)
	case aiiosdk.OpSessionClose:
		return e.close(args)
	case aiiosdk.OpSessionStatus:
		return e.status(), nil
	case aiiosdk.OpSessionPlaybackReport:
		return e.playbackReport(args)
	}
	return nil, fmt.Errorf("voice-skel: unknown control %q", op)
}

func (e *engine) open(args aiiosdk.Object) (any, error) {
	id, _ := args.String("session_id")
	if id == "" {
		return nil, fmt.Errorf("open needs a session_id")
	}
	e.mu.Lock()
	if e.lifecycle != "" && e.lifecycle != "closed" {
		e.mu.Unlock()
		return nil, fmt.Errorf("a session is already open (%s): one engine, one session", e.sessionID)
	}
	e.sessionID, e.lifecycle, e.seq, e.stateSeq = id, "open", 0, 0
	e.inputState, e.admittedEnd, e.processedEnd, e.finished = "accepting", 0, 0, false
	e.completion = nil
	e.byID = map[string]*generation{}
	e.synth, e.playID, e.playing, e.queued = nil, "", false, 0
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	e.rate = 16000
	if raw := args.Raw("audio"); raw != nil {
		au := aiiosdk.Object(raw)
		in := au
		if sub := au.Raw("input"); sub != nil {
			in = aiiosdk.Object(sub)
		}
		if ch, ok := in.Int("channels"); ok && ch != 1 {
			return nil, fmt.Errorf("open refused: this engine speaks mono only, not %d channels", ch)
		}
		if r, ok := in.Int("rate"); ok {
			if r < 8000 || r > 48000 {
				return nil, fmt.Errorf("open refused: this engine speaks 8 to 48 kHz, not %d Hz", r)
			}
			e.rate = int(r)
		}
	}
	if r, ok := args.Int("test_engine_rate"); ok && r > 0 {
		e.rate = int(r)
	}
	e.closeDelay, e.synthLen = 0, 200*time.Millisecond
	if ms, ok := args.Int("test_close_delay_ms"); ok {
		e.closeDelay = time.Duration(ms) * time.Millisecond
	}
	if ms, ok := args.Int("test_synth_ms"); ok {
		e.synthLen = time.Duration(ms) * time.Millisecond
	}
	tele, _ := args.Int("test_telemetry_burst")
	crit, _ := args.Int("test_critical_burst")
	die, _ := args.Int("test_die_after_ms")
	e.mu.Unlock()
	if die > 0 {
		// .
		// .
		go func() {
			time.Sleep(time.Duration(die) * time.Millisecond)
			os.Exit(3)
		}()
	}
	go func() {
		// .
		// .
		// .
		// .
		e.emit("session_ready", nil)
		for i := int64(0); i < tele; i++ {
			e.emit("vad_probability", map[string]any{"value": 0.5})
		}
		for i := int64(0); i < crit; i++ {
			e.emit("transcript_final", map[string]any{"text": fmt.Sprintf("utterance %d", i+1), "speaker": "speaker-1"})
		}
	}()
	fmtOut := map[string]any{"rate": e.rate, "channels": 1}
	return map[string]any{"session_id": id, "accepted": true, "state": "opening",
		"audio": map[string]any{"input": fmtOut, "output": fmtOut}}, nil
}

func (e *engine) synthesize(args aiiosdk.Object) (any, error) {
	sid, _ := args.String("synthesis_id")
	text, _ := args.String("text")
	e.mu.Lock()
	if e.lifecycle != "open" {
		e.mu.Unlock()
		return nil, fmt.Errorf("synthesize refused: the session is %s — new work is refused after close", e.lifecycle)
	}
	if sid == "" || e.gens[sid] {
		e.mu.Unlock()
		return nil, fmt.Errorf("synthesis id %q refused: ids are never reused", sid)
	}
	e.gens[sid] = true
	e.streams++
	g := &generation{id: sid, done: make(chan struct{}), stream: e.streams}
	e.synth, e.byID[sid] = g, g
	// .
	// .
	e.playID, e.playing, e.queued = sid, true, int64(len(text))*160
	dur, pair, rate := e.synthLen, e.audio, e.rate
	e.mu.Unlock()
	// .
	// .
	e.emit("synthesis_start", map[string]any{"synthesis_id": sid, "output_stream": g.stream})
	go e.run(g, dur, pair, rate)
	return map[string]any{"synthesis_id": sid, "accepted": true, "state": "generating"}, nil
}

// .
// .
// .
func (e *engine) run(g *generation, dur time.Duration, pair *aiiosdk.AudioPair, rate int) {
	defer close(g.done)
	end := time.Now().Add(dur)
	for time.Now().Before(end) {
		e.mu.Lock()
		c, stopped := g.cancelled, g.stopped
		e.mu.Unlock()
		if c {
			break
		}
		// .
		// .
		if stopped {
			endStream(pair, g)
		} else {
			speak(pair, g, rate)
		}
		time.Sleep(5 * time.Millisecond)
	}
	endStream(pair, g)
	e.mu.Lock()
	cancelled, stopped := g.cancelled, g.stopped
	if e.synth == g {
		e.synth = nil
	}
	wasPlaying := e.playing && e.playID == g.id
	e.playing, e.queued = false, 0
	e.mu.Unlock()
	terminal := map[string]any{"synthesis_id": g.id, "output_stream": g.stream, "delivered_samples": g.out.Load(), "playback_verified": false}
	if cancelled {
		e.emit("synthesis_cancelled", terminal)
	} else {
		e.emit("synthesis_end", terminal)
	}
	if wasPlaying && !stopped {
		e.emit("playback_end", map[string]any{"synthesis_id": g.id})
	}
}

func (e *engine) cancel(args aiiosdk.Object) (any, error) {
	sid, _ := args.String("synthesis_id")
	e.mu.Lock()
	defer e.mu.Unlock()
	if sid == "" {
		// .
		// .
		if e.synth == nil {
			return map[string]any{"accepted": true, "already_resolved": true}, nil
		}
		sid = e.synth.id
	}
	if !e.gens[sid] {
		return nil, fmt.Errorf("cancel refused: unknown synthesis %q", sid)
	}
	resolved := e.synth == nil || e.synth.id != sid
	if !resolved {
		e.synth.cancelled = true
	}
	// .
	// .
	// .
	return map[string]any{"synthesis_id": sid, "accepted": true, "already_resolved": resolved}, nil
}

func (e *engine) stop(args aiiosdk.Object) (any, error) {
	sid, _ := args.String("synthesis_id")
	reason, _ := args.String("reason")
	e.mu.Lock()
	wasPlaying := e.playing && (sid == "" || e.playID == sid)
	if wasPlaying {
		e.playing, e.queued = false, 0
	}
	if e.synth != nil && (sid == "" || e.synth.id == sid) {
		e.synth.stopped = true
	}
	e.mu.Unlock()
	if wasPlaying {
		// .
		// .
		e.emit("playback_stop", map[string]any{"synthesis_id": sid, "reason": reason})
	}
	return map[string]any{"accepted": true, "was_playing": wasPlaying}, nil
}

func (e *engine) finishInput(args aiiosdk.Object) (any, error) {
	stream, _ := args.String("stream_id")
	end, ok := args.Int("end_sample")
	if !ok || end < 0 {
		return nil, fmt.Errorf("finish_input needs an end_sample (the exclusive cutoff)")
	}
	e.mu.Lock()
	if e.lifecycle != "open" {
		e.mu.Unlock()
		return nil, fmt.Errorf("finish_input refused: the session is %s", e.lifecycle)
	}
	if e.finished {
		// .
		// .
		// .
		e.mu.Unlock()
		if end != e.admittedEnd {
			return nil, fmt.Errorf("finish_input refused: the cutoff is fixed at %d and cannot change to %d", e.admittedEnd, end)
		}
		return map[string]any{"stream_id": stream, "end_sample": end, "accepted": true}, nil
	}
	e.admittedEnd, e.inputState, e.finished = end, "finishing", true
	e.mu.Unlock()
	go func() {
		// .
		// .
		// .
		// .
		time.Sleep(20 * time.Millisecond)
		e.mu.Lock()
		e.processedEnd, e.inputState = end, "finished"
		e.mu.Unlock()
		e.emit("transcript_final", map[string]any{"stream_id": stream, "end_sample": end, "text": "(the tail, finalized)", "speaker": "speaker-1"})
		// .
		// .
		// .
		// .
		done := map[string]any{"stream_id": stream, "end_sample": end, "processed_end_sample": end}
		seq := e.emit("input_finished", done)
		e.mu.Lock()
		done["sequence"] = seq
		e.completion = done
		e.mu.Unlock()
	}()
	return map[string]any{"stream_id": stream, "end_sample": end, "accepted": true}, nil
}

// .
// .
// .
// .
// .
// .
func (e *engine) playbackReport(args aiiosdk.Object) (any, error) {
	var r struct {
		SynthesisID  string `json:"synthesis_id"`
		OutputStream *int64 `json:"output_stream"`
		Rendered     *int64 `json:"rendered_samples"`
		Terminal     *bool  `json:"terminal"`
	}
	if err := json.Unmarshal([]byte(args), &r); err != nil || r.OutputStream == nil || r.Rendered == nil || r.Terminal == nil {
		return nil, fmt.Errorf("playback_report needs synthesis_id, an integer output_stream and rendered_samples, and a boolean terminal")
	}
	e.mu.Lock()
	if e.lifecycle != "open" && e.lifecycle != "draining" {
		e.mu.Unlock()
		return nil, fmt.Errorf("playback_report refused: the session is %s", e.lifecycle)
	}
	g := e.byID[r.SynthesisID]
	if g == nil || *r.OutputStream != int64(g.stream) || *r.Rendered < g.reported || *r.Rendered > g.out.Load() {
		e.mu.Unlock()
		return nil, fmt.Errorf("playback_report refused: foreign or impossible render progress")
	}
	admission := map[string]any{"accepted": true, "synthesis_id": g.id, "output_stream": g.stream, "rendered_samples": *r.Rendered, "terminal": *r.Terminal}
	if g.resolved {
		e.mu.Unlock()
		if *r.Terminal && *r.Rendered == g.reported {
			return admission, nil
		}
		return nil, fmt.Errorf("playback_report refused: terminal render evidence is immutable")
	}
	fenced := g.stopped || g.cancelled
	if *r.Terminal && !g.ended && !fenced {
		e.mu.Unlock()
		return nil, fmt.Errorf("playback_report refused: output still live")
	}
	delivered := g.out.Load()
	if *r.Terminal && !fenced && *r.Rendered != delivered {
		e.mu.Unlock()
		return nil, fmt.Errorf("playback_report refused: complete tail not rendered")
	}
	if *r.Rendered == g.reported && !*r.Terminal {
		e.mu.Unlock()
		return admission, nil
	}
	g.reported, g.resolved = *r.Rendered, *r.Terminal
	outcome, discarded := "progress", int64(0)
	if *r.Terminal {
		outcome, discarded = "drained", delivered-g.reported
		if fenced {
			outcome = "stopped"
		}
	}
	rate := e.rate
	e.mu.Unlock()
	e.emit("playback_observation", map[string]any{
		"synthesis_id": g.id, "output_stream": g.stream, "sample_rate": rate,
		"rendered_samples": g.reported, "delivered_samples": delivered, "discarded_samples": discarded,
		"terminal": *r.Terminal, "outcome": outcome, "evidence": "host_validated_client_report", "playback_verified": false,
	})
	return admission, nil
}

func (e *engine) close(args aiiosdk.Object) (any, error) {
	mode, _ := args.String("mode")
	reason, _ := args.String("reason")
	e.mu.Lock()
	if e.lifecycle != "open" {
		e.mu.Unlock()
		return nil, fmt.Errorf("close refused: the session is %s", e.lifecycle)
	}
	switch mode {
	case "drain":
		if !e.finished {
			e.mu.Unlock()
			return nil, fmt.Errorf("drain close refused: no finish_input cutoff was fixed — a drain without a boundary is an abort that lies")
		}
		e.lifecycle = "draining"
	case "abort":
		e.lifecycle = "aborting"
		if e.synth != nil {
			e.synth.cancelled, e.synth.stopped = true, true
		}
		e.playing, e.queued = false, 0
	default:
		e.mu.Unlock()
		return nil, fmt.Errorf("close needs mode drain or abort, not %q", mode)
	}
	g, delay, state := e.synth, e.closeDelay, e.lifecycle
	e.mu.Unlock()
	go func() {
		// .
		// .
		// .
		// .
		if g != nil {
			<-g.done
		}
		if mode == "drain" {
			for {
				e.mu.Lock()
				tail := e.inputState == "finished"
				e.mu.Unlock()
				if tail {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		time.Sleep(delay)
		if mode == "drain" {
			// .
			// .
			// .
			// .
			for {
				e.mu.Lock()
				pending := 0
				for _, g := range e.byID {
					if !g.resolved {
						pending++
					}
				}
				e.mu.Unlock()
				if pending == 0 {
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
		e.mu.Lock()
		e.lifecycle = "closed"
		e.mu.Unlock()
		if mode == "drain" {
			e.emit("session_end", map[string]any{"reason": reason})
		} else {
			e.emit("cancellation", map[string]any{"reason": reason})
		}
	}()
	return map[string]any{"accepted": true, "state": state}, nil
}

// .
// .
// .
func speak(pair *aiiosdk.AudioPair, g *generation, rate int) {
	if pair == nil || g.ended {
		return
	}
	n := rate / 200
	pcm := make([]byte, n*2)
	for i := 0; i < n; i++ {
		v := int16(6000 * math.Sin(2*math.Pi*440*float64(g.out.Load()+int64(i))/float64(rate)))
		binary.LittleEndian.PutUint16(pcm[2*i:], uint16(v))
	}
	g.seq++
	if err := pair.Write(aiiosdk.AudioFrame{Kind: aiiosdk.AudioPCM, Stream: g.stream, Seq: g.seq, Start: g.out.Load(), PCM: pcm}); err != nil {
		g.ended = true
		return
	}
	g.out.Add(int64(n))
}

// .
// .
func endStream(pair *aiiosdk.AudioPair, g *generation) {
	if pair == nil {
		g.ended = true
		return
	}
	if g.ended {
		return
	}
	g.ended = true
	g.seq++
	_ = pair.Write(aiiosdk.AudioFrame{Kind: aiiosdk.AudioEnd, Stream: g.stream, Seq: g.seq, Start: g.out.Load()})
}

// .
// .
// .
// .
// .
// .
func (e *engine) echoAudio(pair *aiiosdk.AudioPair) {
	var inStream, outStream uint32
	first := true
	for {
		fr, err := pair.Read()
		if err != nil {
			return
		}
		e.mu.Lock()
		switch fr.Kind {
		case aiiosdk.AudioPCM:
			e.audioIn = fr.Start + fr.Samples(1)
		case aiiosdk.AudioEnd, aiiosdk.AudioDiscontinuity:
			e.audioIn = fr.Start
		}
		if first || fr.Stream != inStream {
			first, inStream = false, fr.Stream
			e.streams++
			outStream = e.streams
		}
		e.mu.Unlock()
		fr.Stream = outStream
		if err := pair.Write(fr); err != nil {
			return
		}
		e.mu.Lock()
		e.audioOut = e.audioIn
		e.mu.Unlock()
	}
}

// .
// .
func (e *engine) status() any {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stateSeq++
	synthID, synthState := "", "idle"
	if e.synth != nil {
		synthID, synthState = e.synth.id, "generating"
		if e.synth.cancelled {
			synthState = "cancelling"
		}
	}
	playState := "idle"
	if e.playing {
		playState = "playing"
	}
	return map[string]any{
		"session_id": e.sessionID, "state_sequence": e.stateSeq, "lifecycle": e.lifecycle,
		"input":            map[string]any{"state": e.inputState, "admitted_end_sample": e.admittedEnd, "processed_end_sample": e.processedEnd, "received_end_sample": e.audioIn},
		"input_completion": e.completion,
		"audio":            map[string]any{"input_end_sample": e.audioIn, "output_end_sample": e.audioOut},
		"recognition":      map[string]any{"utterance_open": false, "finalization_pending": e.inputState == "finishing"},
		"synthesis":        map[string]any{"synthesis_id": synthID, "state": synthState},
		"playback":         map[string]any{"synthesis_id": e.playID, "state": playState, "queued_samples": e.queued},
	}
}

func main() {
	p := aiiosdk.New("com.example.voice-skel").DeclareSession()
	e := newEngine()
	// .
	// .
	// .
	if err := p.ServeSession(e.admit); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "voice-skel: %v\n", err)
		os.Exit(1)
	}
}
