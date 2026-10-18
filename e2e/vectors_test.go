package e2e

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// sharedVectors is every file in vectors/, each a byte copy of one the host
// owns, and the path the host keeps it at. TestVectorsMatchSibling (e2e)
// compares each with the host's tree; TestEveryVectorIsDeclared holds this
// list to the directory and to vectors/README.md, so no file can join the
// directory, or leave this list, without the parity check knowing.
var sharedVectors = map[string]string{
	"audio_framing.json":     "spec/audio/vectors/audio_framing.json",
	"embeddings.json":        "internal/pluginhost/testdata/embeddings.json",
	"framing.json":           "spec/bbb/vectors/framing.json",
	"host_window.json":       "internal/pluginhost/testdata/host_window.json",
	"json_domain.json":       "spec/bbb/vectors/json_domain.json",
	"model_when.json":        "internal/pluginhost/testdata/model_when.json",
	"runtime_extent.json":    "internal/pluginhost/testdata/runtime_extent.json",
	"schema_subset.json":     "internal/jsonschema/testdata/schema_subset.json",
	"session_topology.json":  "spec/audio/vectors/session_topology.json",
	"settings_decl.json":     "internal/pluginhost/testdata/settings_decl.json",
	"validation_checks.json": "internal/pluginhost/testdata/validation_checks.json",
	"variant_selection.json": "internal/pluginhost/testdata/variant_selection.json",
}

// sharedVectorNames is sharedVectors' keys in order, so a report reads the
// same way every run.
func sharedVectorNames() []string {
	names := make([]string, 0, len(sharedVectors))
	for name := range sharedVectors {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func vectorsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "vectors"))
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// EVERY VECTOR IS DECLARED, without a host checkout: this test reads only
// this repository, so it runs in `go test ./...`. A file in vectors/ that
// sharedVectors does not name would never be compared with the host; a
// declared file that is gone would be compared with nothing; and the README
// is where an author learns which files the host owns.
func TestEveryVectorIsDeclared(t *testing.T) {
	dir := vectorsDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	present := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "README.md" {
			continue
		}
		present[e.Name()] = true
		if _, ok := sharedVectors[e.Name()]; !ok {
			t.Errorf("vectors/%s is not declared: name the host's copy in sharedVectors, so the parity check compares it", e.Name())
		}
	}
	for _, name := range sharedVectorNames() {
		if !present[name] {
			t.Errorf("sharedVectors declares %s, which vectors/ does not hold", name)
		}
		if !bytes.Contains(readme, []byte(name)) || !bytes.Contains(readme, []byte(sharedVectors[name])) {
			t.Errorf("vectors/README.md does not name %s and its host copy %s", name, sharedVectors[name])
		}
	}
}
