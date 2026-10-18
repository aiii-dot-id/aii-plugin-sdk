package main

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// THE RECORDINGS THIS EXAMPLE SHIPS ARE WELL-FORMED: a RIFF/WAVE file whose
// RIFF size accounts for every byte, holding only its format and its
// samples, as a host, an author's tools and the public release's audit
// read one (the release audit refused this example's tone for a RIFF size
// four bytes too large).
func TestTheExamplesRecordingsAreWellFormed(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("checks", "*.wav"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no recording found: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
			t.Fatalf("%s is not a RIFF/WAVE recording", f)
		}
		if got, want := int(binary.LittleEndian.Uint32(b[4:8])), len(b)-8; got != want {
			t.Errorf("%s: its RIFF size is %d, want %d (the file's length less 8)", f, got, want)
		}
		for i := 12; i+8 <= len(b); {
			id := string(b[i : i+4])
			n := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
			if id != "fmt " && id != "data" {
				t.Errorf("%s: a %q chunk; a shipped recording holds only its format and its samples", f, id)
			}
			i += 8 + n + n&1
			if i > len(b) {
				t.Errorf("%s: the %q chunk runs past the end", f, id)
			}
		}
	}
}
