package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// THE PACKER REFUSES WHAT THE HOST WOULD REFUSE OF A SOURCE OF VECTORS,
// the descriptor included. A source's one operation is driven by the host
// and may not ask for the operator's confirmation; the host refuses such a
// package when it is activated. The packer held the operation's name and
// not its descriptor, so the package was made and the author learned of
// it on somebody's machine. Here the packer's own step is given the
// declaration file and the plugin's emission: a descriptor that does not
// ask is packed, with the file's bytes as written, and one that asks is
// refused by name.
func TestThePackerRefusesASourceWhoseDescriptorAsksForAConfirmation(t *testing.T) {
	dir := t.TempDir()
	member := `{"dimension":2,"form":1,"floor":0.3,"model":"m.onnx","probe_bound":0.95,"probes":[{"kind":"cue","text":"where is the harbor","vector":[0.6,0.8]},{"kind":"memory","text":"the ship reached the harbor at dusk","vector":[0.8,0.6]}]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "embeddings.json"), []byte(member), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &aiiospkg.AuthorConfig{ID: "com.example.meaning", Version: "0.1.0", PluginFamily: aiiospkg.EmbeddingsFamily, AiiosMinVersion: aiiospkg.EmbeddingsMinHost,
		Interface:          aiiospkg.AuthorInterface{ID: aiiospkg.EmbeddingsInterfaceID, Version: aiiospkg.EmbeddingsInterfaceVersion},
		CapabilityEnvelope: []string{}, EmbeddingsFile: "embeddings.json",
		Models: []aiiospkg.ModelDecl{{Name: "m.onnx", URL: "https://models.example.test/m.onnx", SHA256: strings.Repeat("a", 64), Size: 1 << 20}},
	}
	methods := []string{aiiospkg.MethodEmbed}

	raw, err := packEmbeddings(dir, cfg, methods, []byte(`[{"id":"embed","summary":"one vector for one text","effects":"read.internal"}]`))
	if err != nil || string(raw) != member {
		t.Fatalf("a source whose descriptor asks for nothing must be packed, its declaration as written: %v", err)
	}
	_, err = packEmbeddings(dir, cfg, methods, []byte(`[{"id":"embed","summary":"one vector for one text","effects":"read.internal","operator_confirms":true}]`))
	if err == nil || !strings.Contains(err.Error(), "embed may not declare operator_confirms") {
		t.Fatalf("a source whose descriptor asks for a confirmation must be refused by name: %v", err)
	}
	// The earlier checks still come first: a second operation is refused
	// for what it is, whatever its descriptor says.
	_, err = packEmbeddings(dir, cfg, []string{aiiospkg.MethodEmbed, "status"}, []byte(`[{"id":"embed"},{"id":"status","operator_confirms":true}]`))
	if err == nil || !strings.Contains(err.Error(), "describes one operation") {
		t.Fatalf("two operations: %v", err)
	}
}
