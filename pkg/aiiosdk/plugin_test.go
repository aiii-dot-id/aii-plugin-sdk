package aiiosdk

// .
// .
// .
// .
// .

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func testPlugin() *Plugin {
	p := New("test.plugin")
	p.Handle("focus.echo", func(c Call) (any, error) {
		return c.Arguments, nil
	})
	p.Handle("focus.fail", func(c Call) (any, error) {
		return nil, Fail("OPERATION_TARGET_INVALID", "the target is wrong")
	})
	p.Handle("focus.deny", func(c Call) (any, error) {
		return nil, Deny("MY_POLICY", "")
	})
	p.Handle("focus.hostdenied", func(c Call) (any, error) {
		return nil, &Denied{Code: -32000, Message: "no capability broker attached to this worker; invoke-call denied", ReasonCode: "POLICY_DENY"}
	})
	p.Handle("focus.bug", func(c Call) (any, error) {
		return nil, errors.New("nil pointer somewhere")
	})
	p.Handle("focus.wrapped", func(c Call) (any, error) {
		return nil, fmt.Errorf("context: %w", Fail("WRAPPED_CODE", "wrapped"))
	})
	p.Handle("focus.unmarshalable", func(c Call) (any, error) {
		return make(chan int), nil
	})
	p.Handle("focus.overint", func(c Call) (any, error) {
		return int64(1) << 53, nil
	})
	p.Handle("focus.smuggle", func(c Call) (any, error) {
		// .
		// .
		return json.RawMessage(`{"a":1,"a":2}`), nil
	})
	p.Handle("focus.huge", func(c Call) (any, error) {
		return strings.Repeat("a", MaxControlFrameBytes), nil
	})
	p.Handle("focus.nilresult", func(c Call) (any, error) {
		return nil, nil
	})
	return p
}

// .
// .
// .
// .
// .
func assertResponseContract(t *testing.T, resp []byte, wantIDRaw string) map[string]json.RawMessage {
	t.Helper()
	if len(resp) == 0 {
		t.Fatalf("empty response: the worker kills the guest for this (main.go:120-124)")
	}
	if err := ValidateStrict(resp); err != nil {
		t.Fatalf("response outside the strict domain: %s", resp)
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal(resp, &members); err != nil {
		t.Fatalf("response is not an object: %s", resp)
	}
	var version string
	if err := json.Unmarshal(members["jsonrpc"], &version); err != nil || version != "2.0" {
		t.Fatalf("jsonrpc member invalid: %s", resp)
	}
	if _, has := members["method"]; has {
		t.Fatalf("a response carries no method member: %s", resp)
	}
	idRaw, has := members["id"]
	if !has {
		t.Fatalf("id member missing: %s", resp)
	}
	if wantIDRaw != "" && string(idRaw) != wantIDRaw {
		// .
		t.Fatalf("id not echoed byte-form verbatim: got %s want %s", idRaw, wantIDRaw)
	}
	_, hasResult := members["result"]
	_, hasError := members["error"]
	if hasResult == hasError {
		t.Fatalf("exactly one of result|error required: %s", resp)
	}
	return members
}

type errorObject struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    struct {
		ReasonCode string `json:"reasonCode"`
	} `json:"data"`
}

func decodeErrorMember(t *testing.T, members map[string]json.RawMessage) errorObject {
	t.Helper()
	var eo errorObject
	if err := json.Unmarshal(members["error"], &eo); err != nil {
		t.Fatalf("error member is not an error object: %s", members["error"])
	}
	return eo
}

type resultObject struct {
	Status          string          `json:"status"`
	OperationResult json.RawMessage `json:"operation_result"`
	Reason          string          `json:"reason"`
	ReasonCode      string          `json:"reasonCode"`
	ReasonCodeSnake string          `json:"reason_code"`
}

func decodeResultMember(t *testing.T, members map[string]json.RawMessage) resultObject {
	t.Helper()
	var ro resultObject
	if err := json.Unmarshal(members["result"], &ro); err != nil {
		t.Fatalf("result member does not decode: %s", members["result"])
	}
	return ro
}

func invokeFrame(id, operation, argumentsJSON string) []byte {
	// .
	// .
	f := `{"jsonrpc":"2.0","id":` + id + `,"method":"invoke.call","params":{"operation":"` + operation + `","arguments":` + argumentsJSON + `}}`
	return []byte(f)
}

func TestRespondEnvelope(t *testing.T) {
	p := testPlugin()

	t.Run("parse error answers -32700 with null id", func(t *testing.T) {
		for _, bad := range [][]byte{
			[]byte(`{"a":`),
			[]byte(`{"a":1,"a":2}`),
			[]byte(`{"v":9007199254740992}`),
			[]byte(``),
			[]byte("\xff"),
		} {
			members := assertResponseContract(t, p.respond(bad), "null")
			eo := decodeErrorMember(t, members)
			if eo.Code != -32700 || eo.Message != "JSON parse error" {
				t.Fatalf("want -32700 %q, got %d %q", "JSON parse error", eo.Code, eo.Message)
			}
		}
	})

	t.Run("non-object and bad envelope answer -32600", func(t *testing.T) {
		cases := []struct {
			frame  string
			wantID string
		}{
			{`[1,2,3]`, "null"},
			{`{"id":"x","method":"invoke.call"}`, `"x"`},
			{`{"jsonrpc":"2.1","id":"x","method":"invoke.call"}`, `"x"`},
			{`{"jsonrpc":"2.0","id":"x"}`, `"x"`},
			{`{"jsonrpc":"2.0","id":"x","method":7}`, `"x"`},
			{`{"jsonrpc":"2.0","id":true,"method":"invoke.call"}`, "null"},
			{`{"jsonrpc":"2.0","id":{"n":1},"method":"invoke.call"}`, "null"},
		}
		for _, c := range cases {
			members := assertResponseContract(t, p.respond([]byte(c.frame)), c.wantID)
			eo := decodeErrorMember(t, members)
			if eo.Code != -32600 || eo.Message != "invalid JSON-RPC 2.0 request" {
				t.Fatalf("%s: want -32600 %q, got %d %q", c.frame, "invalid JSON-RPC 2.0 request", eo.Code, eo.Message)
			}
		}
	})

	t.Run("unknown method answers the audited C dispatch shape", func(t *testing.T) {
		members := assertResponseContract(t, p.respond([]byte(`{"jsonrpc":"2.0","id":9,"method":"rpc.connect","params":{}}`)), "9")
		eo := decodeErrorMember(t, members)
		if eo.Code != -32601 || eo.Message != "method not found" || eo.Data.ReasonCode != "METHOD_NOT_FOUND" {
			t.Fatalf("want the audited C dispatch shape, got %+v", eo)
		}
	})

	t.Run("id-less request is answered with null id, not dropped", func(t *testing.T) {
		// .
		// .
		members := assertResponseContract(t, p.respond([]byte(`{"jsonrpc":"2.0","method":"invoke.call","params":{"operation":"focus.echo"}}`)), "null")
		ro := decodeResultMember(t, members)
		if ro.Status != "succeeded" {
			t.Fatalf("id-less request should still execute, got %+v", ro)
		}
	})

	t.Run("null id echoes null", func(t *testing.T) {
		assertResponseContract(t, p.respond([]byte(`{"jsonrpc":"2.0","id":null,"method":"invoke.call","params":{"operation":"focus.echo"}}`)), "null")
	})

	t.Run("numeric and string ids echo byte-form verbatim", func(t *testing.T) {
		assertResponseContract(t, p.respond(invokeFrame("42", "focus.echo", "{}")), "42")
		assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.echo", "{}")), `"h1"`)
		assertResponseContract(t, p.respond(invokeFrame("1e2", "focus.echo", "{}")), "1e2")
	})
}

func TestRespondParams(t *testing.T) {
	p := testPlugin()

	t.Run("params must be an object", func(t *testing.T) {
		members := assertResponseContract(t, p.respond([]byte(`{"jsonrpc":"2.0","id":"x","method":"invoke.call","params":5}`)), `"x"`)
		eo := decodeErrorMember(t, members)
		if eo.Code != -32602 || eo.Message != "params must be a JSON object" {
			t.Fatalf("got %d %q", eo.Code, eo.Message)
		}
	})

	t.Run("operation required, the daemon's verbatim message", func(t *testing.T) {
		for _, frame := range []string{
			`{"jsonrpc":"2.0","id":"x","method":"invoke.call"}`,
			`{"jsonrpc":"2.0","id":"x","method":"invoke.call","params":{}}`,
			`{"jsonrpc":"2.0","id":"x","method":"invoke.call","params":{"operation":7}}`,
			`{"jsonrpc":"2.0","id":"x","method":"invoke.call","params":{"operation":""}}`,
		} {
			members := assertResponseContract(t, p.respond([]byte(frame)), `"x"`)
			eo := decodeErrorMember(t, members)
			if eo.Code != -32602 || eo.Message != "operation (string) required" {
				t.Fatalf("%s: got %d %q", frame, eo.Code, eo.Message)
			}
		}
	})

	t.Run("non-object target and arguments deny in the broker's vocabulary", func(t *testing.T) {
		members := assertResponseContract(t, p.respond([]byte(`{"jsonrpc":"2.0","id":"x","method":"invoke.call","params":{"operation":"focus.echo","target":5}}`)), `"x"`)
		ro := decodeResultMember(t, members)
		if ro.Status != "denied" || ro.ReasonCode != "OPERATION_TARGET_INVALID" {
			t.Fatalf("target: got %+v", ro)
		}
		members = assertResponseContract(t, p.respond([]byte(`{"jsonrpc":"2.0","id":"x","method":"invoke.call","params":{"operation":"focus.echo","arguments":[1]}}`)), `"x"`)
		ro = decodeResultMember(t, members)
		if ro.Status != "denied" || ro.ReasonCode != "OPERATION_ARGUMENT_INVALID" {
			t.Fatalf("arguments: got %+v", ro)
		}
	})

	t.Run("unknown operation is a failed result, not method-not-found", func(t *testing.T) {
		members := assertResponseContract(t, p.respond(invokeFrame(`"x"`, "focus.missing", "{}")), `"x"`)
		ro := decodeResultMember(t, members)
		if ro.Status != "failed" || ro.ReasonCode != "OPERATION_NOT_FOUND" || ro.ReasonCodeSnake != "OPERATION_NOT_FOUND" {
			t.Fatalf("got %+v", ro)
		}
		if !strings.Contains(ro.Reason, "focus.missing") {
			t.Fatalf("reason should name the operation: %q", ro.Reason)
		}
	})
}

func TestRespondHandlerOutcomes(t *testing.T) {
	p := testPlugin()

	t.Run("success carries the arguments as operation_result", func(t *testing.T) {
		members := assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.echo", `{"hello":"world","n":3}`)), `"h1"`)
		ro := decodeResultMember(t, members)
		if ro.Status != "succeeded" {
			t.Fatalf("got %+v", ro)
		}
		if string(ro.OperationResult) != `{"hello":"world","n":3}` {
			t.Fatalf("operation_result: %s", ro.OperationResult)
		}
	})

	t.Run("absent arguments default to the empty object", func(t *testing.T) {
		members := assertResponseContract(t, p.respond([]byte(`{"jsonrpc":"2.0","id":"h1","method":"invoke.call","params":{"operation":"focus.echo"}}`)), `"h1"`)
		ro := decodeResultMember(t, members)
		if string(ro.OperationResult) != `{}` {
			t.Fatalf("operation_result: %s", ro.OperationResult)
		}
	})

	t.Run("nil result marshals as null", func(t *testing.T) {
		members := assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.nilresult", "{}")), `"h1"`)
		ro := decodeResultMember(t, members)
		if ro.Status != "succeeded" || string(ro.OperationResult) != "null" {
			t.Fatalf("got %+v %s", ro, ro.OperationResult)
		}
	})

	t.Run("Fail and Deny emit the three-spelling failure result", func(t *testing.T) {
		members := assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.fail", "{}")), `"h1"`)
		ro := decodeResultMember(t, members)
		if ro.Status != "failed" || ro.ReasonCode != "OPERATION_TARGET_INVALID" || ro.ReasonCodeSnake != "OPERATION_TARGET_INVALID" || ro.Reason != "the target is wrong" {
			t.Fatalf("got %+v", ro)
		}
		members = assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.deny", "{}")), `"h1"`)
		ro = decodeResultMember(t, members)
		// .
		// .
		if ro.Status != "denied" || ro.Reason != "MY_POLICY" || ro.ReasonCode != "MY_POLICY" {
			t.Fatalf("got %+v", ro)
		}
	})

	t.Run("a wrapped OperationError still classifies", func(t *testing.T) {
		members := assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.wrapped", "{}")), `"h1"`)
		ro := decodeResultMember(t, members)
		if ro.Status != "failed" || ro.ReasonCode != "WRAPPED_CODE" {
			t.Fatalf("got %+v", ro)
		}
	})

	t.Run("a host Denied returned by the handler becomes a denied result", func(t *testing.T) {
		members := assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.hostdenied", "{}")), `"h1"`)
		ro := decodeResultMember(t, members)
		if ro.Status != "denied" || ro.ReasonCode != "POLICY_DENY" {
			t.Fatalf("got %+v", ro)
		}
	})

	t.Run("plain errors answer the audited handler-failed object", func(t *testing.T) {
		members := assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.bug", "{}")), `"h1"`)
		eo := decodeErrorMember(t, members)
		if eo.Code != -32603 || eo.Message != "plugin handler failed" || eo.Data.ReasonCode != "PLUGIN_HANDLER_FAILED" {
			t.Fatalf("want the audited handler-failed object, got %+v", eo)
		}
	})

	t.Run("unmarshalable results answer handler-failed", func(t *testing.T) {
		members := assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.unmarshalable", "{}")), `"h1"`)
		eo := decodeErrorMember(t, members)
		if eo.Code != -32603 || eo.Data.ReasonCode != "PLUGIN_HANDLER_FAILED" {
			t.Fatalf("got %+v", eo)
		}
	})

	t.Run("an out-of-domain integer fails at the marshaller", func(t *testing.T) {
		// .
		// .
		members := assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.overint", "{}")), `"h1"`)
		eo := decodeErrorMember(t, members)
		if eo.Code != -32603 {
			t.Fatalf("got %+v", eo)
		}
	})

	t.Run("a raw smuggle outside the strict domain is swapped, not shipped", func(t *testing.T) {
		// .
		// .
		// .
		members := assertResponseContract(t, p.respond(invokeFrame(`"h1"`, "focus.smuggle", "{}")), `"h1"`)
		eo := decodeErrorMember(t, members)
		if eo.Code != -32603 {
			t.Fatalf("got %+v", eo)
		}
	})

	t.Run("an over-budget response is swapped before the frame wall", func(t *testing.T) {
		// .
		// .
		resp := p.respond(invokeFrame(`"h1"`, "focus.huge", "{}"))
		if len(resp) > MaxControlFrameBytes {
			t.Fatalf("library shipped an over-budget response (%d bytes)", len(resp))
		}
		members := assertResponseContract(t, resp, `"h1"`)
		eo := decodeErrorMember(t, members)
		if eo.Code != -32603 {
			t.Fatalf("got %+v", eo)
		}
	})
}

func TestRespondCurrent(t *testing.T) {
	saved := current
	defer func() { current = saved }()

	current = nil
	members := assertResponseContract(t, respondCurrent(invokeFrame(`"h1"`, "focus.echo", "{}")), `"h1"`)
	eo := decodeErrorMember(t, members)
	if eo.Code != -32603 || eo.Data.ReasonCode != "PLUGIN_NOT_REGISTERED" {
		t.Fatalf("got %+v", eo)
	}

	current = nil
	p := testPlugin()
	p.Run()
	if current != p {
		t.Fatalf("Run must register the plugin")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatalf("second Run must panic")
			}
		}()
		New("other").Run()
	}()
}

func TestOnEventDispatch(t *testing.T) {
	saved := current
	defer func() { current = saved }()

	// .
	// .
	current = nil
	dispatchEvent("focus.topic", []byte(`{"x":1}`))
	current = New("x")
	dispatchEvent("focus.topic", []byte(`{"x":1}`))

	var gotTopic string
	var gotPayload []byte
	p := New("x").OnEvent(func(topic string, payload []byte) {
		gotTopic, gotPayload = topic, payload
	})
	current = nil
	p.Run()
	dispatchEvent("focus.topic", []byte(`{"x":1}`))
	if gotTopic != "focus.topic" || string(gotPayload) != `{"x":1}` {
		t.Fatalf("sink saw %q %q", gotTopic, gotPayload)
	}
	// .
	// .
	dispatchEvent("bare", nil)
	if gotTopic != "bare" || gotPayload != nil {
		t.Fatalf("nil payload: %q %v", gotTopic, gotPayload)
	}
}

func TestBuilderPanics(t *testing.T) {
	for name, f := range map[string]func(){
		"empty operation": func() { New("x").Handle("", func(Call) (any, error) { return nil, nil }) },
		"nil handler":     func() { New("x").Handle("op", nil) },
		"duplicate handler": func() {
			p := New("x")
			h := func(Call) (any, error) { return nil, nil }
			p.Handle("op", h)
			p.Handle("op", h)
		},
	} {
		f := f
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("must panic")
				}
			}()
			f()
		})
	}
}

func TestCallAccessors(t *testing.T) {
	c := Call{Arguments: json.RawMessage(`{"a":1,"s":"x","b":true,"f":2.5,"o":{"k":"v"},"n":null,"e":1e2,"a-quoted":"42"}`), Target: nil}
	if v, ok := c.Args().Int("a"); !ok || v != 1 {
		t.Fatalf("Int: %v %v", v, ok)
	}
	if v, ok := c.Args().Int("e"); !ok || v != 100 {
		t.Fatalf("exponent-spelled integer: %v %v", v, ok)
	}
	if v, ok := c.Args().String("s"); !ok || v != "x" {
		t.Fatalf("String: %q %v", v, ok)
	}
	if v, ok := c.Args().Bool("b"); !ok || !v {
		t.Fatalf("Bool: %v %v", v, ok)
	}
	if v, ok := c.Args().Float("f"); !ok || v != 2.5 {
		t.Fatalf("Float: %v %v", v, ok)
	}
	if v, ok := c.Args().Object("o").String("k"); !ok || v != "v" {
		t.Fatalf("nested Object: %q %v", v, ok)
	}
	if !c.Args().Has("n") || c.Args().Has("absent") {
		t.Fatalf("Has: null member present, absent member not")
	}
	if _, ok := c.Args().String("a"); ok {
		t.Fatalf("a number is not a string")
	}
	if _, ok := c.Args().Int("f"); ok {
		t.Fatalf("2.5 is not an integer")
	}
	// .
	// .
	// .
	if v, ok := c.Args().Int("a-quoted"); !ok || v != 42 {
		// .
		t.Fatalf("quoted integer 42: %v %v", v, ok)
	}
	// .
	if _, ok := c.TargetObject().String("key"); ok {
		t.Fatalf("nil target must read as absent")
	}
}

func TestHandlerSeesCopies(t *testing.T) {
	// .
	// .
	var stowed json.RawMessage
	p := New("x")
	p.Handle("op", func(c Call) (any, error) {
		stowed = c.Arguments
		return nil, nil
	})
	frame := invokeFrame(`"h1"`, "op", `{"k":"v"}`)
	p.respond(frame)
	for i := range frame {
		frame[i] = 0
	}
	if string(stowed) != `{"k":"v"}` {
		t.Fatalf("handler arguments alias the request buffer: %q", stowed)
	}
	if !bytes.Equal(stowed, []byte(`{"k":"v"}`)) {
		t.Fatalf("stowed mutated")
	}
}
