//go:build !wasm_unknown

package aiiosdk

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// .
// .
// .
func TestAudioCodecMatchesTheHostVector(t *testing.T) {
	raw, err := os.ReadFile("../../vectors/audio_framing.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Frames []struct {
			Kind   string `json:"kind"`
			Stream uint32 `json:"stream"`
			Seq    uint32 `json:"seq"`
			Start  int64  `json:"start"`
			PCMHex string `json:"pcm_hex"`
		} `json:"frames"`
		Hex string `json:"hex"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	kinds := map[string]AudioKind{"pcm": AudioPCM, "discontinuity": AudioDiscontinuity, "end": AudioEnd}
	var frames []AudioFrame
	for _, f := range v.Frames {
		pcm, _ := hex.DecodeString(f.PCMHex)
		frames = append(frames, AudioFrame{Kind: kinds[f.Kind], Stream: f.Stream, Seq: f.Seq, Start: f.Start, PCM: pcm})
	}
	var buf bytes.Buffer
	for _, fr := range frames {
		if err := WriteAudioFrame(&buf, fr); err != nil {
			t.Fatal(err)
		}
	}
	if got := hex.EncodeToString(buf.Bytes()); got != v.Hex {
		t.Fatalf("the kit's codec drifted from the host's vector:\n got %s\nwant %s", got, v.Hex)
	}
	for i, want := range frames {
		got, err := ReadAudioFrame(&buf)
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if got.Kind != want.Kind || got.Stream != want.Stream || got.Seq != want.Seq || got.Start != want.Start || !bytes.Equal(got.PCM, want.PCM) {
			t.Fatalf("frame %d: got %+v want %+v", i, got, want)
		}
	}
	if _, err := ReadAudioFrame(&buf); err != io.EOF {
		t.Fatalf("after the last frame: %v", err)
	}
	if err := WriteAudioFrame(&buf, AudioFrame{Kind: AudioEnd, PCM: []byte{1}}); err == nil {
		t.Fatal("an end frame with a payload must be refused")
	}
	if err := WriteAudioFrame(&buf, AudioFrame{Kind: AudioPCM, PCM: make([]byte, MaxAudioFramePayload+1)}); err != ErrAudioFrameTooBig {
		t.Fatalf("over the ceiling: %v", err)
	}
}
