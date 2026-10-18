package aiiosdk

// The hand-rolled marshaller against two oracles: its own contract
// (domain bounds, determinism, refusals) and — for the shapes both
// support — byte agreement with encoding/json under the real Go
// toolchain, so host-side expectations and guest behavior cannot
// drift apart silently.

import (
	"encoding/json"
	"math"
	"testing"
)

func TestMarshalValueAgainstEncodingJSON(t *testing.T) {
	// Every case here must produce the exact bytes encoding/json
	// produces (it also sorts map keys), proving the two worlds agree
	// wherever they overlap.
	cases := []any{
		nil,
		true,
		false,
		"plain",
		"esc \" \\ \n \t control",
		"unicode é 𝄞",
		0, 1, -1, 42,
		int64(9007199254740991), int64(-9007199254740991),
		uint(7), uint64(9007199254740991),
		1.5, 0.25, -2.75, 3.0, 1e6, 1e-7, 123456789.5, 2.5e-10, 1e-5,
		map[string]any{"b": 1, "a": "x", "c": true, "d": nil},
		map[string]string{"z": "1", "a": "2"},
		map[string]int{"k": 3},
		[]any{1, "two", 3.5, nil, true},
		[]string{"a", "b"},
		[]int{1, 2, 3},
		[]int64{4, 5},
		[]float64{1.5, 2.5},
		[]bool{true, false},
		map[string]any{"nested": []any{map[string]any{"deep": []string{"x"}}}},
		json.RawMessage(`{"verbatim":true}`),
	}
	for _, v := range cases {
		got, err := marshalValue(v)
		if err != nil {
			t.Fatalf("%#v: %v", v, err)
		}
		want, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("oracle: %v", err)
		}
		if string(got) != string(want) {
			t.Fatalf("%#v:\n got %s\nwant %s", v, got, want)
		}
		// And everything emitted sits inside the strict domain.
		if verr := ValidateStrict(got); verr != nil {
			t.Fatalf("%#v: emitted bytes outside the domain: %s", v, got)
		}
	}
}

func TestMarshalValueRefusals(t *testing.T) {
	refuse := []any{
		int64(1) << 53,         // 2^53: over the integer domain
		-(int64(1) << 53),      // under
		uint64(1) << 53,        // unsigned over
		math.Inf(1),            // not finite
		math.NaN(),             // not finite
		1e300,                  // finite float, integer-valued, over 2^53−1
		1e20,                   // likewise — encoding/json would emit it; the domain forbids it
		make(chan int),         // no JSON meaning
		struct{ A int }{1},     // needs the reflection TinyGo cannot run — refused everywhere for one behavior
		[]byte("ambiguous"),    // encoding/json would base64 it; this kit refuses instead of guessing
		map[int]string{1: "x"}, // non-string keys
	}
	for _, v := range refuse {
		if _, err := marshalValue(v); err == nil {
			t.Fatalf("%#v must be refused", v)
		}
	}
	cyc := map[string]any{}
	cyc["self"] = cyc
	if _, err := marshalValue(cyc); err == nil {
		t.Fatalf("a cyclic map must error, never trap the guest on stack exhaustion")
	}
}

type customMarshaler struct{ v string }

func (c customMarshaler) MarshalJSON() ([]byte, error) {
	return []byte(`{"custom":"` + c.v + `"}`), nil
}

func TestMarshalValueMarshalerHook(t *testing.T) {
	// The MarshalJSON hook is a compiled type assertion — the escape
	// hatch for shapes the switch does not carry (structs included).
	got, err := marshalValue(customMarshaler{v: "yes"})
	if err != nil || string(got) != `{"custom":"yes"}` {
		t.Fatalf("got %s %v", got, err)
	}
	got, err = marshalValue(map[string]any{"m": customMarshaler{v: "nested"}})
	if err != nil || string(got) != `{"m":{"custom":"nested"}}` {
		t.Fatalf("nested hook: %s %v", got, err)
	}
}

func TestMarshalValueEmptyRawMessage(t *testing.T) {
	got, err := marshalValue(json.RawMessage(nil))
	if err != nil || string(got) != "null" {
		t.Fatalf("nil RawMessage must emit null: %s %v", got, err)
	}
}

func TestMarshalValueFloatForms(t *testing.T) {
	// Whatever spelling the float encoder picks must stay inside the
	// strict number grammar (including the e-07→e-7 cleanup form).
	for _, f := range []float64{1e6, 1e-7, 123456789.5, 0.1, 2.5e-10, 9007199254740991, 1e-21} {
		got, err := marshalValue(f)
		if err != nil {
			t.Fatalf("%v: %v", f, err)
		}
		if verr := ValidateStrict([]byte(`{"v":` + string(got) + `}`)); verr != nil {
			t.Fatalf("%v emitted %s — outside the domain", f, got)
		}
	}
}

// A LIST OF OBJECTS MARSHALS. The natural shape for "several records"
// — what a channel adapter's receive answers — was not in the switch, so
// the Telegram example failed every live poll.
func TestAListOfObjectsIsAResult(t *testing.T) {
	v := []map[string]any{{"b": 2, "a": "x"}, {}}
	got, err := marshalValue(v)
	if err != nil {
		t.Fatalf("[]map[string]any refused: %v", err)
	}
	want, _ := json.Marshal(v)
	if string(got) != string(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}
