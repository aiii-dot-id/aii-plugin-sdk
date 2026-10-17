package main

import (
	"encoding/json"
	"testing"
)

func TestCasesCarryTheHostClockAsTheHostInjectsIt(t *testing.T) {
	out := withHostClock(json.RawMessage(`{"content":"x"}`))
	var m map[string]json.RawMessage
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["_host_now_ms"]; !ok {
		t.Fatalf("no clock: %s", out)
	}
	if string(m["content"]) != `"x"` {
		t.Fatalf("an argument was lost: %s", out)
	}
	if kept := withHostClock(json.RawMessage(`{"_host_now_ms":7}`)); string(kept) != `{"_host_now_ms":7}` {
		t.Fatalf("a case's own clock was replaced: %s", kept)
	}
	if empty := withHostClock(nil); !json.Valid(empty) {
		t.Fatalf("empty arguments did not become an object: %s", empty)
	}
}
