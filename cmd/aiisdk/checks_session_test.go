package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/internal/workerdrive"
	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// buildSessionChild builds the resident test engine, a carrier as an
// author builds one.
func buildSessionChild(t *testing.T) string {
	t.Helper()
	gobin := os.Getenv("GO")
	if gobin == "" {
		gobin = "go"
	}
	out := filepath.Join(t.TempDir(), exeName("sessionchild"))
	cmd := exec.Command(gobin, "build", "-o", out, "./testdata/sessionchild")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the session test child: %v\n%s", err, b)
	}
	return out
}

// toneWAV is 0.3 s of a quiet tone at 16 kHz, mono, as a WAV.
func toneWAV() []byte {
	const n = 4800
	data := make([]byte, 2*n)
	for i := 0; i < n; i++ {
		binary.LittleEndian.PutUint16(data[2*i:], uint16(int16(math.Round(3000*math.Sin(2*math.Pi*440*float64(i)/16000)))))
	}
	return wavOf(16000, 1, data)
}

// levelWAV is n sample groups at rate and channels, every sample v, as a
// WAV: a recording told from another by any one of its samples.
func levelWAV(rate, channels, n int, v int16) []byte {
	data := make([]byte, 2*channels*n)
	for i := 0; i < len(data); i += 2 {
		binary.LittleEndian.PutUint16(data[i:], uint16(v))
	}
	return wavOf(rate, channels, data)
}

// wavOf is s16le data at rate and channels, as a WAV.
func wavOf(rate, channels int, data []byte) []byte {
	h := []byte("RIFF")
	h = binary.LittleEndian.AppendUint32(h, uint32(36+len(data)))
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint16(h, uint16(channels))
	h = binary.LittleEndian.AppendUint32(h, uint32(rate))
	h = binary.LittleEndian.AppendUint32(h, uint32(2*channels*rate))
	h = binary.LittleEndian.AppendUint16(h, uint16(2*channels))
	h = binary.LittleEndian.AppendUint16(h, 16)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	return append(h, data...)
}

// sessionChecks reads checks as the host reads them, the recording a member.
func sessionChecks(t *testing.T, file string) []aiiospkg.Check {
	return sessionChecksWith(t, file, map[string][]byte{"checks/tone.wav": toneWAV()})
}

// sessionChecksWith reads checks whose recordings are the members named.
func sessionChecksWith(t *testing.T, file string, members map[string][]byte) []aiiospkg.Check {
	t.Helper()
	v, err := aiiospkg.ParseValidation([]byte(file), func(rel string) ([]byte, bool, error) {
		b, ok := members[rel]
		return b, ok, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return v.Checks
}

const (
	hearsTone   = `{"name":"hears it","sets":["*"],"lane":"session","input":{"audio":"checks/tone.wav"},"expect":{"events":{"order":["session_ready","transcript_final","input_finished"],"transcript_contains":["cobalt lantern"]}},"within_ms":5000}`
	speaksAText = `{"name":"speaks it","sets":["*"],"lane":"session","input":{"text":"The local service is ready."},"expect":{"events":{"order":["session_ready","synthesis_start","synthesis_end"],"audio_at_least":{"ms":400,"rms_dbfs":-30}}},"within_ms":5000}`
)

// runSessionChecks starts the test engine in mode, as a host starts a
// resident engine, and runs checks on it as `aiisdk test` does.
func runSessionChecks(t *testing.T, child, mode, file string) ([]checkResult, *harness, *workerdrive.SessionWorker) {
	t.Helper()
	return runSessionChecksWith(t, child, mode, sessionChecks(t, file))
}

// runSessionChecksWith is runSessionChecks for checks already read.
func runSessionChecksWith(t *testing.T, child, mode string, checks []aiiospkg.Check) ([]checkResult, *harness, *workerdrive.SessionWorker) {
	t.Helper()
	h, err := newHarness(nil, []aiiospkg.SettingDecl{{Key: "model", Type: "string", Title: "Model", Default: "packagedefault"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := []string{"SEV_PLUGIN_SOCKET=stdio:", "SEV_PLUGIN_ID=org.example.sessionchild", "SESSIONCHILD_MODE=" + mode}
	sw, err := workerdrive.StartNativeSession(child, env, 10*time.Second, h.answer)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sw.Close(5 * time.Second) })
	h.checking.Store(true)
	got := runChecks(checks, nil, newSessionDriver(sw).run, 0, time.Now)
	h.checking.Store(false)
	return got, h, sw
}

// A RESIDENT ENGINE'S SESSION CHECKS RUN AS THE HOST RUNS THEM: started
// with its session lane and audio pair, each check opens a session the
// host's way — the engine here opens only a check's session, so a pass
// proves the open said "check": true with the check's handles — sends its
// recording in real time and hears its words, or speaks a text loud and
// long enough, and is judged as the host judges.
func TestASessionCheckRunsAsTheHostRunsIt(t *testing.T) {
	child := buildSessionChild(t)
	got, _, sw := runSessionChecks(t, child, "", `{"checks":[`+hearsTone+`,`+speaksAText+`]}`)
	if len(got) != 2 || got[0].outcome != checkPassed || got[1].outcome != checkPassed {
		t.Fatalf("both checks pass: %+v", got)
	}
	opens := strings.Count(sw.Stderr(), "sessionchild: open ")
	if opens != 2 || !strings.Contains(sw.Stderr(), `"check":true`) || !strings.Contains(sw.Stderr(), `"session_id":"check-1"`) || !strings.Contains(sw.Stderr(), `"session_id":"check-2"`) {
		t.Fatalf("each check's open is the host's, saying check: true: %s", sw.Stderr())
	}
}

// WHAT A HOST JUDGES WRONG, THE KIT JUDGES WRONG; WHAT IT WOULD CONVERT, THE
// KIT DOES NOT RUN; A SESSION NEVER ENDED IS A TIMEOUT, AND NOTHING AFTER IT
// IS ASKED.
func TestASessionCheckIsJudgedNotRunOrTimedOutAsAHostWould(t *testing.T) {
	child := buildSessionChild(t)
	t.Run("a word missing", func(t *testing.T) {
		got, _, _ := runSessionChecks(t, child, "wrong", `{"checks":[`+hearsTone+`]}`)
		if got[0].outcome != checkWrong || !strings.Contains(got[0].words, `lacks "cobalt lantern"`) {
			t.Fatalf("%+v", got)
		}
	})
	t.Run("an engine that hears at another rate", func(t *testing.T) {
		got, _, _ := runSessionChecks(t, child, "rate8k", `{"checks":[`+hearsTone+`,`+speaksAText+`]}`)
		if len(got) != 2 || got[0].outcome != checkNotRunHere || !strings.Contains(got[0].words, "answered its input at 8000 Hz") || got[1].outcome != checkPassed {
			t.Fatalf("%+v", got)
		}
		// A CHECK NOT RUN HERE LEAVES THE RUN INCOMPLETE: in the report a pipeline reads and in the exit
		// status, though every check that ran passed. The control is the
		// same run without it: it passes, the host boundary's own rows —
		// not run here by design — beside it.
		for _, run := range []struct {
			name    string
			results []checkResult
			result  string
			code    int
		}{
			{"with the check not run here", got, "incomplete", exitIncomplete},
			{"the control, only the check that ran", got[1:], "pass", exitPass},
		} {
			rep := &report{id: "org.example.sessionchild", version: "0.1.0", reportPath: filepath.Join(t.TempDir(), "qual.json")}
			rep.pass("readiness", "ready")
			reportChecks(rep, run.results)
			code := finish(rep, nil)
			raw, err := os.ReadFile(rep.reportPath)
			if err != nil {
				t.Fatalf("%s: %v", run.name, err)
			}
			var env qualEnvelope
			if err := json.Unmarshal(raw, &env); err != nil {
				t.Fatalf("%s: %v", run.name, err)
			}
			if env.Result != run.result || code != run.code {
				t.Errorf("%s: the report says %q and the run exits %d; want %q and %d", run.name, env.Result, code, run.result, run.code)
			}
			hostBoundary := false
			for _, e := range env.Checks {
				hostBoundary = hostBoundary || (e.Class == "host" && e.Status == "incomplete")
			}
			if !hostBoundary {
				t.Errorf("%s: the report names no host boundary left incomplete: %+v", run.name, env.Checks)
			}
		}
	})
	t.Run("a session the engine never ends", func(t *testing.T) {
		got, _, _ := runSessionChecks(t, child, "noend", `{"checks":[`+speaksAText+`,`+strings.Replace(hearsTone, "hears it", "hears it after", 1)+`]}`)
		if len(got) != 2 || got[0].outcome != checkTimedOut || !strings.Contains(got[0].words, "did not end the check's session") ||
			got[1].outcome != checkUnanswered || !strings.Contains(got[1].words, "never ended an earlier check's session") {
			t.Fatalf("%+v", got)
		}
	})
}

// heardFrame is one input frame as the test engine read it.
type heardFrame struct {
	stream, seq uint32
	kind        sdk.AudioKind
	start       int64
	first       int
}

// heardFrames reads the test engine's "heard" lines, in the order read.
func heardFrames(stderr string) []heardFrame {
	var out []heardFrame
	for _, line := range strings.Split(stderr, "\n") {
		var f heardFrame
		if _, err := fmt.Sscanf(line, "sessionchild: heard stream=%d kind=%d seq=%d start=%d first=%d", &f.stream, &f.kind, &f.seq, &f.start, &f.first); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// AN ABORTED CHECK'S RECORDING ENDS WITH ITS CHECK. The engine fails the first check at the first frame of its
// recording and ends its session at the abort at once, so the second check
// opens straight after. The two recordings differ in every sample, and the
// second outlasts the first. On the engine's input, nothing of the first
// recording — not a frame, not its END — follows the second's first frame;
// every frame from there is the second's on its own stream; the first's END
// is never sent at all, its check having been over long before it was due;
// and the second check passes.
func TestAnAbortedChecksRecordingNeverReachesTheNextCheck(t *testing.T) {
	child := buildSessionChild(t)
	const rate, firstLevel, secondLevel = 16000, 1111, 2222
	const firstLen, secondLen = rate, 3 * rate / 2 // 1 s, then 1.5 s
	checks := sessionChecksWith(t, `{"checks":[`+
		`{"name":"fails at its first frame","sets":["*"],"lane":"session","input":{"audio":"checks/first.wav"},"expect":{"events":{"order":["session_ready","transcript_final","input_finished"]}},"within_ms":5000},`+
		`{"name":"hears the second","sets":["*"],"lane":"session","input":{"audio":"checks/second.wav"},"expect":{"events":{"order":["session_ready","transcript_final","input_finished"],"transcript_contains":["cobalt lantern"]}},"within_ms":5000}]}`,
		map[string][]byte{"checks/first.wav": levelWAV(rate, 1, firstLen, firstLevel), "checks/second.wav": levelWAV(rate, 1, secondLen, secondLevel)})
	got, _, sw := runSessionChecksWith(t, child, "failfirst", checks)
	if len(got) != 2 || got[0].outcome == checkPassed || got[1].outcome != checkPassed {
		t.Fatalf("the first check fails on the engine's word and the second passes: %+v", got)
	}
	// The engine writes a frame to stderr as it reads it: wait for the
	// second recording's END, which precedes its finish_input.
	var heard []heardFrame
	for wait := time.Now().Add(5 * time.Second); ; {
		heard = heardFrames(sw.Stderr())
		if n := len(heard); (n > 0 && heard[n-1].kind == sdk.AudioEnd && heard[n-1].start == secondLen) || time.Now().After(wait) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	second := -1
	for i, f := range heard {
		if f.kind == sdk.AudioPCM && f.first == secondLevel {
			second = i
			break
		}
	}
	if second < 0 {
		t.Fatalf("the second recording never reached the engine: %+v", heard)
	}
	for _, f := range heard[:second] {
		if f.kind != sdk.AudioPCM || f.first != firstLevel || f.stream != 1 {
			t.Errorf("before the second check's audio, %+v reached the engine: only the first recording's frames, on stream 1, may", f)
		}
	}
	t.Logf("%d frame(s) of the first recording reached the engine, all before the second check's first", second)
	if second == 0 || second >= firstLen/(rate/50) {
		t.Errorf("%d frames of the first recording reached the engine: the frame it failed at, and not the rest", second)
	}
	ends := 0
	for _, f := range heard[second:] {
		switch {
		case f.kind == sdk.AudioPCM && f.first == secondLevel && f.stream == 2:
		case f.kind == sdk.AudioEnd && f.stream == 2 && f.start == secondLen:
			ends++
		default:
			t.Errorf("after the second check's first frame, %+v reached the engine: only the second recording, on stream 2, may", f)
		}
	}
	if ends != 1 {
		t.Errorf("the second recording's END reached the engine %d times, want once: %+v", ends, heard[second:])
	}
}

// A SENDER THAT CANNOT BE RETIRED ENDS ITS CARRIER. The engine never reads its input and fails the check a second
// after it is ready; the recording, more than a pipe holds, leaves its
// sender blocked in a write that nothing will drain. That sender cannot be
// stopped, so its carrier is not reused: the check says so, and the check
// after it is not asked.
func TestASenderThatCannotBeRetiredEndsItsCarrier(t *testing.T) {
	child := buildSessionChild(t)
	checks := sessionChecksWith(t, `{"checks":[`+
		`{"name":"never heard","sets":["*"],"lane":"session","input":{"audio":"checks/long.wav"},"expect":{"events":{"order":["session_ready","transcript_final","input_finished"]}},"within_ms":10000},`+
		strings.Replace(hearsTone, "hears it", "hears it after", 1)+`]}`,
		map[string][]byte{"checks/long.wav": levelWAV(48000, 2, 3*48000, 3333), "checks/tone.wav": toneWAV()})
	got, _, _ := runSessionChecksWith(t, child, "deaf", checks)
	if len(got) != 2 || got[0].outcome == checkPassed || !strings.Contains(got[0].words, "stopped reading its input") ||
		got[1].outcome != checkUnanswered || !strings.Contains(got[1].words, "not asked") || !strings.Contains(got[1].words, "stopped reading its input") {
		t.Fatalf("the blocked sender's carrier is ended and the next check is not asked: %+v", got)
	}
}

// UNDER A SESSION CHECK THE STAND-IN HOST ANSWERS AS A HOST DOES: the
// engine asks, during its check's session, for a file, a write and its
// settings; the first two are refused at once, VALIDATION_DENY, and
// settings.get answers the package's own declared default.
func TestUnderASessionCheckTheStandInHostRefusesAtOnce(t *testing.T) {
	child := buildSessionChild(t)
	asks := `{"name":"asks the host","sets":["*"],"lane":"session","input":{"audio":"checks/tone.wav"},"expect":{"events":{"transcript_contains":["fs read refused validation deny","kv put refused validation deny","packagedefault"]}},"within_ms":5000}`
	got, h, _ := runSessionChecks(t, child, "hostcalls", `{"checks":[`+asks+`]}`)
	if got[0].outcome != checkPassed {
		t.Fatalf("the engine's host calls were answered as a host answers them: %+v; harness: %v", got, h.observations)
	}
}

// THE VOICE SKELETON'S CHECKS PASS AS A HOST RUNS THEM: examples/voice-skel
// carries a recording's check and a text's (validation.json, its recording
// packed from checks/ by the packer); `aiisdk test`'s native flow starts
// the skeleton built for this machine with its session lane and audio
// pair, and both pass — it hears its recording's tail and finalizes it,
// and speaks a tone long and loud enough — and it exits cleanly.
func TestTheVoiceSkeletonsChecksPassAsAHostRunsThem(t *testing.T) {
	src := filepath.Join("..", "..", "examples", "voice-skel")
	cfg, err := aiiospkg.LoadAuthorConfig(filepath.Join(src, aiiospkg.AuthorFileName))
	if err != nil {
		t.Fatal(err)
	}
	variant := nativeVariant(cfg)
	if variant == nil {
		t.Skip("the voice skeleton declares no native variant for this machine")
	}
	dir := t.TempDir()
	gobin := os.Getenv("GO")
	if gobin == "" {
		gobin = "go"
	}
	carrier := filepath.Join(dir, filepath.FromSlash(variant.Artifact))
	build := exec.Command(gobin, "build", "-o", carrier, "./"+filepath.ToSlash(src))
	build.Env = append(os.Environ(), "CGO_ENABLED=0", "GOFLAGS=-buildvcs=false")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the voice skeleton: %v\n%s", err, b)
	}
	install := map[string][]byte{}
	raw, err := packValidation(src, cfg, install, []byte(`[]`))
	if err != nil {
		t.Fatalf("the packer takes the skeleton's checks: %v", err)
	}
	install[aiiospkg.ValidationFile] = raw
	root := filepath.Join(stageDir(dir, cfg), "install-root")
	for rel, b := range install {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h, err := newHarness(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rep := &report{}
	runNative(dir, cfg, rep, h, variant, 20*time.Second, "", "")
	byName := map[string]checkEntry{}
	for _, e := range rep.entries {
		byName[e.Name] = e
	}
	for _, name := range []string{"check hears its tail", "check speaks the ready sentence", "exit"} {
		if e := byName[name]; e.Status != "pass" {
			t.Errorf("%s: %+v", name, e)
		}
	}
	if code := finish(rep, nil); code != exitPass {
		t.Fatalf("a run whose checks all pass exits %d: %+v", code, rep.entries)
	}
}
