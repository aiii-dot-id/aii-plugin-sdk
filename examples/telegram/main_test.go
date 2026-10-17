package main

// The adapter against its host's contract, over the native lane: this
// test binary re-executes itself as the adapter (TestMain), so each case
// drives the same handlers the guest carries, behind the kit's own
// dispatch, over framed BBB — and a case can kill the adapter's process
// and revive it, which is what a kill between the adapter's return and
// the host's record needs.
//
// The host is a stand-in and Telegram is scripted behind the broker's own
// reply vocabulary (aii-os internal/broker/broker.go: successResult,
// failureResult, externalReceipt). Its getUpdates keeps the Bot API's
// documented promise: "An update is considered confirmed as soon as
// getUpdates is called with an offset higher than its update_id", and a
// confirmed update is gone.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// serveEnv starts this binary as the adapter instead of as the tests.
const serveEnv = "AIISDK_EXAMPLE_TELEGRAM_SERVE"

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

// ---- the broker's replies ----

// brokerReply is the host's answer to one call the adapter makes: a
// result, or a JSON-RPC error object (a denial).
type brokerReply struct{ result, errObj json.RawMessage }

func receipt(effect string) map[string]any {
	r := map[string]any{"host_authored": true}
	if effect != "" {
		r["effect"] = effect
	}
	return r
}

func succeededWith(or any, effect string) brokerReply {
	b, _ := json.Marshal(map[string]any{"success": true, "ok": true, "status": "succeeded", "operation_result": or, "external_receipt": receipt(effect)})
	return brokerReply{result: b}
}

func failedWith(code string, or any, effect string) brokerReply {
	m := map[string]any{"success": false, "ok": false, "status": "failed", "reason": code, "reasonCode": code, "reason_code": code, "external_receipt": receipt(effect)}
	if or != nil {
		m["operation_result"] = or
	}
	b, _ := json.Marshal(m)
	return brokerReply{result: b}
}

func deniedBy(code string) brokerReply {
	b, _ := json.Marshal(map[string]any{"code": -32000, "message": "not granted here", "data": map[string]string{"reasonCode": code}})
	return brokerReply{errObj: b}
}

// answered is an exchange Telegram answered, read whole by the broker:
// from 400 a failed remote outcome with the evidence attached, performed.
// retryAfter is the host's reading of a Retry-After header; -1 is none.
func answered(status int, body string, retryAfter int64) brokerReply {
	or := map[string]any{"http_status": status}
	switch {
	case body == "":
		or["body"] = nil
	case json.Valid([]byte(body)):
		or["body"] = json.RawMessage(body)
	default:
		or["body"] = body
	}
	if retryAfter >= 0 {
		or["retry_after_s"] = retryAfter
	}
	if status >= 400 {
		return failedWith("NET_REMOTE_OUTCOME_FAILED", or, "performed")
	}
	return succeededWith(or, "performed")
}

// ---- the stand-in host ----

type httpCall struct {
	op   string
	url  string
	args map[string]any
}

func (c httpCall) query() url.Values {
	_, q, _ := strings.Cut(c.url, "?")
	v, _ := url.ParseQuery(q)
	return v
}

// standIn answers settings.get with the bot_token handle, keeps kv the
// way the host's store is kept — outliving any one adapter process; nil
// is a host that grants none — sends every http.* to telegram, and
// records arrivals once by id, as the host's inbound record does
// (ON CONFLICT(id) DO NOTHING).
type standIn struct {
	t        *testing.T
	handle   string
	kv       map[string]string
	kvWrites []string
	telegram func(httpCall) brokerReply
	calls    []httpCall
	recorded map[string]string
	extra    map[string]any // more settings values (the send window's)
}

func (h *standIn) answer(params json.RawMessage) brokerReply {
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
	switch p.Operation {
	case "settings.get":
		values := map[string]any{}
		if h.handle != "" {
			values["bot_token"] = h.handle
		}
		for k, v := range h.extra {
			values[k] = v
		}
		return succeededWith(map[string]any{"values": values}, "")
	case "kv.get":
		if h.kv == nil {
			return deniedBy("POLICY_DENY")
		}
		v, ok := h.kv[target.Key]
		if !ok {
			return failedWith("KV_NOT_FOUND", nil, "")
		}
		return succeededWith(map[string]any{"key": target.Key, "value": v}, "")
	case "kv.put":
		if h.kv == nil {
			return deniedBy("POLICY_DENY")
		}
		var a struct{ Value string }
		_ = json.Unmarshal(p.Arguments, &a)
		h.kv[target.Key] = a.Value
		h.kvWrites = append(h.kvWrites, a.Value)
		return succeededWith(map[string]any{"stored": true, "key": target.Key, "value_bytes": len(a.Value), "scope": "persistent"}, "")
	case "http.get", "http.post":
		var a map[string]any
		_ = json.Unmarshal(p.Arguments, &a)
		c := httpCall{op: p.Operation, url: target.URL, args: a}
		h.calls = append(h.calls, c)
		return h.telegram(c)
	}
	h.t.Errorf("the adapter asked the host for %s", p.Operation)
	return deniedBy("OPERATION_NOT_ALLOWED")
}

// record is the host's inbound record: each arrival once by id. It
// answers how many were new.
func (h *standIn) record(r reply) int {
	h.t.Helper()
	if r.status != "succeeded" {
		h.t.Fatalf("receive: %s %s: %s", r.status, r.code, r.reason)
	}
	var arrivals []struct{ ID, From, Body string }
	if err := json.Unmarshal(r.result, &arrivals); err != nil {
		h.t.Fatalf("receive answered no list of arrivals: %s", r.result)
	}
	if h.recorded == nil {
		h.recorded = map[string]string{}
	}
	fresh := 0
	for _, a := range arrivals {
		if _, have := h.recorded[a.ID]; !have {
			h.recorded[a.ID] = a.Body
			fresh++
		}
	}
	return fresh
}

// ---- the adapter's process ----

// reply is the adapter's answer to one operation, as the host reads it.
type reply struct {
	status string // succeeded | failed | denied; error for an error object
	code   string
	reason string
	result json.RawMessage
}

// line is what the host makes of a failure for the identity and the
// operator (aii-os pluginhost decodeReply).
func (r reply) line() string {
	return fmt.Sprintf("operation %s: %s (%s)", r.status, r.reason, r.code)
}

type adapterProc struct {
	t      *testing.T
	cmd    *exec.Cmd
	in     io.WriteCloser
	frames chan []byte
	n      int
}

func startAdapter(t *testing.T) *adapterProc {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), serveEnv+"=1")
	cmd.Stderr = os.Stderr
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
	p := &adapterProc{t: t, cmd: cmd, in: in, frames: make(chan []byte, 16)}
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
	t.Cleanup(p.kill)
	return p
}

// kill ends the process as a crash does: no end of input, nothing
// finished, its memory gone.
func (p *adapterProc) kill() {
	if p.cmd.ProcessState != nil {
		return
	}
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
}

// invoke drives one operation the way the host does — an invoke.call
// frame — and answers each call the adapter makes back through h, until
// the adapter replies.
func (p *adapterProc) invoke(h *standIn, op string, args map[string]any) reply {
	p.t.Helper()
	p.n++
	// The host injects its clock into every call (`_host_now_ms`); a test that
	// names one keeps it.
	args = withClock(args)
	a, _ := json.Marshal(args)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"invoke.call","params":{"operation":%q,"arguments":%s}}`, p.n, op, a)
	if err := sdk.WriteFrame(p.in, []byte(req), sdk.MaxControlFrameBytes); err != nil {
		p.t.Fatalf("invoke %s: %v", op, err)
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
			p.t.Fatalf("invoke %s: a frame that is not JSON: %s", op, raw)
		}
		if f.Method == "" {
			return decodeReply(p.t, f.Result, f.Error)
		}
		r := h.answer(f.Params)
		member, value := "result", r.result
		if r.errObj != nil {
			member, value = "error", r.errObj
		}
		back := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,%q:%s}`, f.ID, member, value)
		if err := sdk.WriteFrame(p.in, []byte(back), sdk.MaxControlFrameBytes); err != nil {
			p.t.Fatalf("invoke %s: answer %s: %v", op, f.Method, err)
		}
	}
}

func decodeReply(t *testing.T, result, errObj json.RawMessage) reply {
	t.Helper()
	if errObj != nil {
		var e struct {
			Message string
			Data    struct {
				ReasonCode string `json:"reasonCode"`
			}
		}
		_ = json.Unmarshal(errObj, &e)
		return reply{status: "error", code: e.Data.ReasonCode, reason: e.Message}
	}
	var r struct {
		Status          string
		Reason          string
		ReasonCode      string          `json:"reasonCode"`
		OperationResult json.RawMessage `json:"operation_result"`
	}
	if err := json.Unmarshal(result, &r); err != nil {
		t.Fatalf("a reply that is not a result: %s", result)
	}
	return reply{status: r.Status, code: r.ReasonCode, reason: r.Reason, result: r.OperationResult}
}

// ---- send ----

const sentMessage = `{"message_id":7,"from":{"id":999,"is_bot":true,"first_name":"bot"},"chat":{"id":42,"type":"private","first_name":"Recipient"},"date":1759000000,"text":"hello"}`

// Every answer Telegram can give a send, and what the adapter makes of it:
// the code the host acts on, and the words the identity reads.
func TestSendAnswersWhatTelegramAccepted(t *testing.T) {
	cases := []struct {
		name     string
		telegram brokerReply
		status   string
		code     string
		says     string
	}{
		{"a message Telegram took answers its receipt, chat:message_id",
			answered(200, `{"ok":true,"result":`+sentMessage+`}`, -1), "succeeded", "", `"receipt":"42:7"`},
		{"a message Telegram scheduled (message_id 0) is still its receipt",
			answered(200, `{"ok":true,"result":{"message_id":0,"chat":{"id":42,"type":"private"},"date":1}}`, -1), "succeeded", "", `"receipt":"42:0"`},
		{"a chat Telegram cannot find cannot take a message",
			answered(400, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`, -1), "failed", "OPERATION_ARGUMENT_INVALID", "will not deliver to this chat (400: Bad Request: chat not found)"},
		{"a person who blocked the bot cannot take a message",
			answered(403, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`, -1), "failed", "OPERATION_ARGUMENT_INVALID", "(403: Forbidden: bot was blocked by the user)"},
		{"a group that became a supergroup names its new chat",
			answered(400, `{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-1001234567890}}`, -1), "failed", "OPERATION_ARGUMENT_INVALID", "the group is now chat -1001234567890"},
		{"a 400 about the message is this send refused, not the address",
			answered(400, `{"ok":false,"error_code":400,"description":"Bad Request: message is too long"}`, -1), "failed", "NET_REMOTE_FAILED", "Telegram refused it (400: Bad Request: message is too long) — the message was not sent"},
		{"a refused bot token accepted nothing",
			answered(401, `{"ok":false,"error_code":401,"description":"Unauthorized"}`, -1), "failed", "NET_REMOTE_FAILED", "(401: Unauthorized)"},
		{"a 404 accepted nothing",
			answered(404, `{"ok":false,"error_code":404,"description":"Not Found"}`, -1), "failed", "NET_REMOTE_FAILED", "(404: Not Found)"},
		{"a 429 accepted nothing, and the wait is Telegram's own word over the header's",
			answered(429, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 35","parameters":{"retry_after":35}}`, 99), "failed", "NET_REMOTE_FAILED", "and asks to wait 35 s — the message was not sent"},
		{"a 429 with only a Retry-After header carries the host's reading of it",
			answered(429, `{"ok":false,"error_code":429,"description":"Too Many Requests"}`, 30), "failed", "NET_REMOTE_FAILED", "and asks to wait 30 s"},
		{"a 429 with no hint says so, never zero",
			answered(429, `{"ok":false,"error_code":429,"description":"Too Many Requests"}`, -1), "failed", "NET_REMOTE_FAILED", "and named no time to wait"},
		{"a 500 can follow acceptance: the effect is unknown",
			answered(500, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`, -1), "failed", "NET_EFFECT_UNKNOWN", "Telegram answered 500: Internal Server Error — the message may have gone; do not resend blindly"},
		{"a 502 from the front, not even Telegram's JSON: unknown",
			answered(502, `<html>Bad Gateway</html>`, -1), "failed", "NET_EFFECT_UNKNOWN", "Telegram answered 502 — the message may have gone"},
		{"a response lost after the write is unknown",
			failedWith("NET_EFFECT_UNKNOWN", nil, "unknown"), "failed", "NET_EFFECT_UNKNOWN", "the send was written and the response was lost"},
		{"a 200 that cannot be read is unknown",
			answered(200, `<html>ok</html>`, -1), "failed", "NET_EFFECT_UNKNOWN", "Telegram answered 200 and the answer could not be read — the message may have gone"},
		{"a 200 whose message names no chat is unknown, never a receipt",
			answered(200, `{"ok":true,"result":{"message_id":7}}`, -1), "failed", "NET_EFFECT_UNKNOWN", "could not be read"},
		{"ok:false on a 200 is Telegram refusing it",
			answered(200, `{"ok":false,"description":"Bad Request: something else"}`, -1), "failed", "NET_REMOTE_FAILED", "Telegram refused it (200: Bad Request: something else)"},
		{"a request the receipt says was never written reached no one",
			failedWith("NET_REMOTE_OUTCOME_FAILED", nil, "not-performed"), "failed", "NET_REMOTE_FAILED", "Telegram was not reached — the message was not sent"},
		{"an answer too large for the host was an answer: the host's own code, which it reads as unknown",
			failedWith("NET_RESPONSE_TOO_LARGE", nil, "performed"), "failed", "NET_RESPONSE_TOO_LARGE", "NET_RESPONSE_TOO_LARGE"},
		{"the host's denial is relayed with its code",
			deniedBy("POLICY_DENY"), "denied", "POLICY_DENY", "grant net.outbound:api.telegram.org:443"},
		{"a host reply that states no outcome is unknown: nothing says which",
			brokerReply{result: json.RawMessage(`{"operation_result":{"http_status":200}}`)}, "failed", "NET_EFFECT_UNKNOWN", "the host's answer to the send could not be read"},
	}
	var next brokerReply
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: func(httpCall) brokerReply { return next }}
	p := startAdapter(t)
	for _, tc := range cases {
		next = tc.telegram
		h.calls = nil
		got := p.invoke(h, "send", map[string]any{"address": "42", "body": "hello"})
		said := got.reason
		if got.status == "succeeded" {
			said = string(got.result)
		}
		if got.status != tc.status || got.code != tc.code || !strings.Contains(said, tc.says) {
			t.Errorf("%s:\n  got  %s %s: %s\n  want %s %s, saying %q", tc.name, got.status, got.code, said, tc.status, tc.code, tc.says)
		}
		if len(h.calls) != 1 {
			t.Errorf("%s: %d sendMessage calls, want exactly one", tc.name, len(h.calls))
		}
		if got.status != "succeeded" {
			t.Logf("%-78s %s", tc.name, got.line())
		}
	}
}

// One message, one write, never repeated by the broker: the request the
// adapter makes, and the bodies it refuses before making any.
func TestSendWritesOneMessage(t *testing.T) {
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: func(httpCall) brokerReply {
		return answered(200, `{"ok":true,"result":`+sentMessage+`}`, -1)
	}}
	p := startAdapter(t)
	if got := p.invoke(h, "send", map[string]any{"address": "@recipient_bot_test", "body": "hello"}); got.status != "succeeded" {
		t.Fatalf("send: %s", got.line())
	}
	if len(h.calls) != 1 {
		t.Fatalf("%d calls, want one", len(h.calls))
	}
	c := h.calls[0]
	var body map[string]any
	_ = json.Unmarshal([]byte(fmt.Sprint(c.args["body"])), &body)
	switch {
	case c.op != "http.post" || c.url != "https://api.telegram.org/bot{credential}/sendMessage":
		t.Errorf("the send is %s %s", c.op, c.url)
	case body["chat_id"] != "@recipient_bot_test" || body["text"] != "hello" || c.args["content_type"] != "application/json":
		t.Errorf("the request carries %v as %v", body, c.args["content_type"])
	case c.args["auth_profile"] != "tg":
		t.Errorf("the token is not the bot_token handle: %v", c.args["auth_profile"])
	case c.args["idempotent"] != nil:
		t.Errorf("a send declared idempotent would be written twice by the broker on a transport failure: %v", c.args)
	}

	for _, tc := range []struct {
		name, body, says string
	}{
		{"a body over Telegram's 4096 characters", strings.Repeat("a", maxText+1), "1 to 4096 characters and this one has 4097"},
		{"an empty body", "", "this one has 0"},
	} {
		h.calls = nil
		got := p.invoke(h, "send", map[string]any{"address": "42", "body": tc.body})
		if got.code != "OPERATION_ARGUMENT_INVALID" || !strings.Contains(got.reason, tc.says) || len(h.calls) != 0 {
			t.Errorf("%s: %s after %d calls; want OPERATION_ARGUMENT_INVALID saying %q before any", tc.name, got.line(), len(h.calls), tc.says)
		}
	}
	// The count is characters, as the input schema's maxLength counts
	// them: 4096 of a two-byte letter is a message Telegram carries.
	h.calls = nil
	if got := p.invoke(h, "send", map[string]any{"address": "42", "body": strings.Repeat("я", maxText)}); got.status != "succeeded" || len(h.calls) != 1 {
		t.Errorf("4096 characters of two bytes each: %s", got.line())
	}
}

// No operation of a channel adapter may wait for the operator: the host
// drives all three, and refuses the activation of an adapter that
// declares operator_confirms on any (aii-os pluginhost loadDescriptors).
func TestNoOperationWaitsForTheOperator(t *testing.T) {
	raw, err := adapter.DescriptorsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var ops []map[string]any
	if err := json.Unmarshal(raw, &ops); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, op := range ops {
		names = append(names, fmt.Sprint(op["id"]))
		if _, confirms := op["operator_confirms"]; confirms {
			t.Errorf("%s declares operator_confirms: the host refuses a channel adapter that does", op["id"])
		}
	}
	if !reflect.DeepEqual(names, []string{"describe", "receive", "send"}) {
		t.Errorf("the adapter declares %v, want describe, receive, send", names)
	}
}

// ---- receive ----

// telegramQueue is getUpdates as the Bot API documents it: updates wait
// until a poll asks for a later offset, and are then gone for good.
type telegramQueue struct {
	pending []queued
	asked   []int64 // the offset each poll asked from
	instead []brokerReply
}

type queued struct {
	id  int64
	msg string
}

func message(chat, id int64, text string) string {
	return fmt.Sprintf(`{"message_id":%d,"from":{"id":%d,"is_bot":false,"first_name":"Recipient"},"chat":{"id":%d,"type":"private"},"date":1759000000,"text":%q}`, id, chat, chat, text)
}

func (q *telegramQueue) serve(c httpCall) brokerReply {
	query := c.query()
	offset, _ := strconv.ParseInt(query.Get("offset"), 10, 64)
	q.asked = append(q.asked, offset)
	if len(q.instead) > 0 {
		r := q.instead[0]
		q.instead = q.instead[1:]
		return r
	}
	if offset > 0 {
		kept := q.pending[:0]
		for _, u := range q.pending {
			if u.id >= offset {
				kept = append(kept, u)
			}
		}
		q.pending = kept
	}
	limit := 100
	if n, err := strconv.Atoi(query.Get("limit")); err == nil && n > 0 {
		limit = n
	}
	var parts []string
	for i, u := range q.pending {
		if i == limit {
			break
		}
		parts = append(parts, fmt.Sprintf(`{"update_id":%d,"message":%s}`, u.id, u.msg))
	}
	return answered(200, `{"ok":true,"result":[`+strings.Join(parts, ",")+`]}`, -1)
}

// Each message becomes one arrival: the id is the chat and the message,
// never the update; from is the chat; a message with no text is still
// someone writing. The poll asks for one update, long, for messages.
func TestReceiveMapsEachMessage(t *testing.T) {
	q := &telegramQueue{pending: []queued{
		{100, message(42, 7, "are you there?")},
		{101, `{"message_id":8,"chat":{"id":42,"type":"private"},"date":1,"photo":[{"file_id":"x","file_unique_id":"x","width":1,"height":1}],"caption":"look"}`},
		{102, `{"message_id":9,"chat":{"id":-1005,"type":"supergroup","title":"g"},"from":{"id":42,"is_bot":false,"first_name":"Recipient"},"date":1,"photo":[{"file_id":"y","file_unique_id":"y","width":1,"height":1}]}`},
	}}
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: q.serve}
	p := startAdapter(t)
	want := [][]string{{"42:7", "42", "are you there?"}, {"42:8", "42", "look"}, {"-1005:9", "-1005", "[photo]"}}
	for i, w := range want {
		got := p.invoke(h, "receive", map[string]any{})
		var arrivals []struct{ ID, From, Body string }
		_ = json.Unmarshal(got.result, &arrivals)
		if got.status != "succeeded" || len(arrivals) != 1 || arrivals[0].ID != w[0] || arrivals[0].From != w[1] || arrivals[0].Body != w[2] {
			t.Errorf("poll %d: %s %s, want one arrival %v", i+1, got.status, got.result, w)
		}
	}
	got := p.invoke(h, "receive", map[string]any{})
	if got.status != "succeeded" || string(got.result) != "[]" {
		t.Errorf("an empty poll: %s %s, want []", got.status, got.result)
	}
	if want := []int64{0, 101, 102, 103}; !reflect.DeepEqual(q.asked, want) {
		t.Errorf("the polls asked from %v, want %v", q.asked, want)
	}
	c := h.calls[0]
	query := c.query()
	switch {
	case !strings.HasPrefix(c.url, "https://api.telegram.org/bot{credential}/getUpdates?"):
		t.Errorf("the poll is %s", c.url)
	case query.Get("timeout") != "9" || query.Get("limit") != "1" || query.Get("allowed_updates") != `["message"]`:
		t.Errorf("the poll asks %v; want timeout 9, limit 1, messages only", query)
	case c.args["auth_profile"] != "tg" || c.args["timeout_ms"] != float64(9500):
		t.Errorf("the poll is bounded by %v with %v", c.args["timeout_ms"], c.args["auth_profile"])
	}
}

// A poll that fails takes nothing: the next asks from the same offset,
// and the update it did not get arrives then.
func TestAFailedPollLosesNothing(t *testing.T) {
	failures := []struct {
		name  string
		reply brokerReply
		says  string
	}{
		{"another poller holds the bot", answered(409, `{"ok":false,"error_code":409,"description":"Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"}`, -1), "getUpdates failed (409: Conflict"},
		{"a 200 that is ok:false", answered(200, `{"ok":false,"description":"Bad Request: wrong offset"}`, -1), "getUpdates failed (200: Bad Request: wrong offset)"},
		{"a 200 that cannot be read", answered(200, `<html>`, -1), "getUpdates failed (200)"},
		{"a 502", answered(502, ``, -1), "getUpdates failed (502)"},
		{"a lost answer", failedWith("NET_EFFECT_UNKNOWN", nil, "unknown"), "NET_EFFECT_UNKNOWN"},
		{"an answer the host would not take", failedWith("NET_RESPONSE_TOO_LARGE", nil, "performed"), "NET_RESPONSE_TOO_LARGE"},
	}
	for _, f := range failures {
		q := &telegramQueue{pending: []queued{{100, message(42, 7, "hello")}}, instead: []brokerReply{f.reply}}
		h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: q.serve}
		p := startAdapter(t)
		if got := p.invoke(h, "receive", map[string]any{}); got.status != "failed" || !strings.Contains(got.line(), f.says) {
			t.Errorf("%s: %s, want a failure saying %q", f.name, got.line(), f.says)
		}
		if fresh := h.record(p.invoke(h, "receive", map[string]any{})); fresh != 1 || h.recorded["42:7"] != "hello" {
			t.Errorf("%s: the next poll recorded %v", f.name, h.recorded)
		}
		if !reflect.DeepEqual(q.asked, []int64{0, 0}) || len(h.kvWrites) != 0 {
			t.Errorf("%s: the polls asked from %v and wrote %v to kv; want 0 twice and nothing", f.name, q.asked, h.kvWrites)
		}
		p.kill()
	}
}

// The offset an answer advances to is asked for on the next poll, and
// made durable only when the host asks again — its word that it has
// recorded what the last call returned.
func TestTheOffsetIsCommittedOnlyByTheNextCall(t *testing.T) {
	q := &telegramQueue{pending: []queued{{100, message(42, 7, "first")}, {101, message(42, 8, "second")}}}
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: q.serve}
	p := startAdapter(t)
	h.record(p.invoke(h, "receive", map[string]any{}))
	if len(h.kvWrites) != 0 {
		t.Fatalf("kv was written %v before the host asked again", h.kvWrites)
	}
	h.record(p.invoke(h, "receive", map[string]any{}))
	if !reflect.DeepEqual(h.kvWrites, []string{"101"}) || !reflect.DeepEqual(q.asked, []int64{0, 101}) {
		t.Fatalf("kv writes %v and polls from %v; want 101 written as the poll from 101 began", h.kvWrites, q.asked)
	}
}

// THE KILL. The adapter returns an arrival and its process dies before
// the host records it: the revived adapter asks from the offset kv
// holds, Telegram — never asked for a later one — gives the update again,
// and the host records it. A kill after the record replays an update the
// host already has, and the record by id takes it once.
func TestAKillBetweenReturnAndRecordLosesNothing(t *testing.T) {
	q := &telegramQueue{pending: []queued{{100, message(42, 7, "first")}, {101, message(42, 8, "second")}}}
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: q.serve}

	first := startAdapter(t)
	if got := first.invoke(h, "receive", map[string]any{}); !strings.Contains(string(got.result), `"42:7"`) {
		t.Fatalf("the first poll returned %s %s", got.status, got.result)
	}
	first.kill() // before the host recorded 42:7

	revived := startAdapter(t)
	if fresh := h.record(revived.invoke(h, "receive", map[string]any{})); fresh != 1 || h.recorded["42:7"] != "first" {
		t.Fatalf("after the kill the revived adapter recorded %v: 42:7 was lost", h.recorded)
	}
	if fresh := h.record(revived.invoke(h, "receive", map[string]any{})); fresh != 1 || h.recorded["42:8"] != "second" {
		t.Fatalf("the next poll recorded %v", h.recorded)
	}
	revived.kill() // after the host recorded 42:8, before it asked again

	third := startAdapter(t)
	if fresh := h.record(third.invoke(h, "receive", map[string]any{})); fresh != 0 {
		t.Fatalf("a replay of what the host had was recorded again: %d new", fresh)
	}
	if fresh := h.record(third.invoke(h, "receive", map[string]any{})); fresh != 0 || len(h.recorded) != 2 {
		t.Fatalf("the record holds %v", h.recorded)
	}
	if want := []int64{0, 0, 101, 101, 102}; !reflect.DeepEqual(q.asked, want) {
		t.Errorf("the polls asked from %v, want %v", q.asked, want)
	}
	if h.kv[offsetKey] != "102" {
		t.Errorf("kv holds %q, want 102", h.kv[offsetKey])
	}
}

// A host that grants no kv (declared): the offset lives in memory, a
// revived adapter asks from 0, which confirms nothing, and Telegram gives
// what it was never told was taken — so nothing is lost, and what the
// host already has arrives again and is recorded once.
func TestWithoutKVTheOffsetLivesInMemory(t *testing.T) {
	q := &telegramQueue{pending: []queued{{100, message(42, 7, "first")}, {101, message(42, 8, "second")}}}
	h := &standIn{t: t, handle: "tg", telegram: q.serve}
	p := startAdapter(t)
	h.record(p.invoke(h, "receive", map[string]any{}))
	h.record(p.invoke(h, "receive", map[string]any{}))
	p.kill()

	revived := startAdapter(t)
	if fresh := h.record(revived.invoke(h, "receive", map[string]any{})); fresh != 0 || len(h.recorded) != 2 {
		t.Fatalf("the record holds %v after a replay (%d new)", h.recorded, fresh)
	}
	if want := []int64{0, 101, 0}; !reflect.DeepEqual(q.asked, want) {
		t.Errorf("the polls asked from %v, want %v", q.asked, want)
	}
}

// ---- the host says what it recorded ----

// ack is the host's record and its word: each arrival recorded once by id
// except the ones it refuses, and the ids it holds — what the next receive
// carries as `recorded`.
func (h *standIn) ack(r reply, refuse ...string) []any {
	h.t.Helper()
	if r.status != "succeeded" {
		h.t.Fatalf("receive: %s %s: %s", r.status, r.code, r.reason)
	}
	var arrivals []struct{ ID, From, Body string }
	if err := json.Unmarshal(r.result, &arrivals); err != nil {
		h.t.Fatalf("receive answered no list of arrivals: %s", r.result)
	}
	if h.recorded == nil {
		h.recorded = map[string]string{}
	}
	held := []any{}
	for _, a := range arrivals {
		if slices.Contains(refuse, a.ID) {
			continue
		}
		h.recorded[a.ID] = a.Body
		held = append(held, a.ID)
	}
	return held
}

func told(recorded []any) map[string]any { return map[string]any{"recorded": recorded} }

// describe asks the host to say what it recorded.
func TestDescribeAsksToBeToldWhatWasRecorded(t *testing.T) {
	h := &standIn{t: t, handle: "tg", telegram: (&telegramQueue{}).serve}
	p := startAdapter(t)
	got := p.invoke(h, "describe", map[string]any{})
	if got.status != "succeeded" || !strings.Contains(string(got.result), `"acknowledges":true`) {
		t.Fatalf("describe answered %s %s", got.status, got.result)
	}
}

// The offset moves past an update only once the host says it holds the
// arrival: an answer the host has not yet acknowledged is asked for again,
// and kv follows what was settled, once each.
func TestTheOffsetMovesOnlyThroughWhatTheHostRecorded(t *testing.T) {
	q := &telegramQueue{pending: []queued{{100, message(42, 7, "first")}, {101, message(42, 8, "second")}}}
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: q.serve}
	p := startAdapter(t)

	recorded := []any{}
	recorded = h.ack(p.invoke(h, "receive", told(recorded))) // 42:7
	recorded = h.ack(p.invoke(h, "receive", told(recorded))) // told 42:7 → asks from 101 → 42:8
	if !reflect.DeepEqual(recorded, []any{"42:8"}) {
		t.Fatalf("the second answer held %v", recorded)
	}
	// The host did not get to 42:8 (it says nothing of it): the poll asks from 101 again.
	recorded = h.ack(p.invoke(h, "receive", told([]any{})))
	if !reflect.DeepEqual(recorded, []any{"42:8"}) {
		t.Fatalf("the update the host said nothing of was not offered again: %v", recorded)
	}
	h.ack(p.invoke(h, "receive", told(recorded))) // told 42:8 → asks from 102
	if want := []int64{0, 101, 101, 102}; !reflect.DeepEqual(q.asked, want) {
		t.Errorf("the polls asked from %v, want %v", q.asked, want)
	}
	if want := []string{"101", "102"}; !reflect.DeepEqual(h.kvWrites, want) {
		t.Errorf("kv was written %v, want %v: each offset settled once", h.kvWrites, want)
	}
}

// An arrival the host's record refused is not reported, so the offset stays
// at it and the next poll gives it again: it is lost to nobody.
func TestAnArrivalTheHostRefusedIsAskedForAgain(t *testing.T) {
	q := &telegramQueue{pending: []queued{{100, message(42, 7, "first")}, {101, message(42, 8, "second")}}}
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: q.serve}
	p := startAdapter(t)

	recorded := h.ack(p.invoke(h, "receive", told([]any{})), "42:7") // the record refuses 42:7
	if len(recorded) != 0 {
		t.Fatalf("the refused arrival was reported: %v", recorded)
	}
	recorded = h.ack(p.invoke(h, "receive", told(recorded))) // told nothing: asks from 0 again
	if !reflect.DeepEqual(recorded, []any{"42:7"}) || h.recorded["42:7"] != "first" {
		t.Fatalf("the refused arrival was not offered again: held %v, record %v", recorded, h.recorded)
	}
	h.ack(p.invoke(h, "receive", told(recorded)))
	if want := []int64{0, 0, 101}; !reflect.DeepEqual(q.asked, want) {
		t.Errorf("the polls asked from %v, want %v", q.asked, want)
	}
	if len(h.kvWrites) != 1 || h.kvWrites[0] != "101" {
		t.Errorf("kv was written %v: the offset moved past 42:7 before the host held it", h.kvWrites)
	}
}

// An update that made no arrival — nothing the host could record — moves the
// offset on with the ones before it, and never holds a poll where it is.
func TestAnUpdateThatMadeNoArrivalMovesOnAfterTheOnesBeforeIt(t *testing.T) {
	q := &telegramQueue{pending: []queued{{100, message(42, 7, "first")}, {101, "null"}, {102, message(42, 9, "third")}}}
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: q.serve}
	p := startAdapter(t)

	recorded := h.ack(p.invoke(h, "receive", told([]any{}))) // 100 → 42:7
	recorded = h.ack(p.invoke(h, "receive", told(recorded))) // told 42:7 → 101, no arrival
	if len(recorded) != 0 {
		t.Fatalf("an update with no arrival reported one: %v", recorded)
	}
	recorded = h.ack(p.invoke(h, "receive", told(recorded))) // 101 needed nothing → asks from 102 → 42:9
	if !reflect.DeepEqual(recorded, []any{"42:9"}) {
		t.Fatalf("the poll did not go on past the update with no arrival: %v", recorded)
	}
	if want := []int64{0, 101, 102}; !reflect.DeepEqual(q.asked, want) {
		t.Errorf("the polls asked from %v, want %v", q.asked, want)
	}
}

// THE KILL, again, for a host that says what it recorded: the process dies
// between the answer and the host's word, and the revived one — which
// remembers nothing but kv — is told nothing, so asks from the offset kv
// holds, and Telegram gives the update again.
func TestAKillBeforeTheHostSaidWhatItRecordedLosesNothing(t *testing.T) {
	q := &telegramQueue{pending: []queued{{100, message(42, 7, "first")}, {101, message(42, 8, "second")}}}
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: q.serve}

	first := startAdapter(t)
	h.ack(first.invoke(h, "receive", told([]any{})), "42:7") // the host is killed before it records 42:7 too
	first.kill()

	revived := startAdapter(t)
	recorded := h.ack(revived.invoke(h, "receive", told([]any{}))) // a host that starts anew has recorded nothing to tell
	if !reflect.DeepEqual(recorded, []any{"42:7"}) || h.recorded["42:7"] != "first" {
		t.Fatalf("after the kill the revived adapter did not offer the update again: %v %v", recorded, h.recorded)
	}
	recorded = h.ack(revived.invoke(h, "receive", told(recorded)))
	if !reflect.DeepEqual(recorded, []any{"42:8"}) {
		t.Fatalf("the poll after the settled one: %v", recorded)
	}
	if want := []int64{0, 0, 101}; !reflect.DeepEqual(q.asked, want) {
		t.Errorf("the polls asked from %v, want %v", q.asked, want)
	}
}

// A service that gives an update older than the offset — a replay, a
// misbehaving mirror — never moves the offset back: what was confirmed to
// Telegram stays confirmed.
func TestTheOffsetNeverMovesBack(t *testing.T) {
	q := &telegramQueue{pending: []queued{{100, message(42, 7, "first")}}}
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, telegram: q.serve}
	p := startAdapter(t)

	recorded := h.ack(p.invoke(h, "receive", told([]any{}))) // 100 → 42:7
	q.instead = []brokerReply{answered(200, `{"ok":true,"result":[{"update_id":50,"message":`+message(42, 9, "stale")+`}]}`, -1)}
	recorded = h.ack(p.invoke(h, "receive", told(recorded))) // told 42:7 → asks from 101 → an update from 50
	if !reflect.DeepEqual(recorded, []any{"42:9"}) {
		t.Fatalf("the stale update was not carried: %v", recorded)
	}
	h.ack(p.invoke(h, "receive", told(recorded))) // told 42:9 → the offset must not fall to 51
	if want := []int64{0, 101, 101}; !reflect.DeepEqual(q.asked, want) {
		t.Errorf("the polls asked from %v, want %v", q.asked, want)
	}
}

// callClock is the host's clock in the tests, in milliseconds since the epoch.
const callClock = 1_790_000_000_000

// withClock is args with the host's `_host_now_ms`, when they carry none.
func withClock(args map[string]any) map[string]any {
	if _, ok := args["_host_now_ms"]; ok {
		return args
	}
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	out["_host_now_ms"] = callClock
	return out
}

// The channel's sends are bounded: send_limit in any send_window_minutes, both
// settings on the plugin's card. The send over the limit is refused before
// Telegram is asked, in the code the host reads as nothing accepted, saying which
// limit, where to change it and when the next may go. A send Telegram refused took
// no place; one whose outcome is not known took one.
func TestSendsAreBoundedByTheirWindow(t *testing.T) {
	next := answered(200, `{"ok":true,"result":`+sentMessage+`}`, -1)
	h := &standIn{t: t, handle: "tg", kv: map[string]string{}, extra: map[string]any{"send_limit": 2, "send_window_minutes": 5},
		telegram: func(httpCall) brokerReply { return next }}
	p := startAdapter(t)
	const min = 60_000
	send := func(ms int64) reply {
		return p.invoke(h, "send", map[string]any{"address": "42", "body": "hello", "_host_now_ms": ms})
	}
	if got := send(callClock); got.status != "succeeded" {
		t.Fatalf("the first: %s", got.line())
	}
	next = answered(429, `{"ok":false,"error_code":429,"description":"Too Many Requests"}`, 30)
	if got := send(callClock + min); got.status != "failed" || got.code != "NET_REMOTE_FAILED" || strings.Contains(got.reason, "its limit") {
		t.Fatalf("a send Telegram refused: %s", got.line())
	}
	next = answered(200, `{"ok":true,"result":`+sentMessage+`}`, -1)
	if got := send(callClock + 2*min); got.status != "succeeded" {
		t.Fatalf("the second that went — the refused one took no place: %s", got.line())
	}
	asked := len(h.calls)
	got := send(callClock + 3*min)
	leaves := time.UnixMilli(callClock + 5*min).UTC().Format(time.RFC3339)
	if got.status != "failed" || got.code != "NET_REMOTE_FAILED" ||
		!strings.Contains(got.reason, "this channel has sent 2 in the last 5 minutes, its limit — the operator sets it as this plugin's send_limit and send_window_minutes settings; the next send can go at "+leaves+" — the message was not sent") {
		t.Fatalf("the third: %s", got.line())
	}
	if len(h.calls) != asked {
		t.Errorf("the refused send asked Telegram")
	}
	if got := send(callClock + 5*min + 1); got.status != "succeeded" {
		t.Errorf("when the first has left the window: %s", got.line())
	}

	// A send whose outcome is not known took a place.
	h2 := &standIn{t: t, handle: "tg", kv: map[string]string{}, extra: map[string]any{"send_limit": 1, "send_window_minutes": 5},
		telegram: func(httpCall) brokerReply {
			return answered(500, `{"ok":false,"error_code":500,"description":"Internal Server Error"}`, -1)
		}}
	if got := p.invoke(h2, "send", map[string]any{"address": "42", "body": "hello"}); got.code != "NET_EFFECT_UNKNOWN" {
		t.Fatalf("unknown: %s", got.line())
	}
	before := len(h2.calls)
	if got := p.invoke(h2, "send", map[string]any{"address": "42", "body": "again"}); got.status != "failed" || !strings.Contains(got.reason, "its limit") || len(h2.calls) != before {
		t.Errorf("after an unknown send the window is full: %s (asked %d more)", got.line(), len(h2.calls)-before)
	}
}
