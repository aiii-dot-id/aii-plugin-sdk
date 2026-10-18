package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// serveEnv starts this binary as the adapter instead of as the tests.
const serveEnv = "AIISDK_EXAMPLE_TWILIO_SERVE"

func TestMain(m *testing.M) {
	if os.Getenv(serveEnv) == "1" {
		if err := adapter.Serve(""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// callClock is the host's clock in the tests, in milliseconds since the epoch.
const callClock = 1_790_000_000_000

// standIn answers the adapter's host calls: its settings, its kv (nil: not
// granted) and Twilio's Messages endpoint.
type standIn struct {
	t      *testing.T
	kv     map[string]string
	extra  map[string]any
	twilio func() (status int, body string)
	posts  int
}

func result(or any) json.RawMessage {
	b, _ := json.Marshal(map[string]any{"success": true, "ok": true, "status": "succeeded", "operation_result": or, "external_receipt": map[string]any{"host_authored": true}})
	return b
}

func (h *standIn) answer(params json.RawMessage) (json.RawMessage, json.RawMessage) {
	var p struct {
		Operation string          `json:"operation"`
		Target    json.RawMessage `json:"target"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		h.t.Fatalf("a host call that is not JSON: %s", params)
	}
	var target struct{ Key, URL string }
	_ = json.Unmarshal(p.Target, &target)
	denied := func(code string) (json.RawMessage, json.RawMessage) {
		b, _ := json.Marshal(map[string]any{"code": -32000, "message": "not granted here", "data": map[string]string{"reasonCode": code}})
		return nil, b
	}
	switch p.Operation {
	case "settings.get":
		values := map[string]any{"account_sid": "AC1", "from_number": "+15550000", "auth_token": "tw"}
		for k, v := range h.extra {
			values[k] = v
		}
		return result(map[string]any{"values": values}), nil
	case "kv.get", "kv.put":
		if h.kv == nil {
			return denied("POLICY_DENY")
		}
		if p.Operation == "kv.get" {
			v, ok := h.kv[target.Key]
			if !ok {
				b, _ := json.Marshal(map[string]any{"success": false, "ok": false, "status": "failed", "reason": "KV_NOT_FOUND", "reasonCode": "KV_NOT_FOUND", "reason_code": "KV_NOT_FOUND", "external_receipt": map[string]any{}})
				return b, nil
			}
			return result(map[string]any{"key": target.Key, "value": v}), nil
		}
		var a struct{ Value string }
		_ = json.Unmarshal(p.Arguments, &a)
		h.kv[target.Key] = a.Value
		return result(map[string]any{"stored": true, "key": target.Key, "value_bytes": len(a.Value), "scope": "persistent"}), nil
	case "http.post":
		h.posts++
		status, body := h.twilio()
		if status == 0 { // the answer was lost after the write
			b, _ := json.Marshal(map[string]any{"success": false, "ok": false, "status": "failed", "reason": "NET_EFFECT_UNKNOWN", "reasonCode": "NET_EFFECT_UNKNOWN", "reason_code": "NET_EFFECT_UNKNOWN", "external_receipt": map[string]any{"host_authored": true, "effect": "unknown"}})
			return b, nil
		}
		var or any = map[string]any{"http_status": status}
		if body != "" {
			or = map[string]any{"http_status": status, "body": json.RawMessage(body)}
		}
		if status >= 400 { // the broker reads a 4xx as a failed remote outcome, evidence attached
			b, _ := json.Marshal(map[string]any{"success": false, "ok": false, "status": "failed", "reason": "NET_REMOTE_OUTCOME_FAILED", "reasonCode": "NET_REMOTE_OUTCOME_FAILED", "reason_code": "NET_REMOTE_OUTCOME_FAILED", "operation_result": or, "external_receipt": map[string]any{"host_authored": true, "effect": "performed"}})
			return b, nil
		}
		return result(or), nil
	}
	h.t.Fatalf("the adapter asked the host for %s", p.Operation)
	return nil, nil
}

type reply struct {
	status, code, reason string
	result               json.RawMessage
}

type proc struct {
	t      *testing.T
	in     io.WriteCloser
	frames chan []byte
	n      int
}

func start(t *testing.T) *proc {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), serveEnv+"=1")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &proc{t: t, in: in, frames: make(chan []byte, 16)}
	go func() {
		defer close(p.frames)
		for {
			f, err := sdk.ReadFrame(out, sdk.MaxControlFrameBytes)
			if err != nil {
				return
			}
			p.frames <- f
		}
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	return p
}

// invoke drives one operation as the host does, with the host's clock.
func (p *proc) invoke(h *standIn, op string, args map[string]any) reply {
	p.t.Helper()
	p.n++
	if _, ok := args["_host_now_ms"]; !ok {
		args["_host_now_ms"] = callClock
	}
	a, _ := json.Marshal(args)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"invoke.call","params":{"operation":%q,"arguments":%s}}`, p.n, op, a)
	if err := sdk.WriteFrame(p.in, []byte(req), sdk.MaxControlFrameBytes); err != nil {
		p.t.Fatal(err)
	}
	for {
		var raw []byte
		select {
		case f, ok := <-p.frames:
			if !ok {
				p.t.Fatalf("invoke %s: the adapter's process ended", op)
			}
			raw = f
		case <-time.After(10 * time.Second):
			p.t.Fatalf("invoke %s: no frame within ten seconds", op)
		}
		var f struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(raw, &f); err != nil {
			p.t.Fatalf("a frame that is not JSON: %s", raw)
		}
		if f.Method == "" {
			var r struct {
				Status, ReasonCode, Reason string
				OperationResult            json.RawMessage `json:"operation_result"`
			}
			if f.Error != nil {
				return reply{status: "error"}
			}
			_ = json.Unmarshal(f.Result, &r)
			return reply{status: r.Status, code: r.ReasonCode, reason: r.Reason, result: r.OperationResult}
		}
		res, errObj := h.answer(f.Params)
		member, value := "result", res
		if errObj != nil {
			member, value = "error", errObj
		}
		msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,%q:%s}`, f.ID, member, value)
		if err := sdk.WriteFrame(p.in, []byte(msg), sdk.MaxControlFrameBytes); err != nil {
			p.t.Fatal(err)
		}
	}
}

func text(a string) map[string]any { return map[string]any{"address": "+15550002", "body": a} }

// A send posts one message to Twilio and answers its sid.
func TestASendAnswersTheMessageSid(t *testing.T) {
	h := &standIn{t: t, kv: map[string]string{}, twilio: func() (int, string) { return 201, `{"sid":"SM42"}` }}
	got := start(t).invoke(h, "send", text("hello"))
	if got.status != "succeeded" || !strings.Contains(string(got.result), `"receipt":"SM42"`) || h.posts != 1 {
		t.Fatalf("send: %+v (%d posts)", got, h.posts)
	}
}

// The channel's sends are bounded: send_limit in any send_window_minutes, both
// settings on the plugin's card. The send over the limit is refused before
// Twilio is asked, saying which limit, where to change it and when the next may
// go; without kv there is no window, and nothing is sent.
func TestSendsAreBoundedByTheirWindow(t *testing.T) {
	h := &standIn{t: t, kv: map[string]string{}, extra: map[string]any{"send_limit": 2, "send_window_minutes": 5},
		twilio: func() (int, string) { return 201, `{"sid":"SM1"}` }}
	p := start(t)
	const min = 60_000
	at := func(ms int64) map[string]any { a := text("hello"); a["_host_now_ms"] = ms; return a }
	for i, ms := range []int64{callClock, callClock + 2*min} {
		if got := p.invoke(h, "send", at(ms)); got.status != "succeeded" {
			t.Fatalf("send %d: %+v", i+1, got)
		}
	}
	got := p.invoke(h, "send", at(callClock+3*min))
	leaves := time.UnixMilli(callClock + 5*min).UTC().Format(time.RFC3339)
	if got.status != "failed" || got.code != "NET_REMOTE_FAILED" ||
		!strings.Contains(got.reason, "this channel has sent 2 in the last 5 minutes, its limit — the operator sets it as this plugin's send_limit and send_window_minutes settings; the next send can go at "+leaves+" — the message was not sent") || h.posts != 2 {
		t.Fatalf("the third: %+v (%d posts)", got, h.posts)
	}
	if got := p.invoke(h, "send", at(callClock+5*min+1)); got.status != "succeeded" {
		t.Errorf("when the first has left the window: %+v", got)
	}

	// Twilio refusing a send takes no place; a lost answer does.
	h2 := &standIn{t: t, kv: map[string]string{}, extra: map[string]any{"send_limit": 1, "send_window_minutes": 5},
		twilio: func() (int, string) { return 400, `{"code":21211,"message":"Invalid 'To' Phone Number"}` }}
	if got := p.invoke(h2, "send", text("a")); got.status == "succeeded" {
		t.Fatalf("Twilio refusing: %+v", got)
	}
	h2.twilio = func() (int, string) { return 201, `{"sid":"SM2"}` }
	if got := p.invoke(h2, "send", text("b")); got.status != "succeeded" {
		t.Errorf("a send Twilio refused took no place: %+v", got)
	}

	h4 := &standIn{t: t, kv: map[string]string{}, extra: map[string]any{"send_limit": 1, "send_window_minutes": 5},
		twilio: func() (int, string) { return 0, "" }}
	if got := p.invoke(h4, "send", text("d")); got.code != "NET_EFFECT_UNKNOWN" {
		t.Fatalf("a lost answer: %+v", got)
	}
	h4.twilio = func() (int, string) { return 201, `{"sid":"SM4"}` }
	if got := p.invoke(h4, "send", text("e")); got.status != "failed" || !strings.Contains(got.reason, "its limit") || h4.posts != 1 {
		t.Errorf("after a lost answer the window is full: %+v (%d posts)", got, h4.posts)
	}

	h3 := &standIn{t: t, twilio: func() (int, string) { return 201, `{"sid":"SM3"}` }}
	if got := p.invoke(h3, "send", text("c")); got.status != "denied" || h3.posts != 0 {
		t.Errorf("without kv: %+v (%d posts)", got, h3.posts)
	}
}
