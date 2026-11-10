package aiiosdk

import (
	"testing"
	"time"
)

// resetLastHostTime starts a test with no receipt seen, and puts back what was there.
func resetLastHostTime(t *testing.T) {
	t.Helper()
	saved := lastHostTime.Load()
	lastHostTime.Store(0)
	t.Cleanup(func() { lastHostTime.Store(saved) })
}

// The host's time on a receipt is the guest's only clock after the invocation starts: it is read from
// every reply that carries one, a performed failure included, and through the typed wrappers that drop
// the receipt itself; a reply without one, a time that does not parse, and a refusal leave it as it was.
func TestTheHostsReceiptTimeIsTheGuestsClock(t *testing.T) {
	resetLastHostTime(t)
	if _, ok := LastHostTime(); ok {
		t.Fatal("a host time before any receipt")
	}
	reply := func(r string) { withHost(t, func([]byte) ([]byte, error) { return []byte(r), nil }) }

	first := time.Date(2026, 11, 10, 2, 30, 0, 123456789, time.UTC)
	reply(`{"status":"succeeded","operation_result":{},"external_receipt":{"success":true,"timestamp":"2026-11-10T02:30:00.123456789Z"}}`)
	res, err := InvokeCall("kv.get", map[string]any{"key": "x"}, nil)
	if err != nil || !res.ReceiptTime.Equal(first) {
		t.Fatalf("receipt time: %v %v", res, err)
	}
	if at, ok := LastHostTime(); !ok || !at.Equal(first) {
		t.Fatalf("last host time: %v %v", at, ok)
	}

	for _, r := range []string{
		`{"status":"succeeded"}`,
		`{"status":"succeeded","external_receipt":{"timestamp":"yesterday"}}`,
		`{"status":"succeeded","external_receipt":{"timestamp":""}}`,
		`{"code":-32000,"message":"no grant","data":{"reasonCode":"POLICY_DENY"}}`,
	} {
		reply(r)
		res, _ := InvokeCall("kv.get", map[string]any{"key": "x"}, nil)
		if res != nil && !res.ReceiptTime.IsZero() {
			t.Fatalf("%s: a receipt time from nothing: %v", r, res.ReceiptTime)
		}
		if at, ok := LastHostTime(); !ok || !at.Equal(first) {
			t.Fatalf("%s moved the clock: %v %v", r, at, ok)
		}
	}

	// A refused HTTP status is a performed exchange: its receipt carries the host's time.
	later := time.Date(2026, 11, 10, 2, 31, 0, 0, time.UTC)
	reply(`{"status":"failed","reason":"NET_REMOTE_OUTCOME_FAILED","operation_result":{"http_status":429},"external_receipt":{"success":false,"effect":"performed","timestamp":"2026-11-10T02:31:00Z"}}`)
	res, err = InvokeCall("http.post", map[string]any{"url": "https://judge.test/"}, nil)
	if err == nil || res == nil || !res.ReceiptTime.Equal(later) {
		t.Fatalf("failed call: %v %v", res, err)
	}
	if at, _ := LastHostTime(); !at.Equal(later) {
		t.Fatalf("a performed failure did not move the clock: %v", at)
	}

	// The typed wrapper keeps its own result shape and drops the receipt; the clock still moves.
	latest := time.Date(2026, 11, 10, 2, 32, 5, 0, time.UTC)
	reply(`{"status":"succeeded","operation_result":{"http_status":200,"body":{}},"external_receipt":{"effect":"performed","timestamp":"2026-11-10T02:32:05Z"}}`)
	if _, err := HTTP.Post("https://judge.test/", &HTTPOptions{Body: "{}", ContentType: "application/json"}); err != nil {
		t.Fatal(err)
	}
	if at, _ := LastHostTime(); !at.Equal(latest) {
		t.Fatalf("through HTTP.Post: %v", at)
	}
}
