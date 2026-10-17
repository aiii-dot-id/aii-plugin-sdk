package main

// The connector against its contracts, over the native lane: this
// test binary re-executes itself as the plugin (TestMain), so each case
// drives the same handlers the guest carries, behind the kit's own
// dispatch, over framed BBB — and a case can kill the plugin's process in
// the middle of a write and start another, which is what "the same create
// across a restart, with no storage" needs. Every plugin process runs in
// Tokyo's zone, which no calendar here keeps, so no answer can lean on the
// host's clock face.
//
// The host is a stand-in. It answers settings.get with the google handle,
// fails the test on any other host call but http.* (the plugin holds no
// storage), holds every call to the broker's rules (the handle cited; no
// write declared idempotent), and holds every answer that succeeded to the
// operation's declared output schema, as the host does (aii-os pluginhost
// acceptResult) — a refused result after a write is an event the identity
// never hears of.
//
// Google is scripted behind the broker's own reply vocabulary (aii-os
// internal/broker/broker.go resultReply, the receipt's effect) and keeps
// what the Calendar API reference documents: a 409 for an id the calendar
// holds, a deleted event kept under its id as cancelled ("The get method
// always returns them"), a 412 when If-Match names an older etag, a 204
// with an empty body for a delete and a 410 for a second one, pages that
// may hold fewer events than asked.

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// serveEnv starts this binary as the plugin instead of as the tests.
const serveEnv = "AIISDK_EXAMPLE_GOOGLE_CALENDAR_SERVE"

func TestMain(m *testing.M) {
	if os.Getenv(serveEnv) == "1" {
		if err := plugin.Serve(""); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

const (
	newYork = "America/New_York"
	handle  = "google-work"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// ---- the broker's replies ----

// brokerReply is the host's answer to one call the plugin makes: a result,
// or a JSON-RPC error object (a denial). kill answers nothing: the plugin's
// process is killed while it waits.
type brokerReply struct {
	result, errObj json.RawMessage
	kill           bool
}

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

func endedWith(status, code string, or any, effect string) brokerReply {
	m := map[string]any{"success": false, "ok": false, "status": status, "reason": code, "reasonCode": code, "reason_code": code, "external_receipt": receipt(effect)}
	if or != nil {
		m["operation_result"] = or
	}
	b, _ := json.Marshal(m)
	return brokerReply{result: b}
}

func failedWith(code string, or any, effect string) brokerReply {
	return endedWith("failed", code, or, effect)
}

// refusedWith is the broker's refusal as a result: the auth-profile ladder
// answers so (broker.go oauthDenial), before any request is written.
func refusedWith(code string) brokerReply { return endedWith("denied", code, nil, "") }

func deniedBy(code string) brokerReply {
	b, _ := json.Marshal(map[string]any{"code": -32000, "message": "not granted here", "data": map[string]string{"reasonCode": code}})
	return brokerReply{errObj: b}
}

// gave is an exchange Google answered, read whole by the broker: from
// 400 a failed remote outcome with the evidence attached, performed.
// retryAfter is the host's reading of a Retry-After header; -1 is none.
func gave(status int, body string, retryAfter int64) brokerReply {
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

// gerr is Google's error body.
func gerr(code int, reason, message string) string {
	b, _ := json.Marshal(map[string]any{"error": map[string]any{"code": code, "message": message, "errors": []any{map[string]any{"domain": "global", "reason": reason, "message": message}}}})
	return string(b)
}

var (
	lostAnswer = failedWith("NET_EFFECT_UNKNOWN", nil, "unknown")
	notWritten = failedWith("NET_REMOTE_OUTCOME_FAILED", nil, "not-performed")
	backend    = gave(500, gerr(500, "backendError", "Backend Error"), -1)
)

// ---- the stand-in host ----

type httpCall struct {
	op   string
	url  string
	args map[string]any
}

func (c httpCall) query() url.Values {
	u, _ := url.Parse(c.url)
	return u.Query()
}

func (c httpCall) header(name string) string {
	h, _ := c.args["headers"].(map[string]any)
	v, _ := h[name].(string)
	return v
}

func (c httpCall) body() map[string]any {
	var b map[string]any
	s, _ := c.args["body"].(string)
	_ = json.Unmarshal([]byte(s), &b)
	return b
}

// standIn answers the plugin's host calls: settings.get from the handle it
// holds ("" is not set), http.* through google, and nothing else.
type standIn struct {
	t      *testing.T
	handle string
	google func(httpCall) brokerReply
	asked  []string
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
	h.asked = append(h.asked, p.Operation)
	switch p.Operation {
	case "settings.get":
		values := map[string]any{"calendar_id": "primary"}
		if h.handle != "" {
			values["google"] = h.handle
		}
		return succeededWith(map[string]any{"values": values}, "")
	case "http.get", "http.post", "http.put", "http.patch", "http.delete":
		var target struct{ URL string }
		_ = json.Unmarshal(p.Target, &target)
		var a map[string]any
		_ = json.Unmarshal(p.Arguments, &a)
		c := httpCall{op: p.Operation, url: target.URL, args: a}
		if a["auth_profile"] != h.handle {
			h.t.Errorf("%s %s cites %v, not the google handle", c.op, c.url, a["auth_profile"])
		}
		if c.op != "http.get" && a["idempotent"] != nil {
			h.t.Errorf("%s %s declares itself idempotent: the broker would send it twice on a transport failure", c.op, c.url)
		}
		return h.google(c)
	}
	h.t.Errorf("the plugin asked the host for %s: it holds no storage and needs nothing but its handle and Google", p.Operation)
	return deniedBy("OPERATION_NOT_ALLOWED")
}

// ---- the plugin's process ----

// reply is the plugin's answer to one operation, as the host reads it.
type reply struct {
	status string // succeeded | failed | denied; error for an error object; killed
	code   string
	reason string
	result json.RawMessage
}

// line is what the host makes of a failure for the identity and the
// operator (aii-os pluginhost decodeReply).
func (r reply) line() string {
	return fmt.Sprintf("operation %s: %s (%s)", r.status, r.reason, r.code)
}

// said is what an answer tells its reader: the words of a failure, or the
// result of a success.
func (r reply) said() string {
	if r.status == "succeeded" {
		return string(r.result)
	}
	return r.reason
}

// field reads one member of a succeeded answer.
func (r reply) field(key string) any {
	var m map[string]any
	_ = json.Unmarshal(r.result, &m)
	return m[key]
}

type proc struct {
	t      *testing.T
	cmd    *exec.Cmd
	in     io.WriteCloser
	frames chan []byte
	n      int
}

func start(t *testing.T) *proc {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), serveEnv+"=1", "TZ=Asia/Tokyo")
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
	p := &proc{t: t, cmd: cmd, in: in, frames: make(chan []byte, 16)}
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

// kill ends the process as a crash does: SIGKILL, nothing finished, its
// memory gone.
func (p *proc) kill() {
	if p.cmd.ProcessState != nil {
		return
	}
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
}

// invoke drives one operation the way the host does — an invoke.call frame
// — and answers each call the plugin makes back through h, until the
// plugin replies. An answer that succeeded is held to its output schema.
func (p *proc) invoke(h *standIn, op string, args map[string]any) reply {
	p.t.Helper()
	p.n++
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
				p.t.Fatalf("invoke %s: the plugin's process ended", op)
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
			r := decodeReply(p.t, f.Result, f.Error)
			if r.status == "succeeded" {
				conform(p.t, op, r.result)
			}
			return r
		}
		back := h.answer(f.Params)
		if back.kill {
			p.kill()
			return reply{status: "killed"}
		}
		member, value := "result", back.result
		if back.errObj != nil {
			member, value = "error", back.errObj
		}
		msg := fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,%q:%s}`, f.ID, member, value)
		if err := sdk.WriteFrame(p.in, []byte(msg), sdk.MaxControlFrameBytes); err != nil {
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

// ---- the declared schemas, as the host holds them ----

func schemaFor(t *testing.T, op string, input bool) map[string]any {
	t.Helper()
	for _, d := range plugin.Descriptors() {
		if d.ID != op {
			continue
		}
		path := d.Output
		if input {
			path = d.Input
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var s map[string]any
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return s
	}
	t.Fatalf("no descriptor for %s", op)
	return nil
}

// conform holds an answer to the operation's declared output schema.
func conform(t *testing.T, op string, result json.RawMessage) {
	t.Helper()
	var v any
	if err := json.Unmarshal(result, &v); err != nil {
		t.Errorf("%s answered what is not JSON: %s", op, result)
		return
	}
	if err := conforms(schemaFor(t, op, false), v, op); err != nil {
		t.Errorf("the host would refuse this answer — %v: %s", err, result)
	}
}

// conforms validates a decoded value against the closed subset of JSON
// Schema these files use: type, enum, properties, required,
// additionalProperties false, items, minLength, maxLength, pattern,
// minimum, maximum, maxItems.
func conforms(s map[string]any, v any, path string) error {
	if name, ok := s["type"].(string); ok && !isType(v, name) {
		return fmt.Errorf("%s: %v is not of type %s", path, v, name)
	}
	if enum, ok := s["enum"].([]any); ok {
		if str, isStr := v.(string); !isStr || !slices.Contains(enum, any(str)) {
			return fmt.Errorf("%s: %v is none of %v", path, v, enum)
		}
	}
	switch x := v.(type) {
	case map[string]any:
		props, _ := s["properties"].(map[string]any)
		req, _ := s["required"].([]any)
		for _, r := range req {
			if _, ok := x[r.(string)]; !ok {
				return fmt.Errorf("%s: %s is required", path, r)
			}
		}
		for k, val := range x {
			sub, ok := props[k].(map[string]any)
			if !ok {
				if s["additionalProperties"] == false {
					return fmt.Errorf("%s: %s is not declared", path, k)
				}
				continue
			}
			if err := conforms(sub, val, path+"."+k); err != nil {
				return err
			}
		}
	case []any:
		if n, ok := s["maxItems"].(float64); ok && float64(len(x)) > n {
			return fmt.Errorf("%s: %d items", path, len(x))
		}
		if items, ok := s["items"].(map[string]any); ok {
			for i, e := range x {
				if err := conforms(items, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	case string:
		n := float64(utf8.RuneCountInString(x))
		if m, ok := s["minLength"].(float64); ok && n < m {
			return fmt.Errorf("%s: shorter than %v", path, m)
		}
		if m, ok := s["maxLength"].(float64); ok && n > m {
			return fmt.Errorf("%s: longer than %v", path, m)
		}
		if p, ok := s["pattern"].(string); ok && !regexp.MustCompile(p).MatchString(x) {
			return fmt.Errorf("%s: %q does not match %s", path, x, p)
		}
	case float64:
		if m, ok := s["minimum"].(float64); ok && x < m {
			return fmt.Errorf("%s: %v is below %v", path, x, m)
		}
		if m, ok := s["maximum"].(float64); ok && x > m {
			return fmt.Errorf("%s: %v is above %v", path, x, m)
		}
	}
	return nil
}

func isType(v any, name string) bool {
	switch name {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == math.Trunc(f)
	case "number":
		_, ok := v.(float64)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	}
	return v == nil
}

// ---- the scripted Google ----

// step answers one exchange in Google's place: a fixed reply, or one that
// serves the exchange first (its effect happens) and then loses the answer
// or kills the plugin.
type step func(g *google, c httpCall) brokerReply

func replies(r brokerReply) step { return func(*google, httpCall) brokerReply { return r } }

func thenLost(g *google, c httpCall) brokerReply { g.serve(c); return lostAnswer }

func thenKilled(g *google, c httpCall) brokerReply { g.serve(c); return brokerReply{kill: true} }

// google is the Calendar API for the calendar "primary", in memory: events
// by id — a deleted one kept, cancelled, under its id — an etag that
// changes on every write, and pages of at most pageCap events when set.
// at[n] answers the n-th exchange in Google's place.
type google struct {
	t       *testing.T
	zone    string
	events  map[string]map[string]any
	tag     int
	pageCap int
	at      map[int]step
	calls   []httpCall
}

func newGoogle(t *testing.T, zone string) *google {
	return &google{t: t, zone: zone, events: map[string]map[string]any{}, at: map[int]step{}}
}

func (g *google) handle(c httpCall) brokerReply {
	g.calls = append(g.calls, c)
	if f := g.at[len(g.calls)]; f != nil {
		return f(g, c)
	}
	return g.serve(c)
}

// ops is the exchanges so far, as verb and whether they named an event.
func (g *google) ops() []string {
	var out []string
	for _, c := range g.calls {
		out = append(out, c.op)
	}
	return out
}

func (g *google) count(op string) int {
	n := 0
	for _, c := range g.calls {
		if c.op == op {
			n++
		}
	}
	return n
}

func (g *google) nextTag() string {
	g.tag++
	return fmt.Sprintf(`"%d"`, 3181161784712000+g.tag)
}

// put holds an event as Google would: confirmed unless it says otherwise,
// with a fresh etag.
func (g *google) put(ev map[string]any) map[string]any {
	ev["kind"] = "calendar#event"
	if ev["status"] == nil {
		ev["status"] = "confirmed"
	}
	ev["etag"] = g.nextTag()
	g.events[ev["id"].(string)] = ev
	return ev
}

func timed(id, title, start, end, zone string) map[string]any {
	return map[string]any{"id": id, "summary": title,
		"start": map[string]any{"dateTime": start, "timeZone": zone},
		"end":   map[string]any{"dateTime": end, "timeZone": zone}}
}

func allDay(id, title, date, next string) map[string]any {
	return map[string]any{"id": id, "summary": title, "start": map[string]any{"date": date}, "end": map[string]any{"date": next}}
}

func marshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// legalID is Google's rule for an id a client names: base32hex characters,
// a–v and 0–9, 5 to 1024 of them.
func legalID(id string) bool {
	return len(id) >= 5 && len(id) <= 1024 && strings.Trim(id, "0123456789abcdefghijklmnopqrstuv") == ""
}

func (g *google) serve(c httpCall) brokerReply {
	u, err := url.Parse(c.url)
	if err != nil || u.Scheme != "https" || u.Host != "www.googleapis.com" {
		g.t.Errorf("a call outside Google's API: %s", c.url)
		return gave(404, gerr(404, "notFound", "Not Found"), -1)
	}
	rest, ok := strings.CutPrefix(u.EscapedPath(), "/calendar/v3/calendars/primary/events")
	if !ok {
		g.t.Errorf("a call outside the calendar: %s", c.url)
		return gave(404, gerr(404, "notFound", "Not Found"), -1)
	}
	id, _ := url.PathUnescape(strings.TrimPrefix(rest, "/"))
	switch {
	case c.op == "http.get" && id == "":
		return g.list(c.query())
	case c.op == "http.get":
		if ev, ok := g.events[id]; ok {
			return gave(200, marshal(ev), -1)
		}
		return gave(404, gerr(404, "notFound", "Not Found"), -1)
	case c.op == "http.post" && id == "":
		return g.insert(c)
	case c.op == "http.patch" && id != "":
		return g.patch(id, c)
	case c.op == "http.delete" && id != "":
		return g.remove(id)
	}
	g.t.Errorf("an exchange the Calendar API does not have: %s %s", c.op, c.url)
	return gave(400, gerr(400, "badRequest", "Bad Request"), -1)
}

// insert holds a new event under the id it names, or answers 409 for an id
// the calendar holds — a deleted event's included.
func (g *google) insert(c httpCall) brokerReply {
	ev := c.body()
	id, _ := ev["id"].(string)
	switch {
	case !legalID(id):
		return gave(400, gerr(400, "invalid", "Invalid resource id value."), -1)
	case g.events[id] != nil:
		return gave(409, gerr(409, "duplicate", "The requested identifier already exists."), -1)
	}
	ev["htmlLink"] = "https://www.google.com/calendar/event?eid=" + id
	return gave(200, marshal(g.put(ev)), -1)
}

// patch sets the fields the body names and keeps every other; an If-Match
// naming an older etag changes nothing.
func (g *google) patch(id string, c httpCall) brokerReply {
	ev := g.events[id]
	switch {
	case ev == nil:
		return gave(404, gerr(404, "notFound", "Not Found"), -1)
	case ev["status"] == "cancelled":
		return gave(410, gerr(410, "deleted", "Resource has been deleted"), -1)
	case c.header("If-Match") != "" && c.header("If-Match") != ev["etag"]:
		return gave(412, gerr(412, "conditionNotMet", "Precondition Failed"), -1)
	}
	for k, v := range c.body() {
		ev[k] = v
	}
	ev["etag"] = g.nextTag()
	return gave(200, marshal(ev), -1)
}

// remove cancels an event and keeps it under its id; the documented
// success is an empty body.
func (g *google) remove(id string) brokerReply {
	ev := g.events[id]
	switch {
	case ev == nil:
		return gave(404, gerr(404, "notFound", "Not Found"), -1)
	case ev["status"] == "cancelled":
		return gave(410, gerr(410, "deleted", "Resource has been deleted"), -1)
	}
	ev["status"], ev["etag"] = "cancelled", g.nextTag()
	return gave(204, "", -1)
}

// span is when an event holds time; an all-day one's dates are midnights
// in the calendar's zone.
func (g *google) span(ev map[string]any) (time.Time, time.Time) {
	at := func(v any) time.Time {
		m, _ := v.(map[string]any)
		if dt, ok := m["dateTime"].(string); ok {
			t, _ := time.Parse(time.RFC3339, dt)
			return t
		}
		loc, _ := time.LoadLocation(g.zone)
		d, _ := time.ParseInLocation("2006-01-02", m["date"].(string), loc)
		return d
	}
	return at(ev["start"]), at(ev["end"])
}

// list answers events.list as the reference documents it: an event whose
// end is after timeMin and whose start is before timeMax, cancelled ones
// left out, in the order they start, maxResults a page or fewer; a page
// token holds its query, and a token asked with another is refused.
func (g *google) list(q url.Values) brokerReply {
	if q.Get("fields") == "timeZone" {
		return gave(200, `{"timeZone":"`+g.zone+`"}`, -1)
	}
	if q.Get("singleEvents") != "true" || q.Get("orderBy") != "startTime" {
		g.t.Errorf("a list that does not expand recurring events in start order: %v", q)
	}
	from, err := time.Parse(time.RFC3339, q.Get("timeMin"))
	if err != nil {
		return gave(400, gerr(400, "badRequest", "Bad Request"), -1)
	}
	var to time.Time
	if s := q.Get("timeMax"); s != "" {
		if to, err = time.Parse(time.RFC3339, s); err != nil {
			return gave(400, gerr(400, "badRequest", "Bad Request"), -1)
		}
	}
	size, err := strconv.Atoi(q.Get("maxResults"))
	if err != nil || size < 1 || size > 2500 {
		return gave(400, gerr(400, "badRequest", "Bad Request"), -1)
	}
	if g.pageCap > 0 && g.pageCap < size {
		size = g.pageCap
	}
	query := q.Get("timeMin") + "~" + q.Get("timeMax") + "~"
	offset := 0
	if tok := q.Get("pageToken"); tok != "" {
		n, ok := strings.CutPrefix(tok, query)
		if offset, err = strconv.Atoi(n); !ok || err != nil {
			return gave(400, gerr(400, "invalid", "Invalid Value"), -1)
		}
	}
	hits := []map[string]any{}
	for _, ev := range g.events {
		if s, e := g.span(ev); ev["status"] != "cancelled" && e.After(from) && (to.IsZero() || s.Before(to)) {
			hits = append(hits, ev)
		}
	}
	sort.Slice(hits, func(i, j int) bool {
		si, _ := g.span(hits[i])
		sj, _ := g.span(hits[j])
		return si.Before(sj) || si.Equal(sj) && hits[i]["id"].(string) < hits[j]["id"].(string)
	})
	end := min(offset+size, len(hits))
	resp := map[string]any{"kind": "calendar#events", "timeZone": g.zone, "items": hits[min(offset, end):end]}
	if end < len(hits) {
		resp["nextPageToken"] = query + strconv.Itoa(end)
	}
	return gave(200, marshal(resp), -1)
}

// ---- arguments ----

// act is the host's stamp on a dispatch the operator confirmed.
func act(args map[string]any) map[string]any {
	args["_host_operator_act"] = map[string]any{"id": "act-1", "confirmed_at": "2026-10-01T12:00:00Z"}
	return args
}

// dentist is the create the operator confirmed, with changes.
func dentist(changes map[string]any) map[string]any {
	a := map[string]any{"title": "Dentist", "start": "2026-10-02T10:00", "end": "2026-10-02T11:00", "time_zone": newYork, "notify": "none"}
	for k, v := range changes {
		if v == nil {
			delete(a, k)
		} else {
			a[k] = v
		}
	}
	return act(a)
}

// dentistID is the id the plugin derives for dentist(nil) on "primary",
// computed apart from this code, from the recipe the README gives: the
// base32hex of SHA-256 over "24:google-calendar create 1;7:primary;
// 20:2026-10-02T14:00:00Z;20:2026-10-02T15:00:00Z;7:Dentist;0:;". It is
// pinned: a release that derived another would make a second event of a
// create asked again across the upgrade.
const dentistID = "aq3me42qfmfp0ncqdt07a28nboe0lr6a112sqbc47mjt1c391ut0"

func check(t *testing.T, name string, got reply, status, code, says string) {
	t.Helper()
	if got.status != status || got.code != code || !strings.Contains(got.said(), says) {
		t.Errorf("%s:\n  got  %s %s: %s\n  want %s %s, saying %q", name, got.status, got.code, got.said(), status, code, says)
	}
}

// ---- create ----

// Every answer Google can give an insert, and the create's verdict: the
// code the host reads and the words the identity and the operator
// read. The insert is written once, whatever comes back.
func TestCreateAnswersWhatGoogleDid(t *testing.T) {
	same := func(g *google) { g.put(dentistEvent(g, "confirmed", "2026-10-02T10:00:00-04:00")) }
	cases := []struct {
		name   string
		seed   func(*google)
		script map[int]step
		status string
		code   string
		says   string
		ops    []string
	}{
		{"a new event is made under the id derived from it", nil, nil,
			"succeeded", "", `"event_id":"` + dentistID + `"`, []string{"http.post"}},
		{"a 409 read back as this very event was made by an earlier attempt", same, nil,
			"succeeded", "", `"outcome":"created_earlier"`, []string{"http.post", "http.get"}},
		{"a 409 read back as a deleted event: the id is kept, and never taken for this one", func(g *google) { g.put(dentistEvent(g, "cancelled", "2026-10-02T10:00:00-04:00")) }, nil,
			"failed", argInvalid, "this event's id belongs to an event deleted from this calendar, and Google keeps it — the event was not created; to create it again, give it a distinct word", []string{"http.post", "http.get"}},
		{"a 409 read back as a different event: the id alone proves nothing", func(g *google) { g.put(dentistEvent(g, "confirmed", "2026-10-02T09:00:00-04:00")) }, nil,
			"failed", argInvalid, "an event with this id exists and differs in its start — the event was not created; it may be this event, changed since it was made", []string{"http.post", "http.get"}},
		{"a 409 whose read-back is lost is unknown", same, map[int]step{2: replies(lostAnswer)},
			"failed", effectUnknown, "Google says an event with this id exists (409: The requested identifier already exists.) and it could not be read back (the answer was lost) — it may be this event, created earlier; nothing was created now", []string{"http.post", "http.get"}},
		{"a 409 whose read-back fails is unknown", same, map[int]step{2: replies(backend)},
			"failed", effectUnknown, "could not be read back (Google answered 500: Backend Error)", []string{"http.post", "http.get"}},
		{"a 409 whose read-back finds no event is unknown", nil, map[int]step{1: replies(gave(409, gerr(409, "duplicate", "The requested identifier already exists."), -1))},
			"failed", effectUnknown, "could not be read back (Google answered 404: Not Found)", []string{"http.post", "http.get"}},
		{"a 409 whose read-back the host refuses is unknown", same, map[int]step{2: replies(refusedWith("NET_AUTH_PROFILE_DISCONNECTED"))},
			"failed", effectUnknown, "could not be read back (the host refused it (NET_AUTH_PROFILE_DISCONNECTED))", []string{"http.post", "http.get"}},
		{"a 400 refuses these arguments", nil, map[int]step{1: replies(gave(400, gerr(400, "invalid", "Invalid attendee email."), -1))},
			"failed", argInvalid, "Google refused the event (400: Invalid attendee email.) — the event was not created", []string{"http.post"}},
		{"a 401 Google still gives after the host's refresh made nothing", nil, map[int]step{1: replies(gave(401, gerr(401, "authError", "Invalid Credentials"), -1))},
			"failed", remoteFailed, "Google refused it (401: Invalid Credentials) — the event was not created", []string{"http.post"}},
		{"a 403 made nothing", nil, map[int]step{1: replies(gave(403, gerr(403, "forbidden", "Forbidden"), -1))},
			"failed", remoteFailed, "Google refused it (403: Forbidden) — the event was not created", []string{"http.post"}},
		{"a 404 for the calendar made nothing", nil, map[int]step{1: replies(gave(404, gerr(404, "notFound", "Not Found"), -1))},
			"failed", remoteFailed, "Google refused it (404: Not Found) — the event was not created", []string{"http.post"}},
		{"a 429 made nothing and carries the wait", nil, map[int]step{1: replies(gave(429, gerr(429, "rateLimitExceeded", "Rate Limit Exceeded"), 30))},
			"failed", remoteFailed, "Google refused it (429: Rate Limit Exceeded) and asks to wait 30 s — the event was not created", []string{"http.post"}},
		{"a 429 with no hint says so, never zero", nil, map[int]step{1: replies(gave(429, gerr(429, "rateLimitExceeded", "Rate Limit Exceeded"), -1))},
			"failed", remoteFailed, "and named no time to wait", []string{"http.post"}},
		{"a 500 can follow the write: the event may exist", nil, map[int]step{1: replies(backend)},
			"failed", effectUnknown, "Google answered 500: Backend Error — the event may have been created; the same create asked again finds it by its id", []string{"http.post"}},
		{"a 503 from the front, not Google's JSON: the event may exist", nil, map[int]step{1: replies(gave(503, "<html>Service Unavailable</html>", -1))},
			"failed", effectUnknown, "Google answered 503 — the event may have been created", []string{"http.post"}},
		{"an insert written and unanswered may have made it", nil, map[int]step{1: thenLost},
			"failed", effectUnknown, "the create was written and its answer was lost — the event may have been created; the same create asked again finds it by its id", []string{"http.post"}},
		{"a 200 that shows no event may have made one", nil, map[int]step{1: replies(gave(200, "<html>ok</html>", -1))},
			"failed", effectUnknown, "Google answered 200 and the answer does not show the event — the event may have been created", []string{"http.post"}},
		{"a 200 naming another event is not this one's receipt", nil, map[int]step{1: replies(gave(200, `{"id":"somethingelse"}`, -1))},
			"failed", effectUnknown, "Google answered 200 and the answer does not show the event", []string{"http.post"}},
		{"a request the receipt says was never written made nothing", nil, map[int]step{1: replies(notWritten)},
			"failed", remoteFailed, "Google was not reached — the event was not created", []string{"http.post"}},
		{"an answer too large for the host was an answer: the event may exist", nil, map[int]step{1: replies(failedWith("NET_RESPONSE_TOO_LARGE", nil, "performed"))},
			"failed", effectUnknown, "the host answered NET_RESPONSE_TOO_LARGE — the event may have been created", []string{"http.post"}},
		{"a host reply that states no outcome: the event may exist", nil, map[int]step{1: replies(brokerReply{result: json.RawMessage(`{"operation_result":{"http_status":200}}`)})},
			"failed", effectUnknown, "the host's answer could not be read — the event may have been created", []string{"http.post"}},
		{"the host's denial is relayed with its code", nil, map[int]step{1: replies(deniedBy("POLICY_DENY"))},
			"denied", "POLICY_DENY", "the host refused the call (not granted here) — the event was not created", []string{"http.post"}},
		{"an account not connected is relayed, typed, with the fix", nil, map[int]step{1: replies(refusedWith("NET_AUTH_PROFILE_DISCONNECTED"))},
			"denied", "NET_AUTH_PROFILE_DISCONNECTED", "the Google account is not connected, or Google no longer accepts it — the operator connects it again on the Plugins page; the event was not created", []string{"http.post"}},
		{"an account that could not be refreshed is relayed, typed", nil, map[int]step{1: replies(refusedWith("NET_AUTH_PROFILE_UNAVAILABLE"))},
			"denied", "NET_AUTH_PROFILE_UNAVAILABLE", "the Google account's access could not be refreshed just now — try again later; the event was not created", []string{"http.post"}},
	}
	var g *google
	h := &standIn{t: t, handle: handle, google: func(c httpCall) brokerReply { return g.handle(c) }}
	p := start(t)
	for _, tc := range cases {
		g = newGoogle(t, newYork)
		if tc.seed != nil {
			tc.seed(g)
		}
		for n, s := range tc.script {
			g.at[n] = s
		}
		got := p.invoke(h, "calendar.create", dentist(nil))
		check(t, tc.name, got, tc.status, tc.code, tc.says)
		if !reflect.DeepEqual(g.ops(), tc.ops) {
			t.Errorf("%s: Google was asked %v, want %v", tc.name, g.ops(), tc.ops)
		}
		if got.status != "succeeded" {
			t.Logf("%-78s %s", tc.name, got.line())
		}
	}
}

// dentistEvent is the event dentist(nil) intends, as Google holds it —
// with the status and start given.
func dentistEvent(g *google, status, start string) map[string]any {
	ev := timed(dentistID, "Dentist", start, "2026-10-02T11:00:00-04:00", newYork)
	ev["status"] = status
	return ev
}

// The insert the plugin writes: the derived id, the intended fields with
// their offset and zone, the notify choice, and nothing the broker would
// send twice.
func TestCreateWritesTheIntendedEvent(t *testing.T) {
	g := newGoogle(t, newYork)
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	got := p.invoke(h, "calendar.create", dentist(map[string]any{
		"location": "123 Example Street", "description": "bring the form", "attendees": []any{"Recipient@Example.com", "identity@example.invalid", "recipient@example.com"}, "notify": "all",
	}))
	check(t, "a create", got, "succeeded", "", `"outcome":"created"`)
	c := g.calls[0]
	want := map[string]any{
		"id": dentistID, "summary": "Dentist", "location": "123 Example Street", "description": "bring the form",
		"start":     map[string]any{"dateTime": "2026-10-02T10:00:00-04:00", "timeZone": newYork},
		"end":       map[string]any{"dateTime": "2026-10-02T11:00:00-04:00", "timeZone": newYork},
		"attendees": []any{map[string]any{"email": "recipient@example.com"}, map[string]any{"email": "identity@example.invalid"}},
	}
	switch {
	case c.op != "http.post" || !strings.HasPrefix(c.url, "https://www.googleapis.com/calendar/v3/calendars/primary/events?"):
		t.Errorf("the insert is %s %s", c.op, c.url)
	case c.query().Get("sendUpdates") != "all":
		t.Errorf("the insert asks sendUpdates=%q, want the operator's all", c.query().Get("sendUpdates"))
	case !reflect.DeepEqual(c.body(), want):
		t.Errorf("the insert writes\n  %v\nwant\n  %v", c.body(), want)
	case c.args["content_type"] != "application/json" || c.args["timeout_ms"] != float64(15000):
		t.Errorf("the insert's arguments: %v", c.args)
	}
	if n := got.field("notifications"); n != "Google was asked to email every guest about the new event" {
		t.Errorf("the answer says %q of the notifications", n)
	}
	if got.field("etag") == nil || got.field("html_link") == nil || got.field("start") != "2026-10-02T10:00:00-04:00" {
		t.Errorf("the receipt: %s", got.result)
	}
}

// THE KILL. The insert reaches Google and the plugin's process dies before
// it hears back — the host records an invocation that died, and the
// operator is asked again. A new process, which remembers nothing, derives
// the same id from the same event: Google's 409 is read back, and the one
// event is the answer. No storage is involved; the stand-in fails any
// attempt to use some.
func TestCreateIsOneEventAcrossARestart(t *testing.T) {
	g := newGoogle(t, newYork)
	g.at[1] = thenKilled
	h := &standIn{t: t, handle: handle, google: g.handle}
	first := start(t)
	if got := first.invoke(h, "calendar.create", dentist(nil)); got.status != "killed" {
		t.Fatalf("the first create answered %s", got.line())
	}
	revived := start(t)
	got := revived.invoke(h, "calendar.create", dentist(nil))
	check(t, "the create asked again after the kill", got, "succeeded", "", `"outcome":"created_earlier"`)
	if got.field("event_id") != dentistID || len(g.events) != 1 || g.events[dentistID] == nil {
		t.Errorf("after the kill: %s, and Google holds %d events", got.result, len(g.events))
	}
	if want := []string{"http.post", "http.post", "http.get"}; !reflect.DeepEqual(g.ops(), want) {
		t.Errorf("Google was asked %v, want %v", g.ops(), want)
	}
	if !strings.Contains(got.said(), "whether the earlier create emailed its guests is not known here") {
		t.Errorf("the settled create says nothing of the earlier notifications: %s", got.result)
	}

	// The same within one process: an answer lost, then asked again.
	g2 := newGoogle(t, newYork)
	g2.at[1] = thenLost
	h.google = g2.handle
	check(t, "a create whose answer was lost", revived.invoke(h, "calendar.create", dentist(nil)), "failed", effectUnknown, "its answer was lost")
	again := revived.invoke(h, "calendar.create", dentist(nil))
	check(t, "the same create asked again", again, "succeeded", "", `"outcome":"created_earlier"`)
	if again.field("event_id") != dentistID || len(g2.events) != 1 {
		t.Errorf("asked again: %s, and Google holds %d events", again.result, len(g2.events))
	}
}

// The id is the event: legal for Google (base32hex, 5 to 1024 characters)
// whatever the title, the same for the same intended event however its
// arguments are spelled, and another when the event differs — or when the
// distinguishing word does.
func TestTheDerivedID(t *testing.T) {
	ny := zone(t, newYork)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 2, h, m, 0, 0, ny) }
	base := eventID("primary", at(10, 0), at(11, 0), "Dentist", "")
	if base != dentistID {
		t.Fatalf("the derivation moved: %s, want %s", base, dentistID)
	}
	berlin := zone(t, "Europe/Berlin")
	for _, tc := range []struct {
		name string
		id   string
		same bool
	}{
		{"the same instants spelled in another zone", eventID("primary", at(10, 0).In(berlin), at(11, 0).In(berlin), "Dentist", ""), true},
		{"a distinguishing word", eventID("primary", at(10, 0), at(11, 0), "Dentist", "2"), false},
		{"a start a minute later", eventID("primary", at(10, 1), at(11, 0), "Dentist", ""), false},
		{"an end a minute later", eventID("primary", at(10, 0), at(11, 1), "Dentist", ""), false},
		{"another title", eventID("primary", at(10, 0), at(11, 0), "dentist", ""), false},
		{"another calendar", eventID("recipient@example.com", at(10, 0), at(11, 0), "Dentist", ""), false},
		{"a title and a word that trade characters", eventID("primary", at(10, 0), at(11, 0), "Dentist2", ""), false},
	} {
		if (tc.id == base) != tc.same {
			t.Errorf("%s: %s against %s, want same=%v", tc.name, tc.id, base, tc.same)
		}
	}

	// Through the plugin: awkward titles make legal ids — the scripted
	// Google refuses any other with a 400 — and an absent word, an empty
	// one and a title's surrounding spaces change nothing.
	g := newGoogle(t, newYork)
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	for _, title := range []string{"Café ☕ 会議 — \"quoted\" \\ / | ; :", strings.Repeat("長", 1024), "1:x;2:y"} {
		got := p.invoke(h, "calendar.create", dentist(map[string]any{"title": title}))
		id, _ := got.field("event_id").(string)
		if got.status != "succeeded" || !regexp.MustCompile(`^[0-9a-v]{52}$`).MatchString(id) {
			t.Errorf("title %.20q: %s, id %q", title, got.line(), id)
		}
	}
	for _, tc := range []struct {
		name    string
		changes map[string]any
	}{
		{"an empty distinguishing word", map[string]any{"distinct": ""}},
		{"a title with spaces around it", map[string]any{"title": "  Dentist  "}},
		{"seconds spelled out", map[string]any{"start": "2026-10-02T10:00:00", "end": "2026-10-02T11:00:00"}},
	} {
		if got := p.invoke(h, "calendar.create", dentist(tc.changes)); got.field("event_id") != dentistID {
			t.Errorf("%s: %s, want the id %s", tc.name, got.line(), dentistID)
		}
	}
}

// Two deliberately identical events: the second is the first asked again
// until a distinguishing word makes it another event. A deleted event
// keeps its id, and the same word makes it anew.
func TestADistinctWordMakesAnotherEvent(t *testing.T) {
	g := newGoogle(t, newYork)
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	check(t, "the first", p.invoke(h, "calendar.create", dentist(nil)), "succeeded", "", `"outcome":"created"`)
	check(t, "the same again", p.invoke(h, "calendar.create", dentist(nil)), "succeeded", "", `"outcome":"created_earlier"`)
	second := p.invoke(h, "calendar.create", dentist(map[string]any{"distinct": "second"}))
	check(t, "the same with a word", second, "succeeded", "", `"outcome":"created"`)
	if second.field("event_id") == dentistID || len(g.events) != 2 {
		t.Errorf("a distinguishing word made %v; Google holds %d events", second.field("event_id"), len(g.events))
	}

	check(t, "the first deleted", p.invoke(h, "calendar.delete", act(map[string]any{"event_id": dentistID, "event_title": "Dentist", "notify": "none"})), "succeeded", "", `"outcome":"deleted"`)
	check(t, "the first asked again after its delete", p.invoke(h, "calendar.create", dentist(nil)), "failed", argInvalid, "belongs to an event deleted from this calendar")
	anew := p.invoke(h, "calendar.create", dentist(map[string]any{"distinct": "again"}))
	check(t, "made anew with a word", anew, "succeeded", "", `"outcome":"created"`)
	if g.events[dentistID]["status"] != "cancelled" {
		t.Errorf("the deleted event was touched: %v", g.events[dentistID])
	}
}

// ---- every write waits for the operator ----

// No write runs without the host's stamp, and none asks the host for
// anything first — not even its settings.
func TestWritesWaitForTheOperator(t *testing.T) {
	h := &standIn{t: t, handle: handle, google: func(c httpCall) brokerReply {
		t.Errorf("Google was asked %s %s", c.op, c.url)
		return backend
	}}
	p := start(t)
	for op, args := range map[string]map[string]any{
		"calendar.create": dentist(nil),
		"calendar.update": act(map[string]any{"event_id": dentistID, "event_title": "Dentist", "version": `"1"`, "title": "Dentist 2", "notify": "none"}),
		"calendar.delete": act(map[string]any{"event_id": dentistID, "event_title": "Dentist", "notify": "none"}),
	} {
		delete(args, "_host_operator_act")
		h.asked = nil
		got := p.invoke(h, op, args)
		check(t, op+" unstamped", got, "denied", "POLICY_DENY", "the operator has not confirmed this — a calendar write runs only once the operator confirms it on the Plugins page")
		if len(h.asked) != 0 {
			t.Errorf("%s unstamped asked the host %v", op, h.asked)
		}
	}
}

// Arguments that cannot be carried out are refused, by name, before
// Google is asked anything.
func TestWritesRefuseWhatCannotBeCarriedOut(t *testing.T) {
	h := &standIn{t: t, handle: handle, google: func(c httpCall) brokerReply {
		t.Errorf("Google was asked %s %s", c.op, c.url)
		return backend
	}}
	p := start(t)
	upd := func(changes map[string]any) map[string]any {
		a := map[string]any{"event_id": "e1", "event_title": "Dentist", "version": `"1"`, "title": "Dentist 2", "notify": "none"}
		for k, v := range changes {
			if v == nil {
				delete(a, k)
			} else {
				a[k] = v
			}
		}
		return act(a)
	}
	for _, tc := range []struct {
		name, op string
		args     map[string]any
		says     string
	}{
		{"no title", "calendar.create", dentist(map[string]any{"title": nil}), "create names a title — the event was not created"},
		{"a title of spaces", "calendar.create", dentist(map[string]any{"title": "   "}), "title is 1 to 1024 characters — the event was not created"},
		{"no times", "calendar.create", dentist(map[string]any{"start": nil, "end": nil, "time_zone": nil}), "create names start, end and time_zone — the event was not created"},
		{"an end before the start", "calendar.create", dentist(map[string]any{"end": "2026-10-02T09:00"}), "end is not after start — the event was not created"},
		{"a time with an offset instead of a zone", "calendar.create", dentist(map[string]any{"start": "2026-10-02T10:00:00-04:00"}), `start "2026-10-02T10:00:00-04:00" is not a local date and time such as 2026-10-02T10:00 — the event was not created`},
		{"a time the clock skips", "calendar.create", dentist(map[string]any{"start": "2026-03-08T02:30", "end": "2026-03-08T03:30"}), "start 2026-03-08T02:30 does not happen in America/New_York: the clock skips it — the event was not created"},
		{"a time the clock passes twice", "calendar.create", dentist(map[string]any{"start": "2026-11-01T01:30", "end": "2026-11-01T03:00"}), "start 2026-11-01T01:30 happens twice in America/New_York: the clock goes back over it — the event was not created"},
		{"a zone no one keeps", "calendar.create", dentist(map[string]any{"time_zone": "Mars/Olympus"}), `time_zone "Mars/Olympus" is not a zone this plugin knows (an IANA name such as Europe/Zurich) — the event was not created`},
		{"the host's own zone", "calendar.create", dentist(map[string]any{"time_zone": "Local"}), `time_zone "Local" is not a zone this plugin knows`},
		{"no notify choice", "calendar.create", dentist(map[string]any{"notify": nil}), "notify is none, externalOnly or all — the event was not created"},
		{"a guest who is not an address", "calendar.create", dentist(map[string]any{"attendees": []any{"recipient"}}), `attendee "recipient" is not an email address — the event was not created`},
		{"an update with a start alone", "calendar.update", upd(map[string]any{"start": "2026-10-02T11:00"}), "start, end and time_zone are named together — nothing was changed"},
		{"an update that changes nothing", "calendar.update", upd(map[string]any{"title": nil}), "update names at least one of title, start with end and time_zone, location or description — nothing was changed"},
		{"an update bound to no version", "calendar.update", upd(map[string]any{"version": nil}), "version is the event's etag, as the read that gave its id gave it — nothing was changed"},
		{"an update naming no event", "calendar.update", upd(map[string]any{"event_id": nil}), "event_id is the event's id, as a read of the calendar gave it — nothing was changed"},
		{"a delete naming no title", "calendar.delete", act(map[string]any{"event_id": "e1", "notify": "none"}), "event_title is the event's title, as a read of the calendar gave it — nothing was deleted"},
		{"a delete with no notify choice", "calendar.delete", act(map[string]any{"event_id": "e1", "event_title": "Dentist"}), "notify is none, externalOnly or all — nothing was deleted"},
	} {
		h.asked = nil
		got := p.invoke(h, tc.op, tc.args)
		check(t, tc.name, got, "failed", argInvalid, tc.says)
		if len(h.asked) != 0 {
			t.Errorf("%s: the host was asked %v first", tc.name, h.asked)
		}
	}
}

// ---- update ----

// seedEvent is the event the updates and deletes name: a timed event with
// fields the changes must leave alone.
func seedEvent(g *google) map[string]any {
	ev := timed("e1", "Dentist", "2026-10-02T10:00:00-04:00", "2026-10-02T11:00:00-04:00", newYork)
	ev["location"], ev["description"] = "123 Example Street", "bring the form"
	ev["attendees"] = []any{map[string]any{"email": "recipient@example.com", "responseStatus": "accepted"}}
	return g.put(ev)
}

func moveIt(version any, changes map[string]any) map[string]any {
	a := map[string]any{"event_id": "e1", "event_title": "Dentist", "version": version,
		"start": "2026-10-02T11:00", "end": "2026-10-02T12:00", "time_zone": newYork, "notify": "none"}
	for k, v := range changes {
		a[k] = v
	}
	return act(a)
}

// An update reads the event, is bound to the version that was read by
// If-Match, sends only the fields it names, and Google keeps the rest.
func TestUpdateChangesOnlyWhatItNames(t *testing.T) {
	g := newGoogle(t, newYork)
	ev := seedEvent(g)
	version := ev["etag"]
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	got := p.invoke(h, "calendar.update", moveIt(version, map[string]any{"title": "Dentist (moved)", "notify": "all"}))
	check(t, "a move", got, "succeeded", "", `"outcome":"updated"`)
	if want := []string{"http.get", "http.patch"}; !reflect.DeepEqual(g.ops(), want) {
		t.Fatalf("Google was asked %v, want %v", g.ops(), want)
	}
	c := g.calls[1]
	wantBody := map[string]any{"summary": "Dentist (moved)",
		"start": map[string]any{"dateTime": "2026-10-02T11:00:00-04:00", "timeZone": newYork},
		"end":   map[string]any{"dateTime": "2026-10-02T12:00:00-04:00", "timeZone": newYork}}
	switch {
	case c.header("If-Match") != version:
		t.Errorf("the patch is bound to If-Match %q, want the version read, %v", c.header("If-Match"), version)
	case c.query().Get("sendUpdates") != "all":
		t.Errorf("the patch asks sendUpdates=%q", c.query().Get("sendUpdates"))
	case !reflect.DeepEqual(c.body(), wantBody):
		t.Errorf("the patch sends %v, want only %v", c.body(), wantBody)
	case !strings.HasSuffix(strings.Split(c.url, "?")[0], "/calendars/primary/events/e1"):
		t.Errorf("the patch targets %s", c.url)
	}
	after := g.events["e1"]
	if after["location"] != "123 Example Street" || after["description"] != "bring the form" || len(after["attendees"].([]any)) != 1 {
		t.Errorf("fields the change did not name were lost: %v", after)
	}
	if got.field("etag") != after["etag"] || !reflect.DeepEqual(got.field("changed"), []any{"title", "start", "end", "time_zone"}) ||
		got.field("notifications") != "Google was asked to email every guest about this change" {
		t.Errorf("the receipt: %s", got.result)
	}
}

// Every answer Google can give the patch, and the update's verdict. The
// change is written at most once.
func TestUpdateAnswersWhatGoogleDid(t *testing.T) {
	cases := []struct {
		name   string
		script map[int]step
		status string
		code   string
		says   string
		ops    []string
	}{
		{"a change bound to the version read is made", nil,
			"succeeded", "", `"outcome":"updated"`, []string{"http.get", "http.patch"}},
		{"an event changed between the read and the write is Google's 412: stopped, read again, said", map[int]step{2: func(g *google, c httpCall) brokerReply {
			g.events["e1"]["summary"], g.events["e1"]["etag"] = "Dentist — Clinic B", g.nextTag()
			return g.serve(c)
		}}, "failed", argInvalid, `the event changed since it was read: it is now "Dentist — Clinic B" from 2026-10-02T10:00:00-04:00 to 2026-10-02T11:00:00-04:00, version "3181161784712002" — nothing was changed; ask again from what it is now`, []string{"http.get", "http.patch", "http.get"}},
		{"a 412 whose read again fails still stops", map[int]step{2: replies(gave(412, gerr(412, "conditionNotMet", "Precondition Failed"), -1)), 3: replies(backend)},
			"failed", argInvalid, "the event changed since it was read (Google answered 412: Precondition Failed) and could not be read again — nothing was changed; read it and ask again", []string{"http.get", "http.patch", "http.get"}},
		{"an event deleted between the read and the write is gone", map[int]step{2: func(g *google, c httpCall) brokerReply {
			g.events["e1"]["status"] = "cancelled"
			return g.serve(c)
		}}, "failed", argInvalid, "Google says the event is gone (410: Resource has been deleted) — nothing was changed", []string{"http.get", "http.patch"}},
		{"a 400 refuses these arguments", map[int]step{2: replies(gave(400, gerr(400, "invalid", "Invalid Value"), -1))},
			"failed", argInvalid, "Google refused the change (400: Invalid Value) — nothing was changed", []string{"http.get", "http.patch"}},
		{"a 403 changed nothing", map[int]step{2: replies(gave(403, gerr(403, "forbiddenForNonOrganizer", "Shared properties can only be changed by the organizer of the event."), -1))},
			"failed", remoteFailed, "Google refused it (403: Shared properties can only be changed by the organizer of the event.) — nothing was changed", []string{"http.get", "http.patch"}},
		{"a 429 changed nothing and carries the wait", map[int]step{2: replies(gave(429, gerr(429, "rateLimitExceeded", "Rate Limit Exceeded"), 12))},
			"failed", remoteFailed, "and asks to wait 12 s — nothing was changed", []string{"http.get", "http.patch"}},
		{"a 500 can follow the write", map[int]step{2: replies(backend)},
			"failed", effectUnknown, "Google answered 500: Backend Error — the change may have been made; read the event before asking again", []string{"http.get", "http.patch"}},
		{"a change written and unanswered may have been made", map[int]step{2: thenLost},
			"failed", effectUnknown, "the change was written and its answer was lost — the change may have been made; read the event before asking again", []string{"http.get", "http.patch"}},
		{"a 200 that shows no event may have made it", map[int]step{2: replies(gave(200, `{}`, -1))},
			"failed", effectUnknown, "Google answered 200 and the answer does not show the event — the change may have been made", []string{"http.get", "http.patch"}},
		{"a patch never written changed nothing", map[int]step{2: replies(notWritten)},
			"failed", remoteFailed, "Google was not reached — nothing was changed", []string{"http.get", "http.patch"}},
		{"the host's denial of the patch is relayed", map[int]step{2: replies(deniedBy("POLICY_DENY"))},
			"denied", "POLICY_DENY", "the host refused the call (not granted here) — nothing was changed", []string{"http.get", "http.patch"}},
		{"an account disconnected at the patch is relayed, typed", map[int]step{2: replies(refusedWith("NET_AUTH_PROFILE_DISCONNECTED"))},
			"denied", "NET_AUTH_PROFILE_DISCONNECTED", "connects it again on the Plugins page; nothing was changed", []string{"http.get", "http.patch"}},
		{"an account disconnected at the read is relayed, and nothing is written", map[int]step{1: replies(refusedWith("NET_AUTH_PROFILE_DISCONNECTED"))},
			"denied", "NET_AUTH_PROFILE_DISCONNECTED", "connects it again on the Plugins page; nothing was changed", []string{"http.get"}},
		{"a read that fails writes nothing", map[int]step{1: replies(backend)},
			"failed", remoteFailed, "the event could not be read first (Google answered 500: Backend Error) — nothing was changed", []string{"http.get"}},
	}
	var g *google
	h := &standIn{t: t, handle: handle, google: func(c httpCall) brokerReply { return g.handle(c) }}
	p := start(t)
	for _, tc := range cases {
		g = newGoogle(t, newYork)
		version := seedEvent(g)["etag"]
		for n, s := range tc.script {
			g.at[n] = s
		}
		got := p.invoke(h, "calendar.update", moveIt(version, nil))
		check(t, tc.name, got, tc.status, tc.code, tc.says)
		if !reflect.DeepEqual(g.ops(), tc.ops) {
			t.Errorf("%s: Google was asked %v, want %v", tc.name, g.ops(), tc.ops)
		}
		if got.status != "succeeded" {
			t.Logf("%-78s %s", tc.name, got.line())
		}
	}
}

// The approved basis is the version that was read, and the event's title
// as the card showed it; a gone event is gone. Each stops before writing.
func TestUpdateStopsWhenTheBasisChanged(t *testing.T) {
	var g *google
	h := &standIn{t: t, handle: handle, google: func(c httpCall) brokerReply { return g.handle(c) }}
	p := start(t)
	for _, tc := range []struct {
		name  string
		seed  func(*google) any
		args  func(version any) map[string]any
		says  string
		patch int
	}{
		{"a version older than the event's", func(g *google) any { return seedEvent(g)["etag"] },
			func(any) map[string]any { return moveIt(`"3181161784711999"`, nil) },
			`the event changed since it was read: it is now "Dentist" from 2026-10-02T10:00:00-04:00 to 2026-10-02T11:00:00-04:00, version "3181161784712001" — nothing was changed; ask again from what it is now`, 0},
		{"a title the event does not have", func(g *google) any { return seedEvent(g)["etag"] },
			func(v any) map[string]any { return moveIt(v, map[string]any{"event_title": "Dentist (Recipient)"}) },
			`event e1 is titled "Dentist", not "Dentist (Recipient)" as the change names it — nothing was changed`, 0},
		{"an event Google holds as deleted", func(g *google) any { ev := seedEvent(g); ev["status"] = "cancelled"; return ev["etag"] },
			func(v any) map[string]any { return moveIt(v, nil) },
			"event e1 was deleted from this calendar — nothing was changed", 0},
		{"an event Google has never held", func(g *google) any { return `"1"` },
			func(v any) map[string]any { return moveIt(v, nil) },
			"Google has no event e1 on this calendar — nothing was changed; read the calendar for the event's id", 0},
		{"a repeating series, not one occurrence", func(g *google) any {
			ev := seedEvent(g)
			ev["recurrence"] = []any{"RRULE:FREQ=WEEKLY"}
			return ev["etag"]
		},
			func(v any) map[string]any { return moveIt(v, nil) },
			"event e1 is a repeating series, and this plugin changes one occurrence at a time — name the occurrence's id, as a read of the calendar gives it; nothing was changed", 0},
	} {
		g = newGoogle(t, newYork)
		version := tc.seed(g)
		got := p.invoke(h, "calendar.update", tc.args(version))
		check(t, tc.name, got, "failed", argInvalid, tc.says)
		if g.count("http.patch") != tc.patch {
			t.Errorf("%s: %d patches", tc.name, g.count("http.patch"))
		}
	}
}

// An update asked again after its answer was lost is bound to the version
// the first was bound to: the first was made, so the event is a later
// version, and the second stops at the read — it neither writes nor emails
// the guests again — and says the event already holds the change.
func TestAnUpdateAskedAgainDoesNotWriteAgain(t *testing.T) {
	g := newGoogle(t, newYork)
	version := seedEvent(g)["etag"]
	g.at[2] = thenLost
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	check(t, "the first", p.invoke(h, "calendar.update", moveIt(version, map[string]any{"notify": "all"})), "failed", effectUnknown, "its answer was lost")
	check(t, "asked again", p.invoke(h, "calendar.update", moveIt(version, map[string]any{"notify": "all"})), "failed", argInvalid,
		"nothing was changed, and it already holds every value this change names; ask again from what it is now")
	if g.count("http.patch") != 1 {
		t.Errorf("the change was written %d times", g.count("http.patch"))
	}
}

// ---- delete ----

func dropIt(changes map[string]any) map[string]any {
	a := map[string]any{"event_id": "e1", "event_title": "Dentist", "notify": "none"}
	for k, v := range changes {
		a[k] = v
	}
	return act(a)
}

// Every answer Google can give the delete, and the read that confirms it:
// the delete's verdict. A read that fails proves nothing either way.
func TestDeleteConfirmsWhatGoogleDid(t *testing.T) {
	cases := []struct {
		name   string
		script map[int]step
		status string
		code   string
		says   string
		ops    []string
	}{
		{"an empty 204, and a read that finds the event cancelled", nil,
			"succeeded", "", `"confirmation":"Google took the delete (204), and a read then found the event gone (held as cancelled)","confirmed":true`, []string{"http.get", "http.delete", "http.get"}},
		{"a read that finds no event at all confirms it too", map[int]step{3: replies(gave(404, gerr(404, "notFound", "Not Found"), -1))},
			"succeeded", "", `a read then found the event gone (404)","confirmed":true`, []string{"http.get", "http.delete", "http.get"}},
		{"a read answered 410 confirms it", map[int]step{3: replies(gave(410, gerr(410, "deleted", "Resource has been deleted"), -1))},
			"succeeded", "", `(410)","confirmed":true`, []string{"http.get", "http.delete", "http.get"}},
		{"a read that fails is not proof: taken, not confirmed, said", map[int]step{3: replies(backend)},
			"succeeded", "", `"confirmation":"Google took the delete (204); the read to confirm it failed (Google answered 500: Backend Error), so the deletion is not confirmed","confirmed":false`, []string{"http.get", "http.delete", "http.get"}},
		{"a read refused with 401 is not proof", map[int]step{3: replies(gave(401, gerr(401, "authError", "Invalid Credentials"), -1))},
			"succeeded", "", `the read to confirm it failed (Google answered 401: Invalid Credentials), so the deletion is not confirmed","confirmed":false`, []string{"http.get", "http.delete", "http.get"}},
		{"a read the host refuses is not proof", map[int]step{3: replies(refusedWith("NET_AUTH_PROFILE_DISCONNECTED"))},
			"succeeded", "", `the read to confirm it failed (the host refused it (NET_AUTH_PROFILE_DISCONNECTED)), so the deletion is not confirmed","confirmed":false`, []string{"http.get", "http.delete", "http.get"}},
		{"a read whose answer is lost is not proof", map[int]step{3: replies(lostAnswer)},
			"succeeded", "", `(the answer was lost), so the deletion is not confirmed","confirmed":false`, []string{"http.get", "http.delete", "http.get"}},
		{"a read that still finds the event: taken, and not shown gone", map[int]step{2: replies(gave(204, "", -1))},
			"failed", effectUnknown, "Google took the delete (204), and a read right after still finds the event — whether it is deleted is not confirmed; read the calendar before asking again", []string{"http.get", "http.delete", "http.get"}},
		{"an event deleted between the read and the delete: already gone, never another", map[int]step{2: func(g *google, c httpCall) brokerReply {
			g.events["e1"]["status"] = "cancelled"
			return g.serve(c)
		}}, "succeeded", "", `"confirmation":"Google answered 410: Resource has been deleted: the event was gone before this delete","confirmed":true`, []string{"http.get", "http.delete"}},
		{"a delete Google no longer finds: already gone", map[int]step{2: replies(gave(404, gerr(404, "notFound", "Not Found"), -1))},
			"succeeded", "", `"outcome":"already_deleted"`, []string{"http.get", "http.delete"}},
		{"a 403 deleted nothing", map[int]step{2: replies(gave(403, gerr(403, "forbidden", "Forbidden"), -1))},
			"failed", remoteFailed, "Google refused it (403: Forbidden) — nothing was deleted", []string{"http.get", "http.delete"}},
		{"a 429 deleted nothing and carries the wait", map[int]step{2: replies(gave(429, gerr(429, "rateLimitExceeded", "Rate Limit Exceeded"), 5))},
			"failed", remoteFailed, "and asks to wait 5 s — nothing was deleted", []string{"http.get", "http.delete"}},
		{"a 500 can follow the delete", map[int]step{2: replies(backend)},
			"failed", effectUnknown, "Google answered 500: Backend Error — the event may have been deleted; the same delete asked again reads it first", []string{"http.get", "http.delete"}},
		{"a delete written and unanswered may have been made", map[int]step{2: thenLost},
			"failed", effectUnknown, "the delete was written and its answer was lost — the event may have been deleted", []string{"http.get", "http.delete"}},
		{"a delete never written deleted nothing", map[int]step{2: replies(notWritten)},
			"failed", remoteFailed, "Google was not reached — nothing was deleted", []string{"http.get", "http.delete"}},
		{"the host's denial is relayed", map[int]step{2: replies(deniedBy("POLICY_DENY"))},
			"denied", "POLICY_DENY", "the host refused the call (not granted here) — nothing was deleted", []string{"http.get", "http.delete"}},
		{"an account disconnected at the read is relayed, typed, and nothing is deleted", map[int]step{1: replies(refusedWith("NET_AUTH_PROFILE_DISCONNECTED"))},
			"denied", "NET_AUTH_PROFILE_DISCONNECTED", "connects it again on the Plugins page; nothing was deleted", []string{"http.get"}},
		{"an account disconnected at the delete is relayed, typed", map[int]step{2: replies(refusedWith("NET_AUTH_PROFILE_DISCONNECTED"))},
			"denied", "NET_AUTH_PROFILE_DISCONNECTED", "connects it again on the Plugins page; nothing was deleted", []string{"http.get", "http.delete"}},
	}
	var g *google
	h := &standIn{t: t, handle: handle, google: func(c httpCall) brokerReply { return g.handle(c) }}
	p := start(t)
	for _, tc := range cases {
		g = newGoogle(t, newYork)
		seedEvent(g)
		for n, s := range tc.script {
			g.at[n] = s
		}
		got := p.invoke(h, "calendar.delete", dropIt(nil))
		check(t, tc.name, got, tc.status, tc.code, tc.says)
		if !reflect.DeepEqual(g.ops(), tc.ops) {
			t.Errorf("%s: Google was asked %v, want %v", tc.name, g.ops(), tc.ops)
		}
		t.Logf("%-78s %s", tc.name, got.said())
	}
}

// A delete reads the event first: one already deleted is judged against
// the delete — already gone, nothing deleted — an id Google never held is
// refused, never taken as gone, and a title that is not the card's stops
// it. None deletes anything.
func TestDeleteReadsTheEventFirst(t *testing.T) {
	var g *google
	h := &standIn{t: t, handle: handle, google: func(c httpCall) brokerReply { return g.handle(c) }}
	p := start(t)
	for _, tc := range []struct {
		name         string
		seed         func(*google)
		args         map[string]any
		status, code string
		says         string
	}{
		{"an event Google holds as deleted", func(g *google) { seedEvent(g)["status"] = "cancelled" }, dropIt(nil),
			"succeeded", "", `{"confirmation":"a read before the delete found the event already deleted","confirmed":true,"event_id":"e1","notifications":"this call deleted nothing and emailed no one","outcome":"already_deleted"}`},
		{"an id Google never held", func(*google) {}, dropIt(nil),
			"failed", argInvalid, "Google has no event e1 on this calendar — nothing was deleted; read the calendar for the event's id"},
		{"a title that is not the event's", func(g *google) { seedEvent(g) }, dropIt(map[string]any{"event_title": "Standup"}),
			"failed", argInvalid, `event e1 is titled "Dentist", not "Standup" as the delete names it — nothing was deleted`},
		{"a repeating series, not one occurrence", func(g *google) { seedEvent(g)["recurrence"] = []any{"RRULE:FREQ=WEEKLY"} }, dropIt(nil),
			"failed", argInvalid, "event e1 is a repeating series, and this plugin changes one occurrence at a time — name the occurrence's id, as a read of the calendar gives it; nothing was deleted"},
		{"a read that fails", func(g *google) { seedEvent(g); g.at[1] = replies(lostAnswer) }, dropIt(nil),
			"failed", remoteFailed, "the event could not be read first (the answer was lost) — nothing was deleted"},
	} {
		g = newGoogle(t, newYork)
		tc.seed(g)
		check(t, tc.name, p.invoke(h, "calendar.delete", tc.args), tc.status, tc.code, tc.says)
		if g.count("http.delete") != 0 {
			t.Errorf("%s: deleted", tc.name)
		}
	}
}

// A delete asked again after its answer was lost reads the event, finds it
// deleted, and deletes nothing: one cancellation, one email.
func TestADeleteAskedAgainDeletesNothing(t *testing.T) {
	g := newGoogle(t, newYork)
	seedEvent(g)
	g.at[2] = thenLost
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	check(t, "the first", p.invoke(h, "calendar.delete", dropIt(map[string]any{"notify": "all"})), "failed", effectUnknown, "its answer was lost")
	got := p.invoke(h, "calendar.delete", dropIt(map[string]any{"notify": "all"}))
	check(t, "asked again", got, "succeeded", "", `"outcome":"already_deleted"`)
	if g.count("http.delete") != 1 || got.field("notifications") != "this call deleted nothing and emailed no one" {
		t.Errorf("%d deletes; the second says %v", g.count("http.delete"), got.field("notifications"))
	}
	if q := g.calls[1].query(); q.Get("sendUpdates") != "all" || !strings.HasSuffix(strings.Split(g.calls[1].url, "?")[0], "/events/e1") {
		t.Errorf("the delete is %s", g.calls[1].url)
	}
}

// ---- reads ----

// Today is the calendar's whole day, midnight to midnight in its own zone,
// asked for only once the zone is known: 23 hours when the clocks spring
// forward, 25 when they fall back — never a UTC day, and never the host's.
// An all-day event is what Google gives, untouched.
func TestTodayIsTheCalendarsOwnDay(t *testing.T) {
	for _, tc := range []struct {
		name, zone string
		now        time.Time
		from, to   string
		hours      float64
	}{
		{"the day New York springs forward", newYork, time.Date(2026, 3, 8, 22, 30, 0, 0, zone(t, newYork)), "2026-03-08T00:00:00-05:00", "2026-03-09T00:00:00-04:00", 23},
		{"the day Berlin falls back", "Europe/Berlin", time.Date(2026, 10, 25, 23, 30, 0, 0, zone(t, "Europe/Berlin")), "2026-10-25T00:00:00+02:00", "2026-10-26T00:00:00+01:00", 25},
		{"an ordinary evening, when UTC and Tokyo are already tomorrow", newYork, time.Date(2026, 10, 2, 21, 0, 0, 0, zone(t, newYork)), "2026-10-02T00:00:00-04:00", "2026-10-03T00:00:00-04:00", 24},
	} {
		g := newGoogle(t, tc.zone)
		day, _ := time.Parse(time.RFC3339, tc.from)
		date := day.Format("2006-01-02")
		g.put(allDay("holiday", "A holiday", date, day.AddDate(0, 0, 1).Format("2006-01-02")))
		g.put(timed("late", "Late call", tc.from[:11]+"20:00:00"+tc.to[19:], tc.from[:11]+"21:00:00"+tc.to[19:], tc.zone))
		h := &standIn{t: t, handle: handle, google: g.handle}
		p := start(t)
		got := p.invoke(h, "calendar.today", map[string]any{"_host_now_ms": tc.now.UnixMilli()})
		if got.status != "succeeded" {
			t.Fatalf("%s: %s", tc.name, got.line())
		}
		if len(g.calls) != 2 || g.calls[0].query().Get("fields") != "timeZone" {
			t.Fatalf("%s: Google was asked %v; the zone must come first", tc.name, g.calls)
		}
		q := g.calls[1].query()
		from, _ := time.Parse(time.RFC3339, q.Get("timeMin"))
		to, _ := time.Parse(time.RFC3339, q.Get("timeMax"))
		if q.Get("timeMin") != tc.from || q.Get("timeMax") != tc.to || to.Sub(from).Hours() != tc.hours {
			t.Errorf("%s: asked for %s to %s (%v h), want %s to %s (%v h)", tc.name, q.Get("timeMin"), q.Get("timeMax"), to.Sub(from).Hours(), tc.from, tc.to, tc.hours)
		}
		if got.field("day_start") != tc.from || got.field("day_end") != tc.to || got.field("time_zone") != tc.zone {
			t.Errorf("%s: the answer says %v to %v in %v", tc.name, got.field("day_start"), got.field("day_end"), got.field("time_zone"))
		}
		events, _ := got.field("events").(map[string]any)
		items, _ := events["items"].([]any)
		if len(items) != 2 || !strings.Contains(marshal(items[0]), `"start":{"date":"`+date+`"}`) || events["timeZone"] != tc.zone || got.field("truncated") != false {
			t.Errorf("%s: the events: %s", tc.name, got.result)
		}
		if f := q.Get("fields"); f != "items(id,etag,status,summary,start,end,location),nextPageToken,timeZone" {
			t.Errorf("%s: the list asks fields=%s", tc.name, f)
		}
	}
}

// Google may return fewer events than asked while more remain: every page
// is read, each asking the same day. Past the bound the answer says it
// was cut, and its next_page reads the rest of that same day.
func TestTodayReadsEveryPage(t *testing.T) {
	ny := zone(t, newYork)
	now := time.Date(2026, 10, 2, 8, 0, 0, 0, ny).UnixMilli()
	seed := func(g *google, n int) {
		for i := 0; i < n; i++ {
			s := time.Date(2026, 10, 2, 9+i, 0, 0, 0, ny)
			g.put(timed(fmt.Sprintf("ev%d", i), fmt.Sprintf("Meeting %d", i), s.Format(time.RFC3339), s.Add(30*time.Minute).Format(time.RFC3339), newYork))
		}
	}
	g := newGoogle(t, newYork)
	g.pageCap = 2
	seed(g, 5)
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	got := p.invoke(h, "calendar.today", map[string]any{"_host_now_ms": now})
	events, _ := got.field("events").(map[string]any)
	if items, _ := events["items"].([]any); len(items) != 5 || got.field("truncated") != false || got.field("next_page") != nil {
		t.Fatalf("five events in pages of two: %s", got.result)
	}
	if n := len(g.calls); n != 4 {
		t.Errorf("%d exchanges, want the zone and three pages", n)
	}
	for _, c := range g.calls[1:] {
		if q := c.query(); q.Get("timeMin") != "2026-10-02T00:00:00-04:00" || q.Get("timeMax") != "2026-10-03T00:00:00-04:00" || q.Get("maxResults") != "100" {
			t.Errorf("a page asks %v", q)
		}
	}

	g = newGoogle(t, newYork)
	g.pageCap = 2
	seed(g, 7)
	h.google = g.handle
	cut := p.invoke(h, "calendar.today", map[string]any{"_host_now_ms": now})
	events, _ = cut.field("events").(map[string]any)
	next, _ := cut.field("next_page").(string)
	if items, _ := events["items"].([]any); len(items) != 6 || cut.field("truncated") != true || next == "" {
		t.Fatalf("seven events past the bound: %s", cut.result)
	}
	rest := p.invoke(h, "calendar.today", map[string]any{"_host_now_ms": now, "page": next})
	events, _ = rest.field("events").(map[string]any)
	if items, _ := events["items"].([]any); rest.status != "succeeded" || len(items) != 1 || rest.field("truncated") != false || rest.field("day_start") != "2026-10-02T00:00:00-04:00" {
		t.Errorf("the rest of the day: %s %s", rest.status, rest.said())
	}
	if n := len(g.calls); n != 5 || g.calls[4].query().Get("pageToken") == "" {
		t.Errorf("reading on made %d exchanges: %v", n, g.calls[4].url)
	}
	check(t, "a page this plugin never gave", p.invoke(h, "calendar.today", map[string]any{"_host_now_ms": now, "page": "yesterday"}), "failed", argInvalid, "page is not a next_page this plugin gave — nothing was read")
}

// The next few events keep the shape the example always answered —
// Google's own items under events, the status and the effect — read from
// the host's now, page by page until the number asked is in hand.
func TestUpcomingKeepsItsShape(t *testing.T) {
	ny := zone(t, newYork)
	now := time.Date(2026, 10, 2, 8, 0, 0, 0, ny)
	g := newGoogle(t, newYork)
	for i := 0; i < 6; i++ {
		s := now.Add(time.Duration(i+1) * time.Hour)
		g.put(timed(fmt.Sprintf("ev%d", i), "Meeting", s.Format(time.RFC3339), s.Add(time.Hour).Format(time.RFC3339), newYork))
	}
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	got := p.invoke(h, "calendar.upcoming", map[string]any{"limit": 3, "_host_now_ms": now.UnixMilli()})
	events, _ := got.field("events").(map[string]any)
	items, _ := events["items"].([]any)
	if got.field("http_status") != float64(200) || got.field("effect") != "performed" || len(items) != 3 || got.field("truncated") != false {
		t.Errorf("upcoming: %s", got.result)
	}
	q := g.calls[0].query()
	if q.Get("timeMin") != "2026-10-02T12:00:00Z" || q.Has("timeMax") || q.Get("maxResults") != "3" || len(g.calls) != 1 {
		t.Errorf("upcoming asks %v in %d exchanges", q, len(g.calls))
	}

	g.pageCap = 1
	g.calls = nil
	cut := p.invoke(h, "calendar.upcoming", map[string]any{"limit": 5, "_host_now_ms": now.UnixMilli()})
	events, _ = cut.field("events").(map[string]any)
	items, _ = events["items"].([]any)
	next, _ := cut.field("next_page").(string)
	if len(items) != 3 || cut.field("truncated") != true || next == "" || len(g.calls) != 3 {
		t.Fatalf("pages of one, five asked: %s", cut.result)
	}
	rest := p.invoke(h, "calendar.upcoming", map[string]any{"limit": 5, "page": next})
	events, _ = rest.field("events").(map[string]any)
	if items, _ = events["items"].([]any); len(items) != 3 || rest.field("truncated") != false {
		t.Errorf("reading on: %s", rest.said())
	}
	check(t, "no host clock", p.invoke(h, "calendar.upcoming", map[string]any{}), "failed", argInvalid, "the call carries no host clock (_host_now_ms), so now cannot be placed — nothing was read")
}

// A read that finds nothing says so in one code, NET_REMOTE_FAILED — a
// read has no effect to be unsure of — and relays the host's refusals with
// theirs: an account not connected is named, with the fix, whether the
// zone or the list meets it.
func TestReadsSayWhyTheyFoundNothing(t *testing.T) {
	ny := zone(t, newYork)
	now := time.Date(2026, 10, 2, 8, 0, 0, 0, ny).UnixMilli()
	var g *google
	h := &standIn{t: t, handle: handle, google: func(c httpCall) brokerReply { return g.handle(c) }}
	p := start(t)
	window := map[string]any{"from": "2026-10-02T09:00", "to": "2026-10-02T17:00", "minutes": 30}
	for _, tc := range []struct {
		name, op     string
		args         map[string]any
		at           int
		reply        brokerReply
		status, code string
		says         string
	}{
		{"today, the zone refused: not connected", "calendar.today", map[string]any{"_host_now_ms": now}, 1, refusedWith("NET_AUTH_PROFILE_DISCONNECTED"),
			"denied", "NET_AUTH_PROFILE_DISCONNECTED", "the Google account is not connected, or Google no longer accepts it — the operator connects it again on the Plugins page; nothing was read"},
		{"today, the list refused: not connected", "calendar.today", map[string]any{"_host_now_ms": now}, 2, refusedWith("NET_AUTH_PROFILE_DISCONNECTED"),
			"denied", "NET_AUTH_PROFILE_DISCONNECTED", "connects it again on the Plugins page; nothing was read"},
		{"upcoming, not connected", "calendar.upcoming", map[string]any{"_host_now_ms": now}, 1, refusedWith("NET_AUTH_PROFILE_DISCONNECTED"),
			"denied", "NET_AUTH_PROFILE_DISCONNECTED", "connects it again on the Plugins page; nothing was read"},
		{"availability, not connected", "calendar.availability", window, 1, refusedWith("NET_AUTH_PROFILE_DISCONNECTED"),
			"denied", "NET_AUTH_PROFILE_DISCONNECTED", "connects it again on the Plugins page; nothing was read"},
		{"a refresh that failed just now", "calendar.upcoming", map[string]any{"_host_now_ms": now}, 1, refusedWith("NET_AUTH_PROFILE_UNAVAILABLE"),
			"denied", "NET_AUTH_PROFILE_UNAVAILABLE", "the Google account's access could not be refreshed just now — try again later; nothing was read"},
		{"another credential refusal", "calendar.upcoming", map[string]any{"_host_now_ms": now}, 1, refusedWith("NET_AUTH_PROFILE_NOT_ADMITTED"),
			"denied", "NET_AUTH_PROFILE_NOT_ADMITTED", "the host could not use the Google account (NET_AUTH_PROFILE_NOT_ADMITTED) — check the google profile on the Plugins page; nothing was read"},
		{"no grant", "calendar.today", map[string]any{"_host_now_ms": now}, 1, deniedBy("POLICY_DENY"),
			"denied", "POLICY_DENY", "the host refused the call (not granted here) — nothing was read"},
		{"a calendar Google does not have", "calendar.upcoming", map[string]any{"_host_now_ms": now}, 1, gave(404, gerr(404, "notFound", "Not Found"), -1),
			"failed", remoteFailed, "Google answered 404: Not Found — nothing was read"},
		{"a rate limit", "calendar.upcoming", map[string]any{"_host_now_ms": now}, 1, gave(429, gerr(429, "rateLimitExceeded", "Rate Limit Exceeded"), 7),
			"failed", remoteFailed, "Google refused it (429: Rate Limit Exceeded) and asks to wait 7 s — nothing was read"},
		{"a lost answer", "calendar.today", map[string]any{"_host_now_ms": now}, 2, lostAnswer,
			"failed", remoteFailed, "the answer was lost — nothing was read"},
		{"a 200 that is not Google's JSON", "calendar.upcoming", map[string]any{"_host_now_ms": now}, 1, gave(200, "<html>", -1),
			"failed", remoteFailed, "Google answered 200 and the answer could not be read — nothing was read"},
		{"a zone this plugin cannot place", "calendar.today", map[string]any{"_host_now_ms": now}, 1, gave(200, `{"timeZone":"Mars/Olympus"}`, -1),
			"failed", remoteFailed, `Google named the calendar's time zone "Mars/Olympus", which this plugin cannot place — nothing was read`},
		{"no host clock", "calendar.today", map[string]any{}, 0, brokerReply{},
			"failed", argInvalid, "the call carries no host clock (_host_now_ms), so now cannot be placed — nothing was read"},
	} {
		g = newGoogle(t, newYork)
		if tc.at > 0 {
			g.at[tc.at] = replies(tc.reply)
		}
		got := p.invoke(h, tc.op, tc.args)
		check(t, tc.name, got, tc.status, tc.code, tc.says)
		t.Logf("%-50s %s", tc.name, got.line())
	}

	// Without the handle nothing is asked of Google.
	h.handle, g = "", newGoogle(t, newYork)
	check(t, "no handle", p.invoke(h, "calendar.today", map[string]any{"_host_now_ms": now}), "failed", argInvalid,
		"the google handle is not set — connect a Google account on the Plugins page and set the google setting to the profile's name; nothing was read")
	if len(g.calls) != 0 {
		t.Errorf("Google was asked %v without a handle", g.ops())
	}
}

// Free time is this calendar's own: the window less what its events hold —
// an event shown as free, one its owner declined and one cancelled hold
// nothing, an all-day event that is busy holds its whole day — in the
// calendar's zone, measured in real time across a clock change, and said
// to be this calendar alone.
func TestAvailabilityIsThisCalendarAlone(t *testing.T) {
	g := newGoogle(t, newYork)
	at := func(hm string) string { return "2026-10-02T" + hm + ":00-04:00" }
	g.put(timed("a", "Standup", at("09:30"), at("10:00"), newYork))
	g.put(timed("b", "Review", at("10:00"), at("11:00"), newYork))
	g.put(timed("c", "Call", at("12:00"), at("12:20"), newYork))
	free := g.put(timed("d", "Focus", at("13:00"), at("14:00"), newYork))
	free["transparency"] = "transparent"
	no := g.put(timed("e", "Declined", at("14:00"), at("15:00"), newYork))
	no["attendees"] = []any{map[string]any{"email": "me@example.com", "self": true, "responseStatus": "declined"}}
	g.put(timed("f", "Late", at("16:00"), at("17:30"), newYork))
	holiday := g.put(allDay("g", "Office closed?", "2026-10-02", "2026-10-03"))
	holiday["transparency"] = "transparent"
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	got := p.invoke(h, "calendar.availability", map[string]any{"from": "2026-10-02T09:00", "to": "2026-10-02T17:00", "minutes": 30})
	want := []any{
		map[string]any{"start": at("09:00"), "end": at("09:30"), "minutes": float64(30)},
		map[string]any{"start": at("11:00"), "end": at("12:00"), "minutes": float64(60)},
		map[string]any{"start": at("12:20"), "end": at("16:00"), "minutes": float64(220)},
	}
	if got.status != "succeeded" || !reflect.DeepEqual(got.field("free"), want) || got.field("truncated") != false || got.field("time_zone") != newYork {
		t.Errorf("free time:\n  %s\nwant %v", got.said(), want)
	}
	if s, _ := got.field("scope").(string); !strings.Contains(s, "this says nothing about any other person's time, or whether anyone would accept") {
		t.Errorf("the answer does not name its scope: %q", s)
	}
	q := g.calls[1].query()
	if q.Get("timeMin") != at("09:00") || q.Get("timeMax") != at("17:00") || !strings.Contains(q.Get("fields"), "transparency") || g.calls[0].query().Get("fields") != "timeZone" {
		t.Errorf("availability asks %v after %v", q, g.calls[0].url)
	}

	// A busy all-day event holds the whole day; a night the clocks fall
	// back is six real hours from 22:00 to 03:00.
	g.put(allDay("h", "Offsite", "2026-10-05", "2026-10-06"))
	check(t, "a busy all-day event", p.invoke(h, "calendar.availability", map[string]any{"from": "2026-10-05T09:00", "to": "2026-10-05T17:00", "minutes": 15}), "succeeded", "", `"free":[]`)
	night := p.invoke(h, "calendar.availability", map[string]any{"from": "2026-10-31T22:00", "to": "2026-11-01T03:00", "minutes": 60})
	check(t, "the night the clocks fall back", night, "succeeded", "", `"free":[{"end":"2026-11-01T03:00:00-05:00","minutes":360,"start":"2026-10-31T22:00:00-04:00"}]`)

	for _, tc := range []struct {
		name string
		args map[string]any
		says string
	}{
		{"a window that ends before it starts", map[string]any{"from": "2026-10-02T17:00", "to": "2026-10-02T09:00", "minutes": 30}, "to is not after from — no free time was read"},
		{"a window of three weeks", map[string]any{"from": "2026-10-02T09:00", "to": "2026-10-23T09:00", "minutes": 30}, "the window is longer than 14 days — no free time was read"},
		{"no length", map[string]any{"from": "2026-10-02T09:00", "to": "2026-10-02T17:00"}, "minutes is a whole number from 1 to 1440 — no free time was read"},
		{"a time the clock skips", map[string]any{"from": "2026-03-08T02:30", "to": "2026-03-08T09:00", "minutes": 30}, "from 2026-03-08T02:30 does not happen in America/New_York: the clock skips it — no free time was read"},
	} {
		g.calls = nil
		check(t, tc.name, p.invoke(h, "calendar.availability", tc.args), "failed", argInvalid, tc.says)
		if g.count("http.get") > 1 {
			t.Errorf("%s: the events were read", tc.name)
		}
	}
}

// Past the bound, events not read start no earlier than the last one read,
// so free time is claimed only up to it, and the answer says from when to
// ask again.
func TestAvailabilityClaimsNothingItDidNotRead(t *testing.T) {
	g := newGoogle(t, newYork)
	g.pageCap = 1
	for i := 0; i < 6; i++ {
		s := time.Date(2026, 10, 2, 9+i, 0, 0, 0, zone(t, newYork))
		g.put(timed(fmt.Sprintf("ev%d", i), "Meeting", s.Format(time.RFC3339), s.Add(30*time.Minute).Format(time.RFC3339), newYork))
	}
	h := &standIn{t: t, handle: handle, google: g.handle}
	p := start(t)
	got := p.invoke(h, "calendar.availability", map[string]any{"from": "2026-10-02T09:00", "to": "2026-10-02T18:00", "minutes": 30})
	want := []any{
		map[string]any{"start": "2026-10-02T09:30:00-04:00", "end": "2026-10-02T10:00:00-04:00", "minutes": float64(30)},
		map[string]any{"start": "2026-10-02T10:30:00-04:00", "end": "2026-10-02T11:00:00-04:00", "minutes": float64(30)},
		map[string]any{"start": "2026-10-02T11:30:00-04:00", "end": "2026-10-02T12:00:00-04:00", "minutes": float64(30)},
	}
	if !reflect.DeepEqual(got.field("free"), want) || got.field("truncated") != true {
		t.Errorf("four pages of one: %s", got.said())
	}
	if m, _ := got.field("more").(string); m != "the calendar was read up to 2026-10-02T12:00:00-04:00, so no free time from then is claimed — ask again with from 2026-10-02T12:00:00" {
		t.Errorf("the route to the rest: %q", m)
	}

	// Google may answer a page with no events at all while more remain:
	// four such pages claim no free time.
	g.calls = nil
	for n := 2; n <= 5; n++ {
		g.at[n] = replies(gave(200, `{"items":[],"timeZone":"America/New_York","nextPageToken":"more"}`, -1))
	}
	empty := p.invoke(h, "calendar.availability", map[string]any{"from": "2026-10-02T09:00", "to": "2026-10-02T18:00", "minutes": 30})
	check(t, "four empty pages and more to come", empty, "succeeded", "", `"free":[]`)
	if m, _ := empty.field("more").(string); !strings.HasPrefix(m, "the calendar was read up to 2026-10-02T09:00:00-04:00") {
		t.Errorf("the route to the rest: %q", m)
	}
}

// ---- the declarations ----

// The writes wait for the operator and declare the external write; the
// reads do neither. Every operation reaches Google and nothing else — no
// storage. Every example the identity is shown is one the host admits.
func TestTheDeclarations(t *testing.T) {
	writes := map[string]bool{"calendar.create": true, "calendar.update": true, "calendar.delete": true}
	var names []string
	for _, d := range plugin.Descriptors() {
		names = append(names, d.ID)
		switch {
		case writes[d.ID] && (!d.OperatorConfirms || d.Effects != sdk.EffectsWriteExternal):
			t.Errorf("%s: operator_confirms %v, effects %s — a write the operator must confirm", d.ID, d.OperatorConfirms, d.Effects)
		case !writes[d.ID] && (d.OperatorConfirms || d.Effects != sdk.EffectsReadExternal):
			t.Errorf("%s: operator_confirms %v, effects %s — a read", d.ID, d.OperatorConfirms, d.Effects)
		case !reflect.DeepEqual(d.Capabilities, []string{host}):
			t.Errorf("%s asks for %v", d.ID, d.Capabilities)
		}
		for _, ex := range d.Examples {
			var v any
			if err := json.Unmarshal([]byte(ex), &v); err != nil {
				t.Errorf("%s: example %s: %v", d.ID, ex, err)
			} else if err := conforms(schemaFor(t, d.ID, true), v, d.ID); err != nil {
				t.Errorf("%s: example %s is outside the input schema: %v", d.ID, ex, err)
			}
		}
	}
	if want := []string{"calendar.availability", "calendar.create", "calendar.delete", "calendar.today", "calendar.upcoming", "calendar.update"}; !reflect.DeepEqual(names, want) {
		t.Errorf("the plugin declares %v, want %v", names, want)
	}
	raw, err := plugin.DescriptorsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), `"operator_confirms":true`); n != 3 {
		t.Errorf("the emitted descriptors carry operator_confirms %d times, want the three writes", n)
	}
}
