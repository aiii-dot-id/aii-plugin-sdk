package aiiosdk

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
func TestConfirmationSurvivesExecutableDescriptorSerialization(t *testing.T) {
	p := New("test")
	for _, name := range []string{"enroll", "remove", "reset", "list"} {
		p.Handle(name, func(Call) (any, error) { return nil, nil }).
			Describe(name, Descriptor{OperatorConfirms: name != "list"})
	}
	raw, err := p.DescriptorsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 {
		t.Fatalf("operation missing: %s", raw)
	}
	for _, row := range rows {
		if row["id"] == "list" {
			if _, present := row["operator_confirms"]; present {
				t.Fatalf("a read gained confirmation it never declared: %s", raw)
			}
			continue
		}
		if row["operator_confirms"] != true {
			t.Fatalf("%s lost operator confirmation in emitted JSON: %s", row["id"], raw)
		}
	}
}

// .
// .
// .
// .
func TestEveryDescriptorFieldReachesTheEmittedJSON(t *testing.T) {
	full := Descriptor{
		Summary:          "every field set",
		Input:            "schemas/in.json",
		Output:           "schemas/out.json",
		Effects:          EffectsReadInternal,
		Capabilities:     []string{"ring4.kv"},
		MaxResultBytes:   4096,
		Family:           "memory",
		Keywords:         []string{"recall"},
		Examples:         []string{`{"q":"x"}`},
		OperatorConfirms: true,
	}
	p := New("test")
	p.Handle("everything", func(Call) (any, error) { return nil, nil }).Describe("everything", full)
	raw, err := p.DescriptorsJSON()
	if err != nil {
		t.Fatal(err)
	}
	emitted := string(raw)
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("the emitted bytes must be JSON: %v\n%s", err, emitted)
	}
	if len(rows) != 1 {
		t.Fatalf("one descriptor: %s", emitted)
	}
	rt := reflect.TypeOf(full)
	for i := 0; i < rt.NumField(); i++ {
		tag := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		if _, present := rows[0][tag]; !present {
			t.Fatalf("Descriptor.%s is declared as %q and the host parses it, but DescriptorsJSON never emits it — add it to the splice: %s",
				rt.Field(i).Name, tag, emitted)
		}
	}
}
