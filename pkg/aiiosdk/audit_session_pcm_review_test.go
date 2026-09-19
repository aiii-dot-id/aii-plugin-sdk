// .
// .

package aiiosdk

import "testing"

func TestAuditSessionAudioRefusesUnsupportedPCMEncoding(t *testing.T) {
	args := Object(`{"session_id":"s","output_handle":"out:s:spk","audio":{"format":"f32le","input":null,"output":{"rate":16000,"channels":1}}}`)
	if got, err := ParseSessionAudio(args); err == nil {
		t.Fatalf("unsupported f32le silently accepted as s16le topology: %+v", got)
	}
}
