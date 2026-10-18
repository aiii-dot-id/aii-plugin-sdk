package workerdrive

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// A FRAME ON THE ENGINE'S AUDIO INPUT IS WRITTEN WHOLE : a frame is a header and a payload, two writes, and writers at
// once never put one frame's header between another's header and payload.
// Every frame read back is intact and carries its own writer's payload.
func TestAudioInputFramesAreWrittenWhole(t *testing.T) {
	if os.Getenv("AII_WORKERDRIVE_EXIT_CHILD") == "1" {
		t.Skip("the exit test's child runs only the exit test")
	}
	r, wp, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := &SessionWorker{audioIn: wp}
	const writers, frames = 4, 400
	var wg sync.WaitGroup
	for i := 1; i <= writers; i++ {
		wg.Add(1)
		go func(stream uint32) {
			defer wg.Done()
			pcm := bytes.Repeat([]byte{byte(stream)}, 2048)
			for seq := uint32(1); seq <= frames; seq++ {
				if w.WriteAudio(sdk.AudioFrame{Kind: sdk.AudioPCM, Stream: stream, Seq: seq, PCM: pcm}) != nil {
					return // the reader stopped at a broken frame
				}
			}
		}(uint32(i))
	}
	go func() {
		wg.Wait()
		wp.Close()
	}()
	read, broken := 0, ""
	for broken == "" {
		fr, err := sdk.ReadAudioFrame(r)
		if err == io.EOF {
			break
		}
		switch {
		case err != nil:
			broken = err.Error()
		case len(fr.PCM) != 2048 || bytes.Count(fr.PCM, []byte{byte(fr.Stream)}) != len(fr.PCM):
			broken = "a frame whose payload is not its own writer's"
		default:
			read++
		}
	}
	if broken != "" {
		r.Close() // the writers stop on their next write
		wg.Wait()
		t.Fatalf("after %d whole frames: %s", read, broken)
	}
	if read != writers*frames {
		t.Fatalf("%d frames read back, %d written", read, writers*frames)
	}
}
