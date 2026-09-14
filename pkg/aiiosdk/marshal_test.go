package aiiosdk

// .
// .
// .
// .
// .

import (
	"encoding/json"
	"math"
	"testing"
)

func TestMarshalValueAgainstEncodingJSON(t *testing.T) {
	// .
	// .
	// .
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
		// .
		if verr := ValidateStrict(got); verr != nil {
			t.Fatalf("%#v: emitted bytes outside the domain: %s", v, got)
		}
	}
}

func TestMarshalValueRefusals(t *testing.T) {
	refuse := []any{
		int64(1) << 53,
		-(int64(1) << 53),
		uint64(1) << 53,
		math.Inf(1),
		math.NaN(),
		1e300,
		1e20,
		make(chan int),
		struct{ A int }{1},
		[]byte("ambiguous"),
		map[int]string{1: "x"},
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
	// .
	// .
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
	// .
	// .
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
