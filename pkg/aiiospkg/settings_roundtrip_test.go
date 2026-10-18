package aiiospkg

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Accepted settings must retain their meaning after packaging. Calling models
// directly did not prove that the deployed model picker could find it.
func TestSettingsPackagingPreservesEveryDeclaredField(t *testing.T) {
	raw := []byte(`[{"key":"model","type":"string","title":"Model","description":"Choose a model","default":"jev-latest","choices_from":"models"},{"key":"account","type":"secret","title":"Account","required":true,"oauth":{"provider":"google","services":["calendar"]}}]`)
	before, err := ParseSettings(raw)
	if err != nil {
		t.Fatal(err)
	}
	packed, err := SettingsJSON(before)
	if err != nil {
		t.Fatal(err)
	}
	after, err := ParseSettings(packed)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("settings changed in transit: before=%s after=%s", raw, packed)
	}
	var want, got any
	if err = json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(packed, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("packaged settings dropped metadata: %s", packed)
	}
}
