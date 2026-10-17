package aiiosdk

import "testing"

func TestMemoryMetadataAndWorkContextRemainDistinct(t *testing.T) {
	c, refs, err := memoryMetadata(Object(`{"context":{"applicability":"project","project_id":"p","work_session_id":"w","source_incarnation":"s"},"evidence":[{"id":"r","sequence":"9007199254740993","sha256":"h","incarnation":"s","availability":"unavailable"}]}`))
	if err != nil || c.Applicability != "project" || c.ProjectID != "p" || c.WorkSessionID != "w" || c.SourceIncarnation != "s" || len(refs) != 1 || refs[0].Sequence != "9007199254740993" || refs[0].Availability != "unavailable" {
		t.Fatalf("metadata lost: %+v %+v %v", c, refs, err)
	}
	w := (Call{Arguments: []byte(`{"work_session_id":"forged"}`), HostWorkSession: []byte(`{"id":"actual","reason":"","call_id":"tool_call_tex_1"}`)}).WorkSession()
	if w.ID != "actual" || w.CallID != "tool_call_tex_1" {
		t.Fatal(w)
	}
	if (Call{Arguments: []byte(`{"work_session_id":"forged"}`)}).WorkSession().ID != "" {
		t.Fatal("caller task substituted for host context")
	}
	for _, raw := range []string{`{"context":[]}`, `{"evidence":{}}`, `{"evidence":[true]}`} {
		if _, _, err := memoryMetadata(Object(raw)); err == nil {
			t.Fatalf("malformed metadata accepted: %s", raw)
		}
	}
	if c, r, err := memoryMetadata(Object(`{}`)); err != nil || c.Applicability != "" || len(r) != 0 {
		t.Fatal("old host compatibility lost")
	}
}

func TestMemoryOptionsDoNotSupplyTaskAuthority(t *testing.T) {
	a := map[string]any{}
	MemoryApplicability("task")(a)
	MemoryEvidence(MemoryEvidenceRef{ID: "r", Sequence: "1"})(a)
	if len(a) != 2 || a["applicability"] != "task" {
		t.Fatal(a)
	}
	b := map[string]any{}
	MemorySelection("relevant")(b)
	RecentMemories()(b)
	if len(b) != 2 || b["selection"] != "relevant" || b["recent"] != true {
		t.Fatal(b)
	}
}

func TestResultSizeUsesActualGuestEncoding(t *testing.T) {
	value := map[string]any{"text": "\x00\n\"\\🙂", "refs": []any{map[string]any{"sequence": "9007199254740993"}}}
	raw, err := marshalValue(value)
	if err != nil {
		t.Fatal(err)
	}
	n, err := ResultSize(value)
	if err != nil || n != len(raw) {
		t.Fatal("size disagrees with result encoder", n, len(raw), err)
	}
	if _, err := ResultSize(uint64(1 << 63)); err == nil {
		t.Fatal("size accepted unsupported wire integer")
	}
}

func TestMemoryEvidenceUsesGuestEncoder(t *testing.T) {
	for _, refs := range [][]MemoryEvidenceRef{
		{},
		{{ID: "record-\"one", Sequence: "9007199254740993", SHA256: "hash", Incarnation: "source"}},
		{{ID: "record-two", Sequence: "2", SHA256: "hash", Incarnation: "source", Availability: "unavailable"}},
	} {
		args := map[string]any{}
		MemoryEvidence(refs...)(args)
		raw, err := marshalValue(args)
		if err != nil {
			t.Fatalf("guest cannot send memory evidence: %v", err)
		}
		_, got, err := memoryMetadata(Object(raw))
		if err != nil || len(got) != len(refs) {
			t.Fatalf("evidence array changed: %s: %v", raw, err)
		}
		for i := range refs {
			if got[i] != refs[i] {
				t.Fatalf("reference changed: got %+v want %+v", got[i], refs[i])
			}
		}
		// A supplied availability must reach the host for refusal; omitting
		// an empty one keeps the intake reference's normal wire shape.
		items, ok := arrayElements(Object(raw).Raw("evidence"))
		if !ok {
			t.Fatalf("empty evidence must still be an array: %s", raw)
		}
		for i, item := range items {
			if Object(item).Has("availability") != (refs[i].Availability != "") {
				t.Fatalf("availability changed its presence: %s", item)
			}
		}
	}
}
