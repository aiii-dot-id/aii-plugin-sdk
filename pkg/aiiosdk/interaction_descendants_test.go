package aiiosdk

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestInteractionDescendantsAreAnOptionalWireField(t *testing.T) {
	q := InteractionQuery{Version: 1, Filter: InteractionFilter{WorkSession: "root"}}
	old, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(old, []byte("include_descendants")) {
		t.Fatal("false changed legacy query wire")
	}
	q.Filter.IncludeDescendants = true
	raw, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"include_descendants":true`)) {
		t.Fatalf("missing opt-in: %s", raw)
	}
	var got InteractionQuery
	if err := decodeInteraction(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !got.Filter.IncludeDescendants || got.Filter.WorkSession != "root" {
		t.Fatalf("lost scope: %+v", got)
	}
	var legacy InteractionQuery
	if err := decodeInteraction(old, &legacy); err != nil || legacy.Filter.IncludeDescendants {
		t.Fatalf("legacy failed: %+v %v", legacy, err)
	}
	bad := bytes.Replace(raw, []byte(`"include_descendants":true`), []byte(`"include_descendants":"true"`), 1)
	if err := decodeInteraction(bad, &got); err == nil {
		t.Fatal("string boolean accepted")
	}
}
