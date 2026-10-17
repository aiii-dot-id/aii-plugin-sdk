package aiiospkg

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"
)

// wavOf is a 16-bit PCM WAV of n silent sample groups.
func wavOf(rate, channels, n int) []byte {
	data := make([]byte, n*2*channels)
	h := []byte("RIFF")
	h = binary.LittleEndian.AppendUint32(h, uint32(36+len(data)))
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint16(h, uint16(channels))
	h = binary.LittleEndian.AppendUint32(h, uint32(rate))
	h = binary.LittleEndian.AppendUint32(h, uint32(rate*2*channels))
	h = binary.LittleEndian.AppendUint16(h, uint16(2*channels))
	h = binary.LittleEndian.AppendUint16(h, 16)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	return append(h, data...)
}

// A PACKAGE'S RECORDINGS ARE BOUNDED, EACH MEMBER COUNTED ONCE, as the host
// bounds them: two members of more than MaxCheckAudioBytes refuse the
// package; one member sent by two checks is counted once.
func TestTheRecordingsAPackageCarriesAreBoundedEachMemberOnce(t *testing.T) {
	big := wavOf(192000, 2, 1<<20)
	check := func(name, member string) string {
		return `{"name":"` + name + `","sets":["*"],"lane":"session","input":{"audio":"` + member + `"},"expect":{"events":{"order":["input_finished"]}},"within_ms":10000}`
	}
	members := func(rel string) ([]byte, bool, error) {
		if rel == "a.wav" || rel == "b.wav" {
			return big, true, nil
		}
		return nil, false, nil
	}
	v, err := ParseValidation([]byte(`{"checks":[`+check("one", "a.wav")+`,`+check("two", "a.wav")+`]}`), members)
	if err != nil || len(v.Checks) != 2 || v.Checks[0].Unresolved != "" || v.Checks[0].Recording != time.Second*(1<<20)/192000 {
		t.Fatalf("one member sent twice is counted once: %v %+v", err, v)
	}
	_, err = ParseValidation([]byte(`{"checks":[`+check("one", "a.wav")+`,`+check("two", "b.wav")+`]}`), members)
	var refused *ValidationError
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "recordings come to") {
		t.Fatalf("two members of more than %d bytes refuse the package: %v", MaxCheckAudioBytes, err)
	}
}

// THE PACKER STAYS STRICT ABOUT A LANE NO HOST RUNS. The host reads such a
// check and shows it not run; nothing would ever run it, so the packer
// refuses it before release.
func TestThePackerRefusesALaneNoHostRuns(t *testing.T) {
	v, err := ParseValidation([]byte(`{"checks":[{"name":"streams","sets":["*"],"lane":"stream","feed":{"chunks":3},"within_ms":2000}]}`),
		func(string) ([]byte, bool, error) { return nil, false, nil })
	if err != nil {
		t.Fatalf("the reader reads it, as the host does: %v", err)
	}
	if err := ValidatePackageChecks(v, PackageChecks{}); err == nil || !strings.Contains(err.Error(), `lane "stream", which no host runs`) {
		t.Fatalf("the packer refuses a lane no host runs: %v", err)
	}
}
