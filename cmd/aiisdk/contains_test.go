package main

import (
	"encoding/json"
	"testing"
)

// .
// .
func TestResultContainsTakesAStringOrAnArray(t *testing.T) {
	var tc testCase
	if err := json.Unmarshal([]byte(`{"name":"n","operation":"op","expect":{"result_contains":"\"id\":1"}}`), &tc); err != nil {
		t.Fatal(err)
	}
	if tc.Expect.ResultContains.missingFrom(`{"id":1}`) != "" || tc.Expect.ResultContains.missingFrom(`{"id":2}`) != `"id":1` {
		t.Fatalf("one substring: %v", tc.Expect.ResultContains)
	}
	if err := json.Unmarshal([]byte(`{"name":"n","operation":"op","expect":{"result_contains":["\"outcome\":\"created\"","\"skipped\":0"]}}`), &tc); err != nil {
		t.Fatal(err)
	}
	if got := tc.Expect.ResultContains.missingFrom(`{"skipped":0,"outcome":"created"}`); got != "" {
		t.Fatalf("every substring present, in any order: missing %q", got)
	}
	if got := tc.Expect.ResultContains.missingFrom(`{"outcome":"created"}`); got != `"skipped":0` {
		t.Fatalf("the first absent substring is named: %q", got)
	}
	for _, raw := range []string{`null`, `""`, `[]`} {
		if err := json.Unmarshal([]byte(`{"name":"n","operation":"op","expect":{"result_contains":`+raw+`}}`), &tc); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if tc.Expect.ResultContains.missingFrom("anything") != "" {
			t.Fatalf("%s expects nothing", raw)
		}
	}
	if err := json.Unmarshal([]byte(`{"name":"n","operation":"op","expect":{"result_contains":7}}`), &tc); err == nil {
		t.Fatal("a number is not an expectation")
	}
}
