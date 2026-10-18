package main

import (
	"os"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// THE END OF A STREAM IS READ WHILE IT IS BEING WRITTEN. The goroutine that
// writes a reply's stream marks its end with no lock held; a playback report
// reads that mark under the engine's lock, from the control loop. The mark
// was a plain bool, so the two were a data race: a terminal report arriving
// as the stream ended could read it either way, and refused as "output still
// live" it left a drain waiting on a receipt the host had already given.
// `make test` runs under the race detector, and that is what failed here
// while the mark was a plain bool.
func TestAStreamsEndDoesNotRaceItsPlaybackReport(t *testing.T) {
	report := aiiosdk.Object(`{"synthesis_id":"reply","output_stream":7,"rendered_samples":0,"terminal":true}`)
	for i := 0; i < 50; i++ {
		inR, inW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		outR, outW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		pair := aiiosdk.NewAudioPair(inR, outW)
		e := newEngine()
		g := &generation{id: "reply", stream: 7, done: make(chan struct{})}
		e.lifecycle, e.sessionID = "open", "s1"
		e.byID[g.id] = g

		wrote := make(chan struct{})
		go func() { // the stream's writer, as run is
			defer close(wrote)
			endStream(pair, g)
		}()
		// Before the end is written the report is refused, after it is
		// admitted: either is right here. What is asked is that reading the
		// mark and writing it are not a race.
		_, _ = playbackReportForTest(e, report)
		<-wrote
		if _, err := playbackReportForTest(e, report); err != nil {
			t.Fatalf("pass %d: the stream's end is written and its terminal report is refused: %v", i, err)
		}
		for _, f := range []*os.File{inR, inW, outR, outW} {
			f.Close()
		}
	}
}
