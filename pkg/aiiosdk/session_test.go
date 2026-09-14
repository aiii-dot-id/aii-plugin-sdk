//go:build !wasm_unknown

package aiiosdk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// .
// .
// .
// .
func TestResidentSessionAdmitsWhileBusyPreservesOrderAndNotifies(t *testing.T) {
	h2g_r, h2g_w, _ := os.Pipe()
	g2h_r, g2h_w, _ := os.Pipe()
	t.Cleanup(func() { h2g_w.Close(); g2h_r.Close() })

	var mu sync.Mutex
	var admitted []string
	release := make(chan struct{})
	hostAnswered := make(chan struct{})

	admit := answerWith(func(s *Session, op string, args Object) (any, error) {
		mu.Lock()
		admitted = append(admitted, op)
		mu.Unlock()
		switch op {
		case "speech.session.synthesize":
			sid, _ := args.String("synthesis_id")
			go func() {
				<-release
				_ = s.Emit(map[string]any{"type": "synthesis_end", "synthesis_id": sid, "sequence": 1})
			}()
			return map[string]any{"synthesis_id": sid, "accepted": true, "state": "generating"}, nil
		case "speech.session.status":
			return map[string]any{"lifecycle": "open", "state_sequence": 7}, nil
		case "speech.session.reach_host":
			go func() {
				_, err := s.HostCall(context.Background(), "voice.observe", map[string]any{"k": "v"})
				if err == nil {
					close(hostAnswered)
				}
			}()
			return map[string]any{"accepted": true}, nil
		default:
			return map[string]any{"accepted": true}, nil
		}
	})

	p := New("org.example.voice")
	done := make(chan error, 1)
	go func() { done <- p.serveSession(h2g_r, g2h_w, admit) }()

	send := func(id int, op string, args map[string]any) {
		t.Helper()
		a, _ := json.Marshal(args)
		frame := []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"invoke.call","params":{"operation":%q,"arguments":%s}}`, id, op, a))
		if err := WriteFrame(h2g_w, frame, MaxControlFrameBytes); err != nil {
			t.Fatalf("send: %v", err)
		}
	}
	read := func() map[string]json.RawMessage {
		t.Helper()
		g2h_r.SetReadDeadline(time.Now().Add(5 * time.Second))
		f, err := ReadFrame(g2h_r, MaxControlFrameBytes)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(f, &m); err != nil {
			t.Fatalf("decode %s: %v", f, err)
		}
		return m
	}

	// .
	send(1, "speech.session.synthesize", map[string]any{"synthesis_id": "s1"})
	r1 := read()
	if string(r1["id"]) != "1" || !hasField(r1["result"], "accepted") {
		t.Fatalf("synthesize admission: %v", r1)
	}
	// .
	// .
	send(2, "speech.session.status", nil)
	r2 := read()
	if string(r2["id"]) != "2" || !hasField(r2["result"], "lifecycle") {
		t.Fatalf("status must answer while busy: %v", r2)
	}
	// .
	// .
	close(release)
	ev := read()
	if _, hasID := ev["id"]; hasID {
		t.Fatalf("an event is a notification with no id: %v", ev)
	}
	if string(ev["method"]) != `"session.event"` {
		t.Fatalf("event method: %s", ev["method"])
	}
	if !hasFieldValue(ev["params"], "type", "synthesis_end") {
		t.Fatalf("event params: %s", ev["params"])
	}

	// .
	send(3, "speech.session.reach_host", nil)
	_ = read()
	hostCall := read()
	if string(hostCall["method"]) != `"invoke.call"` || len(hostCall["id"]) == 0 {
		t.Fatalf("the guest host-call is a request with an id: %v", hostCall)
	}
	// .
	ans := append([]byte(`{"jsonrpc":"2.0","id":`), hostCall["id"]...)
	ans = append(ans, []byte(`,"result":{"ok":true}}`)...)
	WriteFrame(h2g_w, ans, MaxControlFrameBytes)
	select {
	case <-hostAnswered:
	case <-time.After(5 * time.Second):
		t.Fatal("the guest never saw the host's answer to its call")
	}

	// .
	mu.Lock()
	order := append([]string(nil), admitted...)
	mu.Unlock()
	if len(order) < 2 || order[0] != "speech.session.synthesize" || order[1] != "speech.session.status" {
		t.Fatalf("admission order not preserved: %v", order)
	}

	h2g_w.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveSession ended with: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serveSession did not end on EOF")
	}
}

func hasField(raw json.RawMessage, key string) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && len(m[key]) != 0
}
func hasFieldValue(raw json.RawMessage, key, val string) bool {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	var s string
	return json.Unmarshal(m[key], &s) == nil && s == val
}
