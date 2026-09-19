// .
// .
// .
// .
// .
// .
// .

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// .
// .
func TestAuditNamedEchoAcceptsItsTerminalPlaybackReport(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bin := filepath.Join(t.TempDir(), "voice-skel-review")
	build := exec.CommandContext(ctx, "go", "build", "-race", "-o", bin, ".")
	build.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false")
	if out, err := build.CombinedOutput(); err != nil {
		plain := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
		plain.Env = append(os.Environ(), "GOFLAGS=-buildvcs=false", "CGO_ENABLED=0")
		if out2, err2 := plain.CombinedOutput(); err2 != nil {
			t.Fatalf("build: %v\n%s\n%v\n%s", err, out, err2, out2)
		}
	}
	childIn, hostIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	hostOut, childOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []*os.File{childIn, hostIn, hostOut, childOut} {
		defer f.Close()
	}
	cmd := exec.CommandContext(ctx, bin)
	cmd.ExtraFiles = []*os.File{childIn, childOut}
	cmd.Env = []string{"AII_AUDIO_IN_FD=3", "AII_AUDIO_OUT_FD=4"}
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
	waited := false
	defer func() {
		stdin.Close()
		hostIn.Close()
		if !waited {
			cancel()
			_ = cmd.Wait()
		}
	}()
	childIn.Close()
	childOut.Close()
	deadline := time.Now().Add(10 * time.Second)
	_ = hostOut.SetReadDeadline(deadline)
	_ = stdout.(*os.File).SetReadDeadline(deadline)
	send := func(id int, op, args string) {
		t.Helper()
		msg := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"invoke.call","params":{"operation":%q,"arguments":%s}}`, id, op, args))
		if err := aiiosdk.WriteFrame(stdin, msg, aiiosdk.MaxControlFrameBytes); err != nil {
			t.Fatal(err)
		}
	}
	var echoID string
	var echoStream uint32
	readTo := func(id string) []byte {
		t.Helper()
		for {
			raw, err := aiiosdk.ReadFrame(stdout, aiiosdk.MaxControlFrameBytes)
			if err != nil {
				t.Fatal(err)
			}
			var m struct {
				ID     json.RawMessage `json:"id"`
				Params struct {
					Type         string `json:"type"`
					SynthesisID  string `json:"synthesis_id"`
					OutputStream uint32 `json:"output_stream"`
				} `json:"params"`
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			if m.Params.Type == "synthesis_start" {
				echoID, echoStream = m.Params.SynthesisID, m.Params.OutputStream
			}
			if string(m.ID) == id {
				return raw
			}
		}
	}
	send(1, "speech.session.open", `{"session_id":"s","input_handle":"in:s:mic","output_handle":"out:s:spk","audio":{"format":"s16le","rate":16000,"channels":1}}`)
	readTo("1")
	for _, fr := range []aiiosdk.AudioFrame{{Kind: aiiosdk.AudioPCM, Stream: 3, Seq: 1, PCM: make([]byte, 640)}, {Kind: aiiosdk.AudioEnd, Stream: 3, Seq: 2, Start: 320}} {
		if err := aiiosdk.WriteAudioFrame(hostIn, fr); err != nil {
			t.Fatal(err)
		}
		got, err := aiiosdk.ReadAudioFrame(hostOut)
		if err != nil {
			t.Fatal(err)
		}
		if got.Kind != fr.Kind || got.Start != fr.Start {
			t.Fatalf("echo span: %+v", got)
		}
		echoStream = got.Stream
	}
	send(2, "speech.session.status", `{"session_id":"s"}`)
	readTo("2")
	if echoID == "" || echoStream == 0 {
		t.Fatal("echo was not named")
	}
	send(3, "speech.session.playback_report", fmt.Sprintf(`{"session_id":"s","synthesis_id":%q,"output_stream":%d,"rendered_samples":320,"terminal":true}`, echoID, echoStream))
	reply := readTo("3")
	var receipt struct {
		Result struct {
			Accepted bool `json:"accepted"`
		} `json:"result"`
	}
	if err := json.Unmarshal(reply, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.Result.Accepted {
		t.Errorf("legitimate receipt for named echo %q stream %d rejected: %s", echoID, echoStream, reply)
	}
	stdin.Close()
	hostIn.Close()
	err = cmd.Wait()
	waited = true
	if err != nil {
		t.Errorf("fixture exit: %v", err)
	}
}
