package aiiosdk

import (
	"runtime"
	"strings"
	"testing"
)

// Passing over a string checks exactly what decoding it checks, and builds nothing.
func TestSkippingAStringChecksWhatDecodingItChecks(t *testing.T) {
	for _, raw := range []string{`""`, `"plain"`, `"a\"b\\c\/d\b\f\n\r\t"`, `"é😀"`, `"\ud83d"`, `"\u12"`,
		`"\x"`, "\"raw\ncontrol\"", `"unterminated`, `"é"`, `"\u0000"`} {
		a := &scanner{b: []byte(raw)}
		b := &scanner{b: []byte(raw)}
		_, decoded := a.string_()
		skipped := b.skipString()
		if decoded != skipped || decoded && a.i != b.i {
			t.Errorf("%q: decode %v at %d, skip %v at %d", raw, decoded, a.i, skipped, b.i)
		}
	}
}

// Finding a member of a reply does not build the strings it passes over: a 200 KB data member
// costs no allocation the size of it (0.1.25 built it on every pass).
func TestFindingAMemberDoesNotBuildTheStringsItPasses(t *testing.T) {
	reply := Object(`{"data_b64":"` + strings.Repeat("QUJD", 50000) + `","eof":true,"size":150000}`)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	for range 10 {
		if n, ok := reply.Int("size"); !ok || n != 150000 {
			t.Fatal("size")
		}
	}
	runtime.ReadMemStats(&after)
	if per := (after.TotalAlloc - before.TotalAlloc) / 10; per > 4<<10 {
		t.Fatalf("%d bytes allocated to read one small member past a 200 KB string", per)
	}
}
