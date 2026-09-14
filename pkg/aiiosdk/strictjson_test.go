package aiiosdk

// .
// .
// .

import (
	"strings"
	"testing"
)

func TestObjectMembers(t *testing.T) {
	payload := []byte(`{"jsonrpc":"2.0","id":42,"method":"invoke.call","params":{"operation":"a.b","arguments":{"k":[1,2]}}}`)
	if err := ValidateStrict(payload); err != nil {
		t.Fatalf("fixture must validate: %v", err)
	}
	members, ok := objectMembers(payload)
	if !ok || len(members) != 4 {
		t.Fatalf("members: %v %d", ok, len(members))
	}
	if string(memberByKey(members, "id")) != "42" {
		t.Fatalf("id raw span: %s", memberByKey(members, "id"))
	}
	if string(memberByKey(members, "jsonrpc")) != `"2.0"` {
		t.Fatalf("jsonrpc raw span: %s", memberByKey(members, "jsonrpc"))
	}
	params, ok := objectMembers(memberByKey(members, "params"))
	if !ok || string(memberByKey(params, "arguments")) != `{"k":[1,2]}` {
		t.Fatalf("nested member spans: %v", params)
	}
	if memberByKey(members, "absent") != nil {
		t.Fatalf("absent member must be nil")
	}

	if _, ok := objectMembers([]byte(`[1]`)); ok {
		t.Fatalf("array is not an object")
	}
	if m, ok := objectMembers([]byte(`{}`)); !ok || len(m) != 0 {
		t.Fatalf("empty object: %v %v", m, ok)
	}
	// .
	// .
	m, ok := objectMembers([]byte(`{"a":1}`))
	if !ok || string(memberByKey(m, "a")) != "1" {
		t.Fatalf("escaped key must decode: %v", m)
	}
	// .
	m, ok = objectMembers([]byte("{ \"a\" : 1 , \"b\" : [ 1 , 2 ] }"))
	if !ok || string(memberByKey(m, "b")) != "[ 1 , 2 ]" {
		t.Fatalf("whitespace handling: %v", m)
	}
}

func TestDecodeJSONString(t *testing.T) {
	for raw, want := range map[string]string{
		`"plain"`:   "plain",
		`"a\n\t\""`: "a\n\t\"",
		`"𝄞"`:       "\U0001D11E",
		`"café"`:    "café",
		`"sla\/sh"`: "sla/sh",
	} {
		got, ok := decodeJSONString([]byte(raw))
		if !ok || got != want {
			t.Fatalf("%s: got %q ok=%v", raw, got, ok)
		}
	}
	for _, bad := range []string{`plain`, `"unterminated`, `7`, ``, `"a" `} {
		if _, ok := decodeJSONString([]byte(bad)); ok {
			t.Fatalf("%q must not decode", bad)
		}
	}
	if _, ok := decodeJSONString(nil); ok {
		t.Fatalf("nil must not decode")
	}
}

func TestNumberDomainEdges(t *testing.T) {
	accept := []string{
		"0", "-0", "0.0", "1.5", "0.1", "1e-1", "10e-1", "1.5e2",
		"9007199254740991", "-9007199254740991", "90071992547409910e-1",
		"9.007199254740991e15",
		"1e-400",
		"2.5e-10",
	}
	for _, tok := range accept {
		if err := ValidateStrict([]byte(`{"v":` + tok + `}`)); err != nil {
			t.Fatalf("%s must be accepted: %v", tok, err)
		}
	}
	reject := []string{
		"9007199254740992", "-9007199254740992", "9007199254740992e0",
		"1e309", "1e308", "900719925474099200e-1",
		"01", "1.", ".5", "1e", "+1", "--1", "1e+",
	}
	for _, tok := range reject {
		if err := ValidateStrict([]byte(`{"v":` + tok + `}`)); err == nil {
			t.Fatalf("%s must be rejected", tok)
		}
	}
}

func TestValidateStrictStructure(t *testing.T) {
	accept := []string{
		`{"a":{"b":{"c":[true,false,null]}}}`,
		`"just a string"`,
		`42`,
		`[{"a":1},{"a":1}]`,
		"{\r\n\t \"a\" : 1 }",
	}
	for _, ok := range accept {
		if err := ValidateStrict([]byte(ok)); err != nil {
			t.Fatalf("%s must be accepted: %v", ok, err)
		}
	}
	reject := []string{
		`{"a":1,}`, `[1,]`, `{"a" 1}`, `{a:1}`, `{"a":}`, `[`, `}`,
		`{"a":1}{"b":2}`, `true false`, `"\q"`, "\"tab\tinside\"",
		`{"\ud834":1}`,
	}
	for _, bad := range reject {
		if err := ValidateStrict([]byte(bad)); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
	// .
	// .
	depth := 200
	deep := strings.Repeat(`{"a":`, depth) + "1" + strings.Repeat("}", depth)
	if err := ValidateStrict([]byte(deep)); err != nil {
		t.Fatalf("depth %d must validate: %v", depth, err)
	}
}
