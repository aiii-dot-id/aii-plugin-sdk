// sessionchild is a resident engine for `aiisdk test`'s own tests of
// session checks: it opens only a check's session (the open says
// "check": true and carries the check's handles), says session_ready,
// hears nothing of its input but drains it, at finish_input says a final
// transcript and input_finished, speaks half a second of a tone for a
// synthesis, and ends the session at an abort. SESSIONCHILD_MODE is its
// misbehaviour: wrong (the transcript lacks a word), rate8k (it answers
// its input at 8 kHz), noend (an abort is admitted and the session never
// ended), hostcalls (it asks the host fs.read, kv.put and settings.get,
// and says what came back as its transcript), failfirst (it fails the
// session at the first frame of input it reads, once), deaf (it never
// reads its input, and fails each session a second after it is ready).
// Every input frame it reads is written to stderr as "sessionchild:
// heard", in the order read. A generic org.example plugin; nothing else.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

type engine struct {
	mode    string
	mu      sync.Mutex
	session string
	seq     int
	streams uint32
	pair    *aiiosdk.AudioPair
	failed  bool // failfirst: the one failure was said
}

func (e *engine) emit(s *aiiosdk.Session, typ string, fields map[string]any) {
	e.mu.Lock()
	e.seq++
	ev := map[string]any{"type": typ, "session_id": e.session, "sequence": e.seq}
	e.mu.Unlock()
	for k, v := range fields {
		ev[k] = v
	}
	_ = s.Emit(ev)
}

func (e *engine) admit(c *aiiosdk.Control) {
	s, args := c.Session, c.Args
	if e.pair == nil {
		if pair, err := s.Audio(); err == nil {
			e.pair = pair
			if e.mode != "deaf" {
				go func() {
					for {
						fr, err := pair.Read()
						if err != nil {
							return
						}
						e.heard(s, fr)
					}
				}()
			}
		}
	}
	switch c.Op {
	case aiiosdk.OpSessionOpen:
		sid, _ := args.String("session_id")
		out, _ := args.String("output_handle")
		fmt.Fprintf(os.Stderr, "sessionchild: open %s\n", string(args))
		if strings.TrimSpace(string(args.Raw("check"))) != "true" || out != sid+"-out" {
			c.Answer(nil, errors.New("this engine opens only a check's session: check true, its handles"))
			return
		}
		topo, err := aiiosdk.ParseSessionAudio(args)
		if err != nil {
			c.Answer(nil, err)
			return
		}
		e.mu.Lock()
		e.session, e.seq = sid, 0
		e.mu.Unlock()
		in := topo.Input
		if e.mode == "rate8k" {
			in = aiiosdk.SessionFormat{Rate: 8000, Channels: 1}
		}
		c.Answer(map[string]any{"session_id": sid, "accepted": true, "audio": topo.Admission(in, aiiosdk.SessionFormat{Rate: 16000, Channels: 1})}, nil)
		go func() {
			e.emit(s, "session_ready", nil)
			switch e.mode {
			case "hostcalls":
				e.emit(s, "transcript_final", map[string]any{"text": askTheHost(s)})
			case "deaf":
				time.Sleep(time.Second)
				e.emit(s, "failure", map[string]any{"reason": "this engine hears nothing", "reason_code": "ENGINE_FAULT"})
			}
		}()
	case aiiosdk.OpSessionFinishInput:
		stream, _ := args.String("stream_id")
		end, _ := args.Int("end_sample")
		c.Answer(map[string]any{"accepted": true}, nil)
		go func() {
			text := "Cobalt lantern, seventeen."
			if e.mode == "wrong" {
				text = "cobalt seventeen"
			}
			e.emit(s, "transcript_final", map[string]any{"text": text})
			e.emit(s, "input_finished", map[string]any{"stream_id": stream, "end_sample": end, "processed_end_sample": end})
		}()
	case aiiosdk.OpSessionSynthesize:
		synth, _ := args.String("synthesis_id")
		e.mu.Lock()
		e.streams++
		stream := 5000 + e.streams
		e.mu.Unlock()
		c.Answer(map[string]any{"synthesis_id": synth, "accepted": true}, nil)
		go func() {
			e.emit(s, "synthesis_start", map[string]any{"synthesis_id": synth, "output_stream": stream})
			n := e.speak(stream)
			e.emit(s, "synthesis_end", map[string]any{"synthesis_id": synth, "output_stream": stream, "delivered_samples": n})
		}()
	case aiiosdk.OpSessionClose:
		reason, _ := args.String("reason")
		c.Answer(map[string]any{"accepted": true}, nil)
		if e.mode != "noend" {
			go e.emit(s, "cancellation", map[string]any{"reason": reason})
		}
	case aiiosdk.OpSessionStatus:
		e.mu.Lock()
		sid := e.session
		e.mu.Unlock()
		c.Answer(map[string]any{"session_id": sid, "state_sequence": 1, "lifecycle": "open"}, nil)
	default:
		c.Answer(map[string]any{"accepted": true}, nil)
	}
}

// heard writes one input frame to stderr as it was read — its stream,
// kind, sequence, start sample and first sample — and, in failfirst, fails
// the session at the first frame of samples.
func (e *engine) heard(s *aiiosdk.Session, fr aiiosdk.AudioFrame) {
	first := 0
	if len(fr.PCM) >= 2 {
		first = int(int16(binary.LittleEndian.Uint16(fr.PCM)))
	}
	fmt.Fprintf(os.Stderr, "sessionchild: heard stream=%d kind=%d seq=%d start=%d first=%d\n", fr.Stream, fr.Kind, fr.Seq, fr.Start, first)
	e.mu.Lock()
	fail := e.mode == "failfirst" && !e.failed && fr.Kind == aiiosdk.AudioPCM
	e.failed = e.failed || fail
	e.mu.Unlock()
	if fail {
		e.emit(s, "failure", map[string]any{"reason": "this engine fails at its first frame", "reason_code": "ENGINE_FAULT"})
	}
}

// speak writes half a second of a 440 Hz tone at 16 kHz under stream, and
// its END.
func (e *engine) speak(stream uint32) int64 {
	const n, per = 8000, 800
	seq := uint32(0)
	for at := 0; at < n; at += per {
		pcm := make([]byte, 2*per)
		for i := 0; i < per; i++ {
			binary.LittleEndian.PutUint16(pcm[2*i:], uint16(int16(math.Round(6000*math.Sin(2*math.Pi*440*float64(at+i)/16000)))))
		}
		seq++
		if e.pair == nil || e.pair.Write(aiiosdk.AudioFrame{Kind: aiiosdk.AudioPCM, Stream: stream, Seq: seq, Start: int64(at), PCM: pcm}) != nil {
			return int64(at)
		}
	}
	_ = e.pair.Write(aiiosdk.AudioFrame{Kind: aiiosdk.AudioEnd, Stream: stream, Seq: seq + 1, Start: n})
	return n
}

// askTheHost asks fs.read, kv.put and settings.get and says, one line
// each, what came back — "refused" only when the host's refusal, read by
// its reason code, came at once.
func askTheHost(s *aiiosdk.Session) string {
	var lines []string
	for _, op := range []string{"fs.read", "kv.put", "settings.get"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		start := time.Now()
		got, err := s.HostCallTo(ctx, op, map[string]any{"key": "probe", "path": "probe.json"}, map[string]any{"value": "x"})
		cancel()
		d, refused := aiiosdk.AsDenied(err)
		switch {
		case refused && d.ReasonCode == "VALIDATION_DENY" && time.Since(start) < 100*time.Millisecond:
			lines = append(lines, op+" refused VALIDATION_DENY")
		case err != nil:
			lines = append(lines, op+" failed: "+err.Error())
		default:
			lines = append(lines, op+" answered "+string(got))
		}
	}
	return strings.Join(lines, "; ")
}

func main() {
	e := &engine{mode: os.Getenv("SESSIONCHILD_MODE")}
	p := aiiosdk.New("org.example.sessionchild").DeclareSession()
	if err := p.ServeSessionReady("sessionchild", aiiosdk.ReadyReport{ModelsLoaded: 1, Accelerator: "cpu", ProbeMS: 1}, e.admit); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "sessionchild: %v\n", err)
		os.Exit(1)
	}
}
