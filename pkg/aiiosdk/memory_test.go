//go:build !wasm_unknown

package aiiosdk

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// .
// .
// .
// .

func TestRememberSendsTheTextAndReadsTheOutcome(t *testing.T) {
	var gotParams []byte
	withHost(t, func(params []byte) ([]byte, error) {
		gotParams = append([]byte(nil), params...)
		return []byte(`{"success":true,"ok":true,"status":"succeeded","operation_result":{"id":"pm_1","outcome":"updated","of":"pm_0","created_at":"2026-09-10T12:00:00Z","scope":"persistent"},"external_receipt":{"host_authored":true}}`), nil
	})
	got, err := Memory.Remember("the deploy moved to Thursday", Supersedes("pm_0"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "pm_1" || got.Outcome != "updated" || got.Of != "pm_0" || got.Scope != "persistent" || got.CreatedAt == "" {
		t.Fatalf("remembered = %+v", got)
	}
	p := string(gotParams)
	for _, want := range []string{`"operation":"memory.remember"`, `"text":"the deploy moved to Thursday"`, `"supersedes":"pm_0"`} {
		if !strings.Contains(p, want) {
			t.Fatalf("params lack %s:\n%s", want, p)
		}
	}
	if strings.Contains(p, `"target"`) {
		t.Fatalf("memory.remember takes no target:\n%s", p)
	}
}

func TestRecallSendsTheOptionsAndReadsTheHits(t *testing.T) {
	var gotParams []byte
	withHost(t, func(params []byte) ([]byte, error) {
		gotParams = append([]byte(nil), params...)
		return []byte(`{"success":true,"ok":true,"status":"succeeded","operation_result":{"hits":[{"id":"pm_1","text":"the harbour bell rings at noon","snippet":"the harbour bell rings at noon","match":"both","score":0.031,"strength":0.87,"attribution":"plugin","ring":4,"time":"2026-09-10T12:00:00Z","accesses":2,"class":"operational","similarity":0.71}],"status":"partial","matched":3,"shown":1,"policy":"carrd","truncated":true,"meaning":{"status":"found","detail":"","basis":"example/embedding-3"}},"external_receipt":{"host_authored":true}}`), nil
	})
	since := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	got, err := Memory.Recall("harbour bell", Exact(), Since(since), Limit(1), Decay(DecayDefault))
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "partial" || got.Matched != 3 || got.Shown != 1 || got.Policy != "carrd" || !got.Truncated || len(got.Hits) != 1 {
		t.Fatalf("result = %+v", got)
	}
	if got.Meaning != "found" || got.MeaningBasis != "example/embedding-3" {
		t.Fatalf("the meaning layer's outcome must be read: %+v", got)
	}
	h := got.Hits[0]
	if h.ID != "pm_1" || h.Match != "both" || h.Strength != 0.87 || h.Score != 0.031 || h.Ring != 4 || h.Accesses != 2 || h.Class != "operational" || h.Attribution != "plugin" || h.Similarity != 0.71 {
		t.Fatalf("hit = %+v", h)
	}
	p := string(gotParams)
	for _, want := range []string{`"operation":"memory.recall"`, `"query":"harbour bell"`, `"exact":true`, `"since":"2026-09-01T00:00:00Z"`, `"limit":1`, `"decay":"carrd"`} {
		if !strings.Contains(p, want) {
			t.Fatalf("params lack %s:\n%s", want, p)
		}
	}
}

func TestRecallByIDMissIsAnAnswerAndADenialIsTyped(t *testing.T) {
	withHost(t, func(params []byte) ([]byte, error) {
		return []byte(`{"success":false,"ok":false,"status":"failed","reason":"MEMORY_NOT_FOUND","reasonCode":"MEMORY_NOT_FOUND","reason_code":"MEMORY_NOT_FOUND","external_receipt":{"host_authored":true}}`), nil
	})
	got, err := Memory.Recall("", ByID("pm_gone"))
	if err != nil || got.Status != "found_nothing" || len(got.Hits) != 0 {
		t.Fatalf("a detail miss is an answer: %+v %v", got, err)
	}

	withHost(t, func(params []byte) ([]byte, error) {
		return []byte(`{"code":-32000,"message":"memory is not granted to plugin p; the operator grants it in plugins.grants.p.memory","data":{"reasonCode":"POLICY_DENY","denied_at":"capability_evaluation"}}`), nil
	})
	_, err = Memory.Remember("anything")
	var d *Denied
	if !errors.As(err, &d) || d.ReasonCode != "POLICY_DENY" {
		t.Fatalf("want *Denied POLICY_DENY, got %T %v", err, err)
	}

	withHost(t, func(params []byte) ([]byte, error) {
		return []byte(`{"success":false,"ok":false,"status":"failed","reason":"MEMORY_QUOTA_EXCEEDED","reasonCode":"MEMORY_QUOTA_EXCEEDED","reason_code":"MEMORY_QUOTA_EXCEEDED","external_receipt":{"host_authored":true}}`), nil
	})
	_, err = Memory.Remember("one more")
	var oe *OperationError
	if !errors.As(err, &oe) || oe.ReasonCode != "MEMORY_QUOTA_EXCEEDED" {
		t.Fatalf("want *OperationError MEMORY_QUOTA_EXCEEDED, got %T %v", err, err)
	}
}

func TestMemoryOutsideGuest(t *testing.T) {
	if _, err := Memory.Remember("x"); !errors.Is(err, ErrNotInGuest) {
		t.Fatalf("want ErrNotInGuest, got %v", err)
	}
	if _, err := Memory.Recall("x"); !errors.Is(err, ErrNotInGuest) {
		t.Fatalf("want ErrNotInGuest, got %v", err)
	}
}
