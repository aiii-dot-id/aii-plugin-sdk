package aiiosdk

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestPrimaryHintIsOptionalAndBoundByTheDescriptorHash(t *testing.T) {
	makePlugin := func(primary bool) *Plugin {
		return New("notes").Handle("notes.search", func(Call) (any, error) { return nil, nil }).
			Describe("notes.search", Descriptor{Summary: "Find applicable notes", Input: "schemas/search.json",
				Output: "schemas/results.json", Effects: EffectsReadInternal, Family: "notes", Primary: primary})
	}
	ordinary, err := makePlugin(false).DescriptorsJSON()
	if err != nil {
		t.Fatal(err)
	}
	primary := makePlugin(true)
	raw, err := primary.DescriptorsJSON()
	if err != nil {
		t.Fatal(err)
	}
	var before, after []map[string]any
	if err := json.Unmarshal(ordinary, &before); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &after); err != nil {
		t.Fatal(err)
	}
	if _, present := before[0]["primary"]; present || after[0]["primary"] != true {
		t.Fatalf("optional primary hint: before=%v after=%v", before, after)
	}
	manifest, err := primary.manifestInterfaces("notes", 1)
	if err != nil {
		t.Fatal(err)
	}
	var declaration struct {
		Core []struct {
			Hash string `json:"schema_hash"`
		} `json:"core"`
	}
	if err := json.Unmarshal(manifest, &declaration); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if len(declaration.Core) != 1 || declaration.Core[0].Hash != "sha256:"+hex.EncodeToString(sum[:]) || string(raw) == string(ordinary) {
		t.Fatalf("primary declaration must be in the existing interface hash: %s", manifest)
	}
}
