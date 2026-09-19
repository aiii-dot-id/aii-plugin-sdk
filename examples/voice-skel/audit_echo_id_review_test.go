// .
// .
// .
// .

package main

import (
	"fmt"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
	"os"
	"testing"
	"time"
)

func TestAuditEchoCannotReplaceAnAdmittedSynthesisID(t *testing.T) {
	e := newEngine()
	if _, err := e.open(aiiosdk.Object(`{"session_id":"s","test_synth_ms":0}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.synthesize(aiiosdk.Object(`{"session_id":"s","synthesis_id":"echo-2","text":"one"}`)); err != nil {
		t.Fatal(err)
	}
	e.mu.Lock()
	original := e.byID["echo-2"]
	e.mu.Unlock()
	select {
	case <-original.done:
	case <-time.After(time.Second):
		t.Fatal("synthesis did not finish")
	}
	receipt := aiiosdk.Object(`{"session_id":"s","synthesis_id":"echo-2","output_stream":1,"rendered_samples":0,"terminal":true}`)
	if _, err := e.playbackReport(receipt); err != nil {
		t.Fatalf("original receipt: %v", err)
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	pair := aiiosdk.NewAudioPair(inR, outW)
	done := make(chan struct{})
	go func() { defer close(done); e.echoAudio(pair) }()
	defer func() {
		inW.Close()
		inR.Close()
		outW.Close()
		outR.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Error("echo worker not joined")
		}
	}()
	_ = outR.SetReadDeadline(time.Now().Add(time.Second))
	stream := uint32(0)
	for _, fr := range []aiiosdk.AudioFrame{{Kind: aiiosdk.AudioPCM, Stream: 9, Seq: 1, PCM: []byte{1, 2}}, {Kind: aiiosdk.AudioEnd, Stream: 9, Seq: 2, Start: 1}} {
		if err := aiiosdk.WriteAudioFrame(inW, fr); err != nil {
			t.Fatal(err)
		}
		got, err := aiiosdk.ReadAudioFrame(outR)
		if err != nil {
			t.Fatal(err)
		}
		stream = got.Stream
	}
	e.mu.Lock()
	echo := e.byID[fmt.Sprintf("echo-%d", stream)]
	e.mu.Unlock()
	if echo == nil {
		t.Fatal("echo not registered")
	}
	select {
	case <-echo.done:
	case <-time.After(time.Second):
		t.Fatal("echo END not accounted")
	}
	if _, err := e.playbackReport(receipt); err != nil {
		t.Fatalf("an exact terminal retry was valid before echo allocation but is now refused: %v; old stream=%d echo stream=%d", err, original.stream, stream)
	}
	e.mu.Lock()
	same := e.byID["echo-2"] == original
	e.mu.Unlock()
	if !same {
		t.Error("echo replaced the admitted generation's identity")
	}
}

func TestAuditEngineRefusesUnsupportedPCMEncoding(t *testing.T) {
	e := newEngine()
	_, err := e.open(aiiosdk.Object(`{"session_id":"s","output_handle":"out:s:spk","audio":{"format":"f32le","input":null,"output":{"rate":16000,"channels":1}}}`))
	if err == nil {
		t.Fatal("canonical output-only open accepted f32le while the engine only speaks s16le")
	}
}
