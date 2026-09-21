//go:build !wasm_unknown

package aiiosdk

// .
// .

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

// .
// .
func streamHost(t *testing.T, chunks ...string) *[]string {
	t.Helper()
	var asked []string
	next := 0
	withHost(t, func(params []byte) ([]byte, error) {
		p := string(params)
		switch {
		case strings.Contains(p, `"operation":"http.get"`):
			asked = append(asked, "http.get")
			return []byte(`{"success":true,"ok":true,"status":"succeeded","operation_result":{"stream_id":"s1","http_status":200,"chunk_max_bytes":65536},"external_receipt":{}}`), nil
		case strings.Contains(p, `"operation":"http.read"`):
			asked = append(asked, "http.read")
			if next >= len(chunks) {
				t.Errorf("http.read asked past the terminal chunk")
				return []byte(`{"success":false,"ok":false,"status":"failed","reasonCode":"NET_STREAM_UNKNOWN","external_receipt":{}}`), nil
			}
			c := chunks[next]
			next++
			return []byte(fmt.Sprintf(`{"success":true,"ok":true,"status":"succeeded","operation_result":{"data_b64":%q,"done":%t,"seq":%d,"total_bytes":%d},"external_receipt":{}}`,
				base64.StdEncoding.EncodeToString([]byte(c)), next == len(chunks), next, len(c))), nil
		case strings.Contains(p, `"operation":"http.close"`):
			asked = append(asked, "http.close")
			return []byte(`{"success":true,"ok":true,"status":"succeeded","operation_result":{},"external_receipt":{}}`), nil
		}
		t.Errorf("unexpected host call: %s", p)
		return nil, fmt.Errorf("unexpected")
	})
	return &asked
}

// .
// .
// .
func TestReadAllHoldsItsLimitOnTheTerminalChunk(t *testing.T) {
	for _, chunks := range [][]string{
		{"abcd"},
		{"ab", "cd"},
		{"abc", "d"},
	} {
		streamHost(t, chunks...)
		s, err := HTTP.Stream("GET", "https://api.example.com/v1", nil)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.ReadAll(3)
		if err == nil {
			t.Errorf("%q: ReadAll(3) returned %d bytes and no error", chunks, len(got))
		} else if !strings.Contains(err.Error(), "3-byte limit") {
			t.Errorf("%q: the refusal does not name the limit: %v", chunks, err)
		}
	}
}

// .
// .
// .
func TestReadAllAtItsLimitAndTheCloseItOwes(t *testing.T) {
	streamHost(t, "ab", "c")
	s, err := HTTP.Stream("GET", "https://api.example.com/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.ReadAll(3); err != nil || string(got) != "abc" {
		t.Fatalf("a 3-byte body under ReadAll(3): %q %v", got, err)
	}

	asked := streamHost(t, "abcd", "ef", "gh")
	s, _ = HTTP.Stream("GET", "https://api.example.com/v1", nil)
	if _, err := s.ReadAll(3); err == nil {
		t.Fatal("an overrun mid-stream returned no error")
	}
	if got := strings.Join(*asked, " "); got != "http.get http.read http.close" {
		t.Errorf("mid-stream overrun: host was asked %q, want one read then the close", got)
	}

	asked = streamHost(t, "abcd")
	s, _ = HTTP.Stream("GET", "https://api.example.com/v1", nil)
	_, _ = s.ReadAll(3)
	if got := strings.Join(*asked, " "); got != "http.get http.read" {
		t.Errorf("terminal overrun: host was asked %q — the exchange had already ended, there is nothing to close", got)
	}
}
