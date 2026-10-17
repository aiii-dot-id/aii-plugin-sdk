package aiiosdk

import (
	"bytes"
	"testing"
)

func TestWorkSessionUsesOnlyTheInvocationEnvelope(t *testing.T) {
	for _, tc := range []struct {
		name, args, context string
		want                WorkSession
	}{
		{"trusted sibling", `{"text":"safe"}`, `,"_host_work_session":{"id":"task-1","reason":"","call_id":"call-1"}`, WorkSession{ID: "task-1", CallID: "call-1"}},
		{"old host", `{"text":"safe"}`, "", WorkSession{}},
		{"caller cannot forge", `{"_host_work_session":{"id":"forged","call_id":"forged-call"}}`, "", WorkSession{}},
		{"caller cannot override", `{"_host_work_session":{"id":"forged"}}`, `,"_host_work_session":{"id":"actual","call_id":"actual-call"}`, WorkSession{ID: "actual", CallID: "actual-call"}},
		{"unattributed", `{}`, `,"_host_work_session":{"reason":"unattributed"}`, WorkSession{Reason: "unattributed"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := New("context-fixture")
			var captured Call
			called := false
			p.Handle("strict", func(c Call) (any, error) {
				captured, called = c, true
				// Existing strict handlers continue to receive exactly their
				// old argument object; transport metadata adds no public key.
				if string(c.Arguments) != tc.args {
					t.Fatalf("argument object changed: %s", c.Arguments)
				}
				return true, nil
			})
			frame := []byte(`{"jsonrpc":"2.0","id":1,"method":"invoke.call","params":{"operation":"strict","arguments":` + tc.args + tc.context + `}}`)
			reply := p.respond(frame)
			if !called || !bytes.Contains(reply, []byte(`"status":"succeeded"`)) {
				t.Fatalf("handler did not succeed: %s", reply)
			}
			if got := captured.WorkSession(); got != tc.want {
				t.Fatalf("work context: got %+v want %+v", got, tc.want)
			}
			for i := range frame {
				frame[i] = ' '
			}
			if got := captured.WorkSession(); got != tc.want || string(captured.Arguments) != tc.args {
				t.Fatal("retained invocation aliases the input buffer")
			}
		})
	}
}
