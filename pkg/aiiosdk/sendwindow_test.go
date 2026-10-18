package aiiosdk

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// windowHost stands in for the host: settings, kv, and nothing else.
type windowHost struct {
	settings map[string]any
	kv       map[string]string // nil: kv is not granted
	puts     int
	failGet  bool
}

func (h *windowHost) serve(t *testing.T) {
	withHost(t, func(params []byte) ([]byte, error) {
		var p struct {
			Operation string          `json:"operation"`
			Target    json.RawMessage `json:"target"`
			Arguments json.RawMessage `json:"arguments"`
		}
		_ = json.Unmarshal(params, &p)
		var target struct{ Key string }
		_ = json.Unmarshal(p.Target, &target)
		ok := func(or any) ([]byte, error) {
			b, _ := json.Marshal(map[string]any{"success": true, "ok": true, "status": "succeeded", "operation_result": or, "external_receipt": map[string]any{}})
			return b, nil
		}
		switch p.Operation {
		case "settings.get":
			return ok(map[string]any{"values": h.settings})
		case "kv.get", "kv.put":
			if h.kv == nil {
				return []byte(denyAllReply), nil
			}
			if p.Operation == "kv.get" {
				if h.failGet {
					return []byte(`{"success":false,"ok":false,"status":"failed","reason":"KV_BROKEN","reasonCode":"KV_BROKEN","reason_code":"KV_BROKEN","external_receipt":{}}`), nil
				}
				v, found := h.kv[target.Key]
				if !found {
					return []byte(`{"success":false,"ok":false,"status":"failed","reason":"KV_NOT_FOUND","reasonCode":"KV_NOT_FOUND","reason_code":"KV_NOT_FOUND","external_receipt":{}}`), nil
				}
				return ok(map[string]any{"key": target.Key, "value": v})
			}
			var a struct{ Value string }
			_ = json.Unmarshal(p.Arguments, &a)
			h.kv[target.Key] = a.Value
			h.puts++
			return ok(map[string]any{"stored": true, "key": target.Key, "value_bytes": len(a.Value), "scope": "persistent"})
		}
		t.Fatalf("an unexpected host call %s", p.Operation)
		return nil, nil
	})
}

func at(ms int64) Call { return Call{Arguments: json.RawMessage(`{"_host_now_ms":` + itoa(ms) + `}`)} }

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC).UnixMilli()

// The window is a sliding one: the limit's worth of sends inside it stop the next,
// which says when the oldest leaves; they leave, and it may go.
func TestASendWindowStopsTheSendThatWouldExceedItAndReopens(t *testing.T) {
	h := &windowHost{settings: map[string]any{SendLimitSetting: 3, SendWindowSetting: 10}, kv: map[string]string{}}
	h.serve(t)
	w := SendWindow{Key: "x.sends"}
	send := func(ms int64) SendVerdict {
		t.Helper()
		v, err := w.Admit(at(ms))
		if err != nil {
			t.Fatalf("admit at %d: %v", ms, err)
		}
		if v.OK {
			if err := w.Record(at(ms), v); err != nil {
				t.Fatalf("record: %v", err)
			}
		}
		return v
	}
	min := int64(60_000)
	for i, ms := range []int64{t0, t0 + 1*min, t0 + 2*min} {
		if v := send(ms); !v.OK || v.Sent != i {
			t.Fatalf("send %d: %+v", i+1, v)
		}
	}
	v := send(t0 + 3*min)
	if v.OK || v.Sent != 3 || v.Limit != 3 || v.WindowMinutes != 10 || !v.RetryAt.Equal(time.UnixMilli(t0+10*min).UTC()) {
		t.Fatalf("the fourth: %+v, want stopped, the first leaving at t0+10m", v)
	}
	r := v.Refusal("nothing was sent")
	if r.ReasonCode != "NET_REMOTE_FAILED" || r.Status != "failed" ||
		!strings.Contains(r.Reason, "sent 3 in the last 10 minutes") || !strings.Contains(r.Reason, "send_limit and send_window_minutes settings") ||
		!strings.Contains(r.Reason, "can go at 2026-10-01T12:10:00Z") || !strings.HasSuffix(r.Reason, "nothing was sent") {
		t.Errorf("refusal: %+v", r)
	}
	if v := send(t0 + 10*min + 1); !v.OK || v.Sent != 2 {
		t.Errorf("when the first has left: %+v", v)
	}
	// A refused send is not recorded.
	before := h.puts
	if v := send(t0 + 10*min + 2); v.OK {
		t.Fatalf("the window is full again: %+v", v)
	}
	if h.puts != before {
		t.Errorf("a refused send was recorded")
	}
}

func TestTheDefaultsApplyWhenTheSettingsAreNotDeclared(t *testing.T) {
	h := &windowHost{settings: map[string]any{}, kv: map[string]string{}}
	h.serve(t)
	v, err := (SendWindow{Key: "k"}).Admit(at(t0))
	if err != nil || !v.OK || v.Limit != DefaultSendLimit || v.WindowMinutes != DefaultSendWindowMinutes {
		t.Fatalf("%+v %v", v, err)
	}
}

// A setting the declaration should have refused stops the send; it never lifts the limit.
func TestASettingOutOfBoundsStopsTheSend(t *testing.T) {
	for name, settings := range map[string]map[string]any{
		"zero limit":        {SendLimitSetting: 0},
		"a limit too large": {SendLimitSetting: MaxSendLimit + 1},
		"negative limit":    {SendLimitSetting: -5},
		"zero window":       {SendWindowSetting: 0},
		"a window too long": {SendWindowSetting: MaxSendWindowMinutes + 1},
	} {
		h := &windowHost{settings: settings, kv: map[string]string{}}
		h.serve(t)
		_, err := (SendWindow{Key: "k"}).Admit(at(t0))
		var oe *OperationError
		if !errors.As(err, &oe) || oe.ReasonCode != "NET_REMOTE_FAILED" || !strings.Contains(oe.Reason, "a whole number 1 to") {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Fail closed: no clock, no kv, a kv that faults — the send is stopped, and the
// words say which requirement.
func TestASendWindowThatCannotBeKeptStopsTheSend(t *testing.T) {
	h := &windowHost{settings: map[string]any{}, kv: map[string]string{}}
	h.serve(t)
	_, err := (SendWindow{Key: "k"}).Admit(Call{Arguments: json.RawMessage(`{}`)})
	var oe *OperationError
	if !errors.As(err, &oe) || !strings.Contains(oe.Reason, "no clock") {
		t.Errorf("no clock: %v", err)
	}
	h.kv = nil
	_, err = (SendWindow{Key: "k"}).Admit(at(t0))
	if !errors.As(err, &oe) || oe.Status != "denied" || !strings.Contains(oe.Reason, "grant kv to this plugin on the Plugins page") {
		t.Errorf("no kv: %v", err)
	}
	h.kv, h.failGet = map[string]string{}, true
	_, err = (SendWindow{Key: "k"}).Admit(at(t0))
	if !errors.As(err, &oe) || oe.ReasonCode != "NET_REMOTE_FAILED" || !strings.Contains(oe.Reason, "could not be read or written") {
		t.Errorf("a faulting kv: %v", err)
	}
}

// A damaged record starts the window over, bounded by the limit; times from the
// future beyond the skew, and from before the window, do not count.
func TestTheWindowCountsOnlyTimesItCouldHaveWritten(t *testing.T) {
	h := &windowHost{settings: map[string]any{SendLimitSetting: 2, SendWindowSetting: 10}, kv: map[string]string{"k": "not json"}}
	h.serve(t)
	w := SendWindow{Key: "k"}
	if v, err := w.Admit(at(t0)); err != nil || !v.OK || v.Sent != 0 {
		t.Errorf("a damaged record: %+v %v", v, err)
	}
	old, future, fine := t0-11*60_000, t0+5*60_000, t0-60_000
	raw, _ := json.Marshal([]int64{old, future, fine})
	h.kv["k"] = string(raw)
	if v, _ := w.Admit(at(t0)); !v.OK || v.Sent != 1 {
		t.Errorf("one time in the window: %+v", v)
	}
}

// Only a send that went, or may have, takes a place.
func TestOnlyASendThatWentOrMayHaveGoneCounts(t *testing.T) {
	w := SendWindow{}
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"sent":           {nil, true},
		"effect unknown": {Fail("NET_EFFECT_UNKNOWN", "x"), true},
		"refused":        {Fail("NET_REMOTE_FAILED", "x"), false},
		"bad arguments":  {Fail("OPERATION_ARGUMENT_INVALID", "x"), false},
		"denied":         {Deny("POLICY_DENY", "x"), false},
		"a plain error":  {errors.New("x"), false},
	} {
		if got := w.Counts(tc.err); got != tc.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}

// The record is bounded: never more than MaxSendLimit times.
func TestTheRecordIsBounded(t *testing.T) {
	h := &windowHost{settings: map[string]any{SendLimitSetting: MaxSendLimit, SendWindowSetting: MaxSendWindowMinutes}, kv: map[string]string{}}
	h.serve(t)
	w := SendWindow{Key: "k"}
	var times []int64
	for i := 0; i < MaxSendLimit; i++ {
		times = append(times, t0-int64(MaxSendLimit-i))
	}
	raw, _ := json.Marshal(times)
	h.kv["k"] = string(raw)
	v, err := w.Admit(at(t0))
	if err != nil || v.OK {
		t.Fatalf("a full window: %+v %v", v, err)
	}
	// Recording past the limit (a lowered limit, a raised one) keeps the newest MaxSendLimit.
	if err := w.Record(at(t0), v); err != nil {
		t.Fatal(err)
	}
	var kept []int64
	_ = json.Unmarshal([]byte(h.kv["k"]), &kept)
	if len(kept) != MaxSendLimit || kept[len(kept)-1] != t0 || kept[0] != times[1] {
		t.Errorf("kept %d, first %d last %d", len(kept), kept[0], kept[len(kept)-1])
	}
}

// A lowered limit: with three sends in the window and a limit of one, the next
// may go only when the two oldest have left — when the newest is the one
// holding the last place — not when the first has.
func TestALoweredLimitWaitsUntilThereIsARoom(t *testing.T) {
	h := &windowHost{settings: map[string]any{SendLimitSetting: 1, SendWindowSetting: 10}, kv: map[string]string{}}
	h.serve(t)
	min := int64(60_000)
	raw, _ := json.Marshal([]int64{t0, t0 + min, t0 + 2*min})
	h.kv["k"] = string(raw)
	v, err := (SendWindow{Key: "k"}).Admit(at(t0 + 3*min))
	if err != nil || v.OK || v.Sent != 3 || !v.RetryAt.Equal(time.UnixMilli(t0+12*min).UTC()) {
		t.Fatalf("%+v %v, want stopped, room at t0+12m", v, err)
	}
}
