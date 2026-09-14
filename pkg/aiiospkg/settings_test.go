package aiiospkg

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

// .
// .
func TestSettingsDeclarationVectors(t *testing.T) {
	raw, err := os.ReadFile("../../vectors/settings_decl.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []struct {
			Name          string          `json:"name"`
			Decl          json.RawMessage `json:"decl"`
			OK            bool            `json:"ok"`
			ErrorContains string          `json:"error_contains"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) < 15 {
		t.Fatalf("the vectors are thin: %d cases", len(file.Cases))
	}
	for _, c := range file.Cases {
		decls, err := ParseSettings(c.Decl)
		switch {
		case c.OK && err != nil:
			t.Errorf("%s: refused: %v", c.Name, err)
		case !c.OK && err == nil:
			t.Errorf("%s: accepted, want a refusal containing %q", c.Name, c.ErrorContains)
		case !c.OK && !strings.Contains(err.Error(), c.ErrorContains):
			t.Errorf("%s: refusal %q does not name %q", c.Name, err, c.ErrorContains)
		}
		if c.OK {
			// .
			packaged, err := SettingsJSON(decls)
			if err != nil {
				t.Fatalf("%s: emit: %v", c.Name, err)
			}
			again, err := ParseSettings(packaged)
			if err != nil {
				t.Fatalf("%s: the packaged member does not parse under the host's rules: %v\n%s", c.Name, err, packaged)
			}
			if len(again) != len(decls) {
				t.Fatalf("%s: round trip lost entries", c.Name)
			}
		}
	}
}

func TestSettingsJSONIsCanonicalAndMinimal(t *testing.T) {
	min, max := 1.0, 50.0
	out, err := SettingsJSON([]SettingDecl{{Key: "recall_limit", Type: "number", Title: "Recall limit", Default: 5.0, Minimum: &min, Maximum: &max}, {Key: "api_key", Type: "secret", Title: "API key", Required: true}})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"default":5,"key":"recall_limit","maximum":50,"minimum":1,"title":"Recall limit","type":"number"},{"key":"api_key","required":true,"title":"API key","type":"secret"}]`
	if string(out) != want {
		t.Fatalf("canonical member:\n%s\nwant\n%s", out, want)
	}
	eff := EffectiveSettings([]SettingDecl{{Key: "recall_limit", Type: "number", Title: "R", Default: 5.0, Maximum: &max}}, map[string]interface{}{"recall_limit": 99.0})
	if eff["recall_limit"] != 5.0 {
		t.Fatalf("a value outside its bounds is dropped for the default: %v", eff)
	}
}

// .
// .
// .
func TestLabelledChoicesAndIntegersRoundTrip(t *testing.T) {
	one, top := 1.0, 200.0
	out, err := SettingsJSON([]SettingDecl{
		{Key: "voice", Type: "enum", Title: "Speaking voice", Values: []string{"alba", "ryan"}, Labels: map[string]string{"ryan": "Ryan (British English)", "alba": "Alba (Scottish English)"}, Default: "alba"},
		{Key: "top_k", Type: "integer", Title: "Top-k", Default: 40.0, Minimum: &one, Maximum: &top}})
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"default":"alba","key":"voice","labels":{"alba":"Alba (Scottish English)","ryan":"Ryan (British English)"},"title":"Speaking voice","type":"enum","values":["alba","ryan"]},{"default":40,"key":"top_k","maximum":200,"minimum":1,"title":"Top-k","type":"integer"}]`
	if string(out) != want {
		t.Fatalf("canonical member:\n%s\nwant\n%s", out, want)
	}
	decls, err := ParseSettings(out)
	if err != nil || len(decls) != 2 || decls[0].Labels["ryan"] != "Ryan (British English)" {
		t.Fatalf("read back: %v %+v", err, decls)
	}
	if err := CheckSettingValue(decls[0], "Alba (Scottish English)"); err == nil {
		t.Fatal("a label is shown, never stored")
	}
	if err := CheckSettingValue(decls[1], 2.5); err == nil || !strings.Contains(err.Error(), "whole number") {
		t.Fatalf("a fraction for an integer: %v", err)
	}
	if err := CheckSettingValue(decls[1], json.Number("7")); err != nil {
		t.Fatalf("a whole number: %v", err)
	}
	eff := EffectiveSettings(decls, map[string]interface{}{"top_k": 2.5, "voice": "ryan"})
	if eff["top_k"] != 40.0 || eff["voice"] != "ryan" {
		t.Fatalf("a fraction is dropped for the default, a holding choice stands: %v", eff)
	}
	values := make([]string, MaxSettingEnumValues)
	for i := range values {
		values[i] = fmt.Sprintf("locale-%03d", i)
	}
	if _, err := SettingsJSON([]SettingDecl{{Key: "locale", Type: "enum", Title: "Locale", Values: values}}); err != nil {
		t.Fatalf("%d choices are one setting: %v", MaxSettingEnumValues, err)
	}
	if _, err := SettingsJSON([]SettingDecl{{Key: "locale", Type: "enum", Title: "Locale", Values: append(values, "one-more")}}); err == nil || !strings.Contains(err.Error(), "1..256 values") {
		t.Fatalf("the 257th choice is refused by the bound: %v", err)
	}
}
