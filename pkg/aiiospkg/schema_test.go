package aiiospkg

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// .
// .
// .
// .
func TestSchemaSubsetVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "vectors", "schema_subset.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Accept []json.RawMessage `json:"accept"`
		Refuse []struct {
			Schema  json.RawMessage `json:"schema"`
			Keyword string          `json:"keyword"`
			Path    string          `json:"path"`
		} `json:"refuse"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Accept) == 0 || len(v.Refuse) == 0 {
		t.Fatal("the vectors must carry both accepted and refused schemas")
	}
	for i, s := range v.Accept {
		if err := CheckSchemaSubset(s); err != nil {
			t.Errorf("accept[%d] %s: unexpected refusal %v", i, s, err)
		}
	}
	for i, r := range v.Refuse {
		err := CheckSchemaSubset(r.Schema)
		var se *SchemaSubsetError
		if !errors.As(err, &se) || se.Keyword != r.Keyword || se.Path != r.Path {
			t.Errorf("refuse[%d] %s: want %s at %s, got %v", i, r.Schema, r.Keyword, r.Path, err)
		}
	}
}
