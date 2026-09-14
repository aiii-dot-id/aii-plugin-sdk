package aiiospkg

import (
	"bytes"
	"testing"
)

func TestCanonicalizeV1Accepts(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"sorts_object_keys", `{"b":1,"a":2}`, `{"a":2,"b":1}`},
		{"nested_sort_and_minimal_separators", `{ "z" : [ 1 , {"b":true,"a":null} ] , "a" : "x" }`, `{"a":"x","z":[1,{"a":null,"b":true}]}`},
		{"unicode_escape_decodes_to_raw", "{\"k\":\"\\u003cscript\\u003e\"}", `{"k":"<script>"}`},
		{"astral_pair_decodes", "\"\\ud83d\\ude00\"", "\"\U0001F600\""},
		{"control_stays_escaped_lowercase", "\"\\u001F\"", "\"\\u001f\""},
		{"standard_escapes", "\"\\n\\t\\r\\b\\f\\\\\\\"\"", "\"\\n\\t\\r\\b\\f\\\\\\\"\""},
		{"solidus_unescapes", "\"\\/\"", `"/"`},
		{"numbers_pass_through", `[0,-0.5,123,1.25]`, `[0,-0.5,123,1.25]`},
		{"empty_object_and_array", `{"a":[],"b":{}}`, `{"a":[],"b":{}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalizeV1([]byte(tc.in))
			if err != nil {
				t.Fatalf("CanonicalizeV1(%q): %v", tc.in, err)
			}
			if string(got) != tc.want {
				t.Fatalf("CanonicalizeV1(%q) = %q, want %q", tc.in, got, tc.want)
			}
			// .
			again, err := CanonicalizeV1(got)
			if err != nil || !bytes.Equal(again, got) {
				t.Fatalf("not idempotent: %q -> %q (%v)", got, again, err)
			}
		})
	}
}

func TestCanonicalizeV1Rejects(t *testing.T) {
	cases := []struct{ name, in string }{
		{"duplicate_key_top", `{"k":1,"k":2}`},
		{"duplicate_key_nested", `{"x":{"k":1,"k":2}}`},
		{"nul_escape", "\"\\u0000\""},
		{"nul_raw_byte", "\"\x00\""},
		{"lone_high_surrogate", "\"\\ud83d\""},
		{"lone_low_surrogate", "\"\\udc00\""},
		{"inverted_surrogates", "\"\\udc00\\ud83d\""},
		{"bad_unicode_escape", "\"\\u00zz\""},
		{"exponent_number", `1e5`},
		{"plus_exponent", `1E+2`},
		{"leading_zero", `01`},
		{"trailing_fraction_zero", `1.50`},
		{"bare_fraction_zero", `1.0`},
		{"negative_zero", `-0`},
		{"trailing_token", `{} {}`},
		{"invalid_utf8", "\"\xff\""},
		{"raw_control_in_string", "\"\x01\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := CanonicalizeV1([]byte(tc.in)); err == nil {
				t.Fatalf("CanonicalizeV1(%q) accepted as %q; want reject", tc.in, got)
			}
		})
	}
}

func TestSHA256Prefixed(t *testing.T) {
	// .
	if got := SHA256Prefixed(nil); got != "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("SHA256Prefixed(nil) = %s", got)
	}
}

func TestMarshalCanonicalWashesHTMLEscapes(t *testing.T) {
	// .
	// .
	got, err := marshalCanonical(map[string]string{"k": "<a>&"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"k":"<a>&"}` {
		t.Fatalf("marshalCanonical = %q", got)
	}
}
