package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/internal/workerdrive"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

const (
	sessionControlBound = 5 * time.Second

	sessionStopBound = 5 * time.Second

	checkNotRunHere = "not_run_here"
)

var checkSpeaker = map[string]interface{}{"rate": 48000, "channels": 1}

type sessionDriver struct {
	w       *workerdrive.SessionWorker
	streams uint32
	out     *outputTally

	gone bool
}

func newSessionDriver(w *workerdrive.SessionWorker) *sessionDriver {
	d := &sessionDriver{w: w, out: newOutputTally()}
	go d.out.read(w)
	return d
}

type outputTally struct {
	mu      sync.Mutex
	streams map[uint32]*streamTally
	changed chan struct{}
}

type streamTally struct {
	end   int64
	ended bool
	level aiiospkg.Level
}

func newOutputTally() *outputTally {
	return &outputTally{streams: map[uint32]*streamTally{}, changed: make(chan struct{}, 1)}
}

func (o *outputTally) read(w *workerdrive.SessionWorker) {
	for {
		fr, err := w.ReadAudio()
		if err != nil {
			return
		}
		o.mu.Lock()
		st := o.streams[fr.Stream]
		if st == nil {
			st = &streamTally{}
			o.streams[fr.Stream] = st
		}
		switch fr.Kind {
		case sdk.AudioPCM:
			st.level.Add(fr.PCM)
			st.end = fr.Start + int64(len(fr.PCM)/2)
		case sdk.AudioEnd:
			st.end, st.ended = fr.Start, true
		}
		o.mu.Unlock()
		if fr.Kind == sdk.AudioEnd {
			select {
			case o.changed <- struct{}{}:
			default:
			}
		}
	}
}

func (o *outputTally) stream(n uint32) (end int64, ended bool, level float64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	st := o.streams[n]
	if st == nil {
		return 0, false, math.Inf(-1)
	}
	return st.end, st.ended, st.level.DBFS()
}

type sessionEvent struct {
	Type         string `json:"type"`
	SessionID    string `json:"session_id"`
	Text         string `json:"text"`
	SynthesisID  string `json:"synthesis_id"`
	OutputStream *int64 `json:"output_stream"`
	Reason       string `json:"reason"`
	ReasonCode   string `json:"reason_code"`
	RetryMayPass bool   `json:"retry_may_pass"`
}

func (d *sessionDriver) run(n int, c aiiospkg.Check) checkResult {
	r := checkResult{name: c.Name}
	if d.gone {
		r.outcome, r.words = checkUnanswered, "not asked: the engine never ended an earlier check's session"
		return r
	}
	id := fmt.Sprintf("check-%d", n)
	args := map[string]interface{}{"session_id": id, "check": true, "output_handle": id + "-out"}
	var rec aiiospkg.Recording
	if c.Audio != "" {
		var why string
		if rec, why = aiiospkg.ReadRecording(c.WAV); why != "" {
			r.outcome, r.words = checkUnresolved, "input.audio: "+c.Audio+" "+why
			return r
		}
		d.streams++
		args["input_handle"] = id + "-in"
		args["audio"] = map[string]interface{}{"format": "s16le", "input": map[string]interface{}{"rate": rec.Rate, "channels": rec.Channels, "stream": d.streams}, "output": checkSpeaker}
	} else {
		args["audio"] = map[string]interface{}{"format": "s16le", "input": nil, "output": checkSpeaker}
	}
	ans := aiiospkg.SessionAnswer{Level: math.Inf(-1)}
	if c.Text != "" {
		ans.SynthesisID = id + "-s"
	}
	sent := time.Now()
	admission, err := d.w.Control("speech.session.open", args, sessionControlBound)
	var refused *workerdrive.ControlRefusedError
	switch {
	case errors.As(err, &refused):
		ans.Refused = &aiiospkg.SessionRefusal{Control: "speech.session.open", Words: string(refused.Answer)}
		r.outcome, r.words = aiiospkg.JudgeSession(c.Expect.Events, c.Text != "", ans)
		r.took = time.Since(sent)
		return r
	case errors.Is(err, workerdrive.ErrNoAnswer):
		r.outcome, r.words = checkTimedOut, "the open's admission was unknown at its bound"
		return d.end(r, id)
	case err != nil:
		r.outcome, r.words = checkUnanswered, "not answered: "+err.Error()
		return r
	}
	in, out, ferr := engineFormats(admission, c.Audio != "")
	switch {
	case ferr != "":
		r.outcome, r.words = checkWrong, "the engine admitted the session with audio formats the host cannot carry: "+ferr
		return d.end(r, id)
	case c.Audio != "" && (in.rate != rec.Rate || in.channels != rec.Channels):
		r.outcome = checkNotRunHere
		r.words = fmt.Sprintf("not run here: your engine answered its input at %d Hz, %d channel(s), and the recording is %d Hz, %d channel(s); a host converts between them, this kit does not — record the check at %d Hz, %d channel(s)",
			in.rate, in.channels, rec.Rate, rec.Channels, in.rate, in.channels)
		return d.end(r, id)
	}
	stream, fail := d.listen(id, c, rec, &ans, sent)
	r.took = time.Since(sent)
	if ans.SynthesisID != "" && stream >= 0 {
		end, _, level := d.out.stream(uint32(stream))
		ans.Samples, ans.Rate, ans.Level = end, out.rate, level
	}
	switch {
	case fail == nil:
		r.outcome, r.words = aiiospkg.JudgeSession(c.Expect.Events, c.Text != "", ans)
	case errors.Is(fail, workerdrive.ErrNoAnswer):
		r.outcome, r.words = checkTimedOut, "a control's admission was unknown at its bound: "+fail.Error()
	default:
		r.outcome, r.words = checkUnanswered, "not answered: "+fail.Error()
	}
	if r.outcome == checkPassed && c.Within > 0 && r.took > c.Within {
		r.outcome = checkTimedOut
		r.words = fmt.Sprintf("answered as you say in %d ms, past its bound of %d ms", r.took.Milliseconds(), c.Within.Milliseconds())
	}
	return d.end(r, id)
}

func (d *sessionDriver) listen(id string, c aiiospkg.Check, rec aiiospkg.Recording, ans *aiiospkg.SessionAnswer, sent time.Time) (stream int64, fail error) {
	stream = -1
	deadline := time.NewTimer(time.Until(sent.Add(c.Within)))
	defer deadline.Stop()
	var sentAll <-chan struct{}
	started, synthEnded := false, false
	done := func() bool {
		if c.Audio != "" || !synthEnded || stream < 0 {
			return false
		}
		_, ended, _ := d.out.stream(uint32(stream))
		return ended
	}
	for {
		select {
		case raw, ok := <-d.w.Events():
			if !ok {
				return stream, errors.New("the session's lane ended before its answer was complete")
			}
			var ev sessionEvent
			if err := json.Unmarshal(raw, &ev); err != nil {
				ans.Events = append(ans.Events, aiiospkg.SessionEvent{Type: eventTypeOf(raw), Withheld: true})
				continue
			}
			if ev.SessionID != "" && ev.SessionID != id {
				continue
			}
			if ev.Type == "vad_probability" || ev.Type == "transcript_partial" {
				continue
			}
			ans.Events = append(ans.Events, aiiospkg.SessionEvent{Type: ev.Type, Text: ev.Text, SynthesisID: ev.SynthesisID,
				Reason: ev.Reason, ReasonCode: ev.ReasonCode, RetryMayPass: ev.RetryMayPass})
			switch {
			case ev.Type == "session_ready" && !started:
				started = true
				if c.Audio != "" {
					ch := make(chan struct{})
					sentAll = ch
					go d.pace(rec, ch)
					break
				}
				_, err := d.w.Control("speech.session.synthesize", map[string]interface{}{"session_id": id, "synthesis_id": ans.SynthesisID, "text": c.Text}, sessionControlBound)
				var refused *workerdrive.ControlRefusedError
				if errors.As(err, &refused) {
					ans.Refused = &aiiospkg.SessionRefusal{Control: "speech.session.synthesize", Words: string(refused.Answer)}
					return stream, nil
				}
				if err != nil {
					return stream, err
				}
			case ev.Type == "input_finished" && c.Audio != "":
				ans.Complete = true
				return stream, nil
			case (ev.Type == "synthesis_start" || ev.Type == "synthesis_end") && ev.SynthesisID == ans.SynthesisID && ans.SynthesisID != "":
				if ev.OutputStream != nil && *ev.OutputStream >= 0 && *ev.OutputStream <= math.MaxUint32 && stream < 0 {
					stream = *ev.OutputStream
				}
				synthEnded = synthEnded || ev.Type == "synthesis_end"
			case ev.Type == "failure" || ev.Type == "session_end" || (ev.Type == "cancellation" && ev.SynthesisID == "") ||
				((ev.Type == "synthesis_cancelled" || ev.Type == "cancellation") && ev.SynthesisID != "" && ev.SynthesisID == ans.SynthesisID):
				return stream, nil
			}
			if done() {
				ans.Complete = true
				return stream, nil
			}
		case <-sentAll:
			sentAll = nil
			_, err := d.w.Control("speech.session.finish_input", map[string]interface{}{"session_id": id, "stream_id": id + "-in", "end_sample": len(rec.PCM) / (2 * rec.Channels)}, sessionControlBound)
			var refused *workerdrive.ControlRefusedError
			if errors.As(err, &refused) {
				ans.Refused = &aiiospkg.SessionRefusal{Control: "speech.session.finish_input", Words: string(refused.Answer)}
				return stream, nil
			}
			if err != nil {
				return stream, err
			}
		case <-d.out.changed:
			if done() {
				ans.Complete = true
				return stream, nil
			}
		case <-deadline.C:
			return stream, nil
		}
	}
}

func (d *sessionDriver) pace(rec aiiospkg.Recording, sent chan<- struct{}) {
	defer close(sent)
	frame := 2 * rec.Channels
	per := rec.Rate / 50 * frame
	origin := time.Now()
	seq := uint32(0)
	var at int64
	for off := 0; off < len(rec.PCM); off += per {
		end := off + per
		if end > len(rec.PCM) {
			end = len(rec.PCM)
		}
		time.Sleep(time.Until(origin.Add(time.Duration(at) * time.Second / time.Duration(rec.Rate))))
		seq++
		if d.w.WriteAudio(sdk.AudioFrame{Kind: sdk.AudioPCM, Stream: d.streams, Seq: seq, Start: at, PCM: rec.PCM[off:end]}) != nil {
			return
		}
		at += int64((end - off) / frame)
	}
	time.Sleep(time.Until(origin.Add(time.Duration(at) * time.Second / time.Duration(rec.Rate))))
	seq++
	_ = d.w.WriteAudio(sdk.AudioFrame{Kind: sdk.AudioEnd, Stream: d.streams, Seq: seq, Start: at})
}

func (d *sessionDriver) end(r checkResult, id string) checkResult {
	_, _ = d.w.Control("speech.session.close", map[string]interface{}{"session_id": id, "mode": "abort", "reason": "the check is over"}, sessionControlBound)
	wait := time.NewTimer(sessionStopBound)
	defer wait.Stop()
	for {
		select {
		case raw, ok := <-d.w.Events():
			if !ok {
				return r
			}
			var ev sessionEvent
			_ = json.Unmarshal(raw, &ev)
			if (ev.SessionID == "" || ev.SessionID == id) && (ev.Type == "session_end" || ev.Type == "failure" || (ev.Type == "cancellation" && ev.SynthesisID == "")) {
				return r
			}
		case <-wait.C:
			snap, err := d.w.Control("speech.session.status", map[string]interface{}{"session_id": id}, sessionControlBound)
			var s struct {
				Lifecycle string `json:"lifecycle"`
			}
			if err == nil && json.Unmarshal(snap, &s) == nil && (s.Lifecycle == "closed" || s.Lifecycle == "failed") {
				return r
			}
			d.gone = true
			if r.outcome != checkWrong {
				r.outcome = checkTimedOut
				if r.words != "" {
					r.words += "; "
				}
				r.words += "the engine did not end the check's session after its abort"
			}
			return r
		}
	}
}

type engineAudio struct{ rate, channels int }

func engineFormats(admission json.RawMessage, wantInput bool) (in, out engineAudio, why string) {
	var res struct {
		Audio map[string]json.RawMessage `json:"audio"`
	}
	if len(admission) == 0 || json.Unmarshal(admission, &res) != nil || res.Audio == nil {
		return in, out, "the admission names no audio formats"
	}
	if raw, ok := res.Audio["format"]; ok {
		var enc string
		if json.Unmarshal(raw, &enc) != nil || enc != "s16le" {
			return in, out, "audio.format is not s16le"
		}
	}
	read := func(name string) (engineAudio, bool, string) {
		raw, present := res.Audio[name]
		if !present || string(raw) == "null" {
			return engineAudio{}, present, ""
		}
		var f struct{ Rate, Channels int }
		if json.Unmarshal(raw, &f) != nil || f.Rate < 8000 || f.Rate > 192000 || f.Channels < 1 || f.Channels > 2 {
			return engineAudio{}, true, "the " + name + " format is not audio"
		}
		return engineAudio{f.Rate, f.Channels}, true, ""
	}
	in, inPresent, why := read("input")
	if why != "" {
		return in, out, why
	}
	if out, _, why = read("output"); why != "" {
		return in, out, why
	}
	switch {
	case out == (engineAudio{}):
		return in, out, "no output format"
	case wantInput && in == (engineAudio{}):
		return in, out, "no input format"
	case !wantInput && (!inPresent || in != (engineAudio{})):
		return in, out, "an output-only open is answered with audio.input present and null"
	}
	return in, out, ""
}

func eventTypeOf(raw json.RawMessage) string {
	var t struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &t)
	return t.Type
}
