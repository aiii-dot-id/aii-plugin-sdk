package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type memoryHarnessReply struct {
	OK      bool                   `json:"ok"`
	Reason  string                 `json:"reason"`
	Result  map[string]interface{} `json:"operation_result"`
	Receipt struct {
		HostAuthored bool `json:"host_authored"`
	} `json:"external_receipt"`
}

func memoryHarnessCall(t *testing.T, h *harness, op, args string) memoryHarnessReply {
	t.Helper()
	raw, rpcErr := h.answerMemory(op, json.RawMessage(args))
	if rpcErr != nil {
		t.Fatalf("unexpected RPC error: %s", rpcErr)
	}
	var r memoryHarnessReply
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if r.Receipt.HostAuthored {
		t.Fatal("a harness response claimed host-authored authority")
	}
	return r
}

func TestHarnessMemoryRefusesUnestablishedContextAndEvidence(t *testing.T) {
	for _, args := range []string{
		`{"text":"note","applicability":"task"}`,
		`{"text":"note","applicability":"project"}`,
		`{"text":"note","applicability":"reusable"}`,
		`{"text":"note","applicability":"task","_host_work_session":{"id":"caller-task"}}`,
		`{"text":"note","evidence":[{"id":"r1","sequence":"1","sha256":"` + strings.Repeat("a", 64) + `","incarnation":"caller"}]}`,
		`{"text":"note","evidence":[{"availability":"available"}]}`,
	} {
		t.Run(args, func(t *testing.T) {
			h := &harness{memGranted: true}
			r := memoryHarnessCall(t, h, "memory.remember", args)
			if r.OK || r.Reason != "OPERATION_ARGUMENT_INVALID" || len(h.memories) != 0 || h.memoryN != 0 {
				t.Fatalf("unestablished request must refuse without mutation: %+v, %+v", r, h.memories)
			}
			if !strings.Contains(args, "_host_work_session") && r.Result["detail"] == nil {
				t.Fatal("refusal did not disclose the harness limitation")
			}
		})
	}
}

func TestHarnessMemoryLegacyFullTextAndContext(t *testing.T) {
	h := &harness{memGranted: true}
	text := strings.Repeat("retained detail ", 80)
	args, _ := json.Marshal(map[string]interface{}{"text": text, "applicability": "legacy", "evidence": []interface{}{}})
	r := memoryHarnessCall(t, h, "memory.remember", string(args))
	if !r.OK || r.Result["scope"] != "harness" {
		t.Fatalf("legacy write: %+v", r)
	}
	assertHarnessMemoryContext(t, r.Result)
	id := r.Result["id"].(string)
	r = memoryHarnessCall(t, h, "memory.recall", `{"id":"`+id+`","selection":"legacy"}`)
	if !r.OK {
		t.Fatalf("legacy detail: %+v", r)
	}
	hit := r.Result["hits"].([]interface{})[0].(map[string]interface{})
	if hit["text"] != strings.TrimSpace(text) || hit["snippet"] == hit["text"] || hit["accesses"] != float64(1) {
		t.Fatalf("detail must retain full text and reinforce once: %+v", hit)
	}
	assertHarnessMemoryContext(t, hit)
	for _, selection := range []string{"relevant", "reusable"} {
		r = memoryHarnessCall(t, h, "memory.recall", `{"query":"retained","selection":"`+selection+`"}`)
		if !r.OK || r.Result["matched"] != float64(0) || len(r.Result["hits"].([]interface{})) != 0 {
			t.Fatalf("%s must not relabel legacy notes: %+v", selection, r)
		}
		r = memoryHarnessCall(t, h, "memory.recall", `{"id":"`+id+`","selection":"`+selection+`"}`)
		if r.OK || r.Reason != "MEMORY_NOT_FOUND" {
			t.Fatalf("%s by-ID must honor selection: %+v", selection, r)
		}
	}
	if h.memories[0].accesses != 1 {
		t.Fatal("out-of-selection lookups reinforced a note")
	}
}

func assertHarnessMemoryContext(t *testing.T, value map[string]interface{}) {
	t.Helper()
	c, ok := value["context"].(map[string]interface{})
	if !ok || len(c) != 1 || c["applicability"] != "legacy" {
		t.Fatalf("harness must identify unscoped legacy context, without fabricated IDs: %+v", value)
	}
	e, ok := value["evidence"].([]interface{})
	if !ok || len(e) != 0 {
		t.Fatalf("harness must not fabricate validated evidence: %+v", value)
	}
}

func TestHarnessMemoryRecentIsCurrentOrderedAndNonreinforcing(t *testing.T) {
	h := &harness{memGranted: true}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := h.newMemory("old superseded", base)
	first := h.newMemory("first current", base.Add(time.Second))
	last := h.newMemory("last current", base.Add(2*time.Second))
	old.supersededBy = last.id
	for _, m := range h.memories {
		m.accesses = 7
	}
	r := memoryHarnessCall(t, h, "memory.recall", `{"recent":true,"selection":"legacy","limit":1}`)
	if !r.OK || r.Result["matched"] != float64(2) || r.Result["shown"] != float64(1) || r.Result["truncated"] != true || r.Result["status"] != "partial" || r.Result["policy"] != "none" {
		t.Fatalf("recent coverage: %+v", r)
	}
	hit := r.Result["hits"].([]interface{})[0].(map[string]interface{})
	if hit["id"] != last.id || hit["match"] != "recent" {
		t.Fatalf("recent must return the newest current row: %+v", hit)
	}
	for _, key := range []string{"score", "strength", "fused"} {
		if _, ok := hit[key]; ok {
			t.Fatalf("recent inspection invented %s", key)
		}
	}
	r = memoryHarnessCall(t, h, "memory.recall", `{"recent":true,"before":"2026-01-01T00:00:02Z"}`)
	if !r.OK || r.Result["matched"] != float64(1) || r.Result["hits"].([]interface{})[0].(map[string]interface{})["id"] != first.id {
		t.Fatalf("recent time window: %+v", r)
	}
	for _, m := range h.memories {
		if m.accesses != 7 {
			t.Fatal("recent lookup reinforced a note")
		}
	}
}

func TestHarnessMemoryRejectsInvalidInputsBeforeMutation(t *testing.T) {
	for _, tc := range []struct{ op, args string }{
		{"remember", `null`}, {"remember", `[]`}, {"remember", `{"text":3}`},
		{"remember", `{"text":"ok","applicability":"unknown"}`},
		{"remember", `{"text":"ok","evidence":"claimed"}`},
		{"remember", `{"text":"ok","project_id":"caller"}`},
		{"recall", `null`}, {"recall", `{"query":"one","recent":true}`},
		{"recall", `{"id":"hm_1","query":"one"}`}, {"recall", `{"id":"hm_1","recent":true}`},
		{"recall", `{"id":"hm_1","limit":1}`}, {"recall", `{"id":"hm_1","exact":true}`},
		{"recall", `{"id":"hm_1","since":"bad"}`}, {"recall", `{"id":"hm_1","decay":"none"}`},
		{"recall", `{"query":"one","limit":-1}`}, {"recall", `{"query":"one","limit":51}`},
		{"recall", `{"query":"one","selection":"unknown"}`},
		{"recall", `{"query":"one","selection":"task"}`}, {"recall", `{"query":"one","selection":"project"}`},
		{"recall", `{"query":"one","since":"bad"}`}, {"recall", `{"query":"one","before":"bad"}`},
		{"recall", `{"query":"one","decay":"invented"}`}, {"recall", `{"recent":"true"}`},
		{"recall", `{"recent":true,"exact":true}`},
		{"recall", `{"query":"one","project_id":"caller"}`},
	} {
		t.Run(tc.op+tc.args, func(t *testing.T) {
			h := &harness{memGranted: true}
			m := h.newMemory("one retained note", time.Now())
			r := memoryHarnessCall(t, h, "memory."+tc.op, tc.args)
			if r.OK || r.Reason != "OPERATION_ARGUMENT_INVALID" || len(h.memories) != 1 || m.accesses != 0 || m.supersededBy != "" {
				t.Fatalf("invalid input must refuse before mutation: %+v, %+v", r, m)
			}
		})
	}
}

func TestHarnessMemoryCorrectionAndLegacyCompatibility(t *testing.T) {
	h := &harness{memGranted: true}
	r := memoryHarnessCall(t, h, "memory.remember", `{"text":"first version"}`)
	if !r.OK {
		t.Fatalf("old omitted applicability: %+v", r)
	}
	first := r.Result["id"].(string)
	r = memoryHarnessCall(t, h, "memory.remember", `{"text":"corrected version","supersedes":"`+first+`"}`)
	if !r.OK || r.Result["outcome"] != "updated" || r.Result["of"] != first {
		t.Fatalf("correct: %+v", r)
	}
	second := r.Result["id"].(string)
	r = memoryHarnessCall(t, h, "memory.remember", `{"text":"stale correction","supersedes":"`+first+`"}`)
	if r.OK || r.Reason != "MEMORY_NOT_FOUND" || len(h.memories) != 2 {
		t.Fatalf("stale CAS: %+v", r)
	}
	r = memoryHarnessCall(t, h, "memory.recall", `{"id":"`+first+`"}`)
	if !r.OK || r.Result["hits"].([]interface{})[0].(map[string]interface{})["superseded_by"] != second {
		t.Fatalf("historical detail: %+v", r)
	}
	r = memoryHarnessCall(t, h, "memory.recall", `{"query":"version"}`)
	if !r.OK || r.Result["matched"] != float64(1) || r.Result["hits"].([]interface{})[0].(map[string]interface{})["id"] != second {
		t.Fatalf("legacy query: %+v", r)
	}
	if r.Result["meaning"].(map[string]interface{})["status"] != "source_unavailable" {
		t.Fatal("harness claimed a real embeddings source")
	}
	h.memGranted = false
	_, denied := h.answerMemory("memory.remember", json.RawMessage(`{"text":"not allowed"}`))
	if denied == nil || len(h.memories) != 2 {
		t.Fatal("missing memory grant was not enforced")
	}
}
