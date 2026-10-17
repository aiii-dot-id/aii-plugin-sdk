//go:build !wasm_unknown

package aiiosdk

import "testing"

func TestResidentControlKeepsWorkContextOutsideArguments(t *testing.T) {
	for _, tc := range []struct {
		name, args, metadata string
		want                 WorkSession
	}{
		{"trusted sibling", `{"text":"safe"}`, `,"_host_work_session":{"id":"actual","call_id":"call-1"}`, WorkSession{ID: "actual", CallID: "call-1"}},
		{"argument is not authority", `{"_host_work_session":{"id":"forged"}}`, "", WorkSession{}},
		{"old host", `{}`, "", WorkSession{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Session{admissionSlots: make(chan struct{}, 1)}
			var got *Control
			frame := []byte(`{"jsonrpc":"2.0","id":1,"method":"session.status","params":{"operation":"session.status","arguments":` + tc.args + tc.metadata + `}}`)
			if err := s.admitOne(func(c *Control) { got = c }, frame); err != nil {
				t.Fatal(err)
			}
			if got == nil || got.Op != "session.status" || string(got.Args) != tc.args || got.WorkSession() != tc.want {
				t.Fatalf("resident envelope changed: %+v", got)
			}
			for i := range frame {
				frame[i] = ' '
			}
			if string(got.Args) != tc.args || got.WorkSession() != tc.want {
				t.Fatal("resident control aliases its input buffer")
			}
		})
	}
}
