package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// THE PACKER REFUSES A CHECK THE HOST WOULD REFUSE, AND ONE THAT CAN NEVER
// PASS. Each validation.json here is packaged against the same staged
// members and the same descriptor emission: the one that checks a read is
// packed as written; one that checks a write, one on a lane the package
// does not serve, one on a lane no host runs, one whose $ref leaves the
// install-root and one whose input names a member the package does not
// carry are each refused, saying why.
func TestThePackerRefusesAWriteAnUnservedLaneALaneNoHostRunsAndInputsOutsideThePackage(t *testing.T) {
	descriptors := []byte(`[{"id":"search","summary":"finds","effects":"read.internal"},{"id":"store","summary":"keeps","effects":"write.local"},{"id":"ask","summary":"asks","effects":"read.external","operator_confirms":true}]`)
	cfg := &aiiospkg.AuthorConfig{ID: "org.example.checked", Version: "0.1.0",
		Interface: aiiospkg.AuthorInterface{ID: "org.example.search", Version: 1},
		Variants:  []aiiospkg.AuthorVariant{{VariantID: "wasm"}}, ValidationFile: "validation.json"}
	install := map[string][]byte{"inputs.json": []byte(`{"search":{"q":"harbor"}}`)}
	check := func(body string) string {
		return `{"checks":[{"name":"finds the harbor","sets":["*"],` + body + `,"within_ms":2000}]}`
	}
	for _, row := range []struct {
		name, file, want string
	}{
		{"a read, its arguments from another member", check(`"lane":"operation","call":{"operation":"search","arguments":{"$ref":"inputs.json#/search"}},"expect":{"result_contains":"harbor"}`), ""},
		{"a write", check(`"lane":"operation","call":{"operation":"store","arguments":{}},"expect":{"result_contains":"kept"}`), "whose declared effects are write.local"},
		{"an operation that waits for the operator", check(`"lane":"operation","call":{"operation":"ask"},"expect":{"result_contains":"x"}`), "waits for the operator's confirmation"},
		{"an operation the package does not declare", check(`"lane":"operation","call":{"operation":"forget"},"expect":{"result_contains":"x"}`), "which the package does not declare"},
		{"the embeddings lane on a package that is no source", check(`"lane":"embeddings","input":{"kind":"cue","text":"x"},"expect":{"vector_cosine_at_least":{"reference":[1,0],"min":0.9}}`), "not a source of vectors"},
		{"a lane no host runs", check(`"lane":"stream","feed":{"chunks":3}`), `lane "stream", which no host runs`},
		{"a $ref that leaves the install-root", check(`"lane":"operation","call":{"operation":"search","arguments":{"$ref":"../secrets.json#/q"}},"expect":{"result_contains":"x"}`), "leaves the install-root"},
		{"an input the package does not carry", check(`"lane":"operation","call":{"operation":"search","arguments":{"$ref":"missing.json#/q"}},"expect":{"result_contains":"x"}`), "which the package does not carry"},
		{"a set the package does not declare", `{"checks":[{"name":"x","sets":["elsewhere"],"lane":"operation","call":{"operation":"search"},"expect":{"result_contains":"x"},"within_ms":2000}]}`, `component set "elsewhere"`},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "validation.json"), []byte(row.file), 0o644); err != nil {
				t.Fatal(err)
			}
			raw, err := packValidation(dir, cfg, install, descriptors)
			switch {
			case row.want == "" && (err != nil || string(raw) != row.file):
				t.Fatalf("a check of a read must be packed as written: %v", err)
			case row.want != "" && (err == nil || !strings.Contains(err.Error(), row.want)):
				t.Fatalf("want a refusal saying %q, got %v", row.want, err)
			}
		})
	}
}

// sessionWAV is a 16-bit PCM WAV of n silent samples at rate, mono.
func sessionWAV(rate, n int, bits uint16) []byte {
	data := make([]byte, n*int(bits)/8)
	h := []byte("RIFF")
	h = binary.LittleEndian.AppendUint32(h, uint32(36+len(data)))
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint32(h, uint32(rate))
	h = binary.LittleEndian.AppendUint32(h, uint32(rate*int(bits)/8))
	h = binary.LittleEndian.AppendUint16(h, bits/8)
	h = binary.LittleEndian.AppendUint16(h, bits)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	return append(h, data...)
}

// THE PACKER CARRIES A SESSION CHECK'S RECORDING AND REFUSES WHAT THE HOST
// REFUSES. A recording a session check names is read from the
// plugin's directory and packed as an install-root member, the bytes the
// host verifies and sends; the file is then read as the host reads it,
// over what is packed alone. A session check on a package that is no
// resident speech engine, one naming a set that is not native, a recording
// the host cannot send and a recording that is not there are refused.
func TestThePackerCarriesASessionChecksRecordingAndRefusesWhatTheHostRefuses(t *testing.T) {
	voice := func(variants ...aiiospkg.AuthorVariant) *aiiospkg.AuthorConfig {
		if len(variants) == 0 {
			variants = []aiiospkg.AuthorVariant{{VariantID: "macos-native", ExecutionRuntime: "native_t3_component"}}
		}
		return &aiiospkg.AuthorConfig{ID: "org.example.voice", Version: "0.1.0", PluginFamily: "voice_interface",
			Interface: aiiospkg.AuthorInterface{ID: aiiospkg.SessionInterfaceID, Version: 1}, Variants: variants, ValidationFile: "validation.json"}
	}
	hears := func(member string, within int) string {
		return `{"checks":[{"name":"hears it","sets":["*"],"lane":"session","input":{"audio":"` + member + `"},"expect":{"events":{"order":["session_ready","input_finished"],"transcript_contains":["tail"]}},"within_ms":` + strconv.Itoa(within) + `}]}`
	}
	speaks := `{"checks":[{"name":"speaks","sets":["*"],"lane":"session","input":{"text":"The local voice service is ready."},"expect":{"events":{"order":["session_ready","synthesis_end"],"audio_at_least":{"ms":100,"rms_dbfs":-45}}},"within_ms":5000}]}`
	good := sessionWAV(16000, 8000, 16)
	for _, row := range []struct {
		name  string
		cfg   *aiiospkg.AuthorConfig
		file  string
		files map[string][]byte
		want  string
	}{
		{name: "a recording the plugin's directory holds", cfg: voice(), file: hears("checks/tail.wav", 5000), files: map[string][]byte{"checks/tail.wav": good}},
		{name: "a text", cfg: voice(), file: speaks},
		{name: "a package that is no resident speech engine", cfg: &aiiospkg.AuthorConfig{ID: "org.example.tool", Version: "0.1.0", PluginFamily: "tool_bridge",
			Variants: []aiiospkg.AuthorVariant{{VariantID: "macos-native", ExecutionRuntime: "native_t3_component"}}, ValidationFile: "validation.json"},
			file: speaks, want: "no resident speech engine"},
		{name: "a set that is not native", cfg: voice(aiiospkg.AuthorVariant{VariantID: "macos-native", ExecutionRuntime: "native_t3_component"}, aiiospkg.AuthorVariant{VariantID: "wasm", ExecutionRuntime: "wasm_component"}),
			file: speaks, want: `set "wasm", which is not a native component set`},
		{name: "a recording that is not 16-bit", cfg: voice(), file: hears("checks/tail.wav", 5000), files: map[string][]byte{"checks/tail.wav": sessionWAV(16000, 8000, 8)}, want: "is not 16-bit PCM"},
		{name: "a recording that is not there", cfg: voice(), file: hears("checks/gone.wav", 5000), want: "which the package does not carry"},
		{name: "a recording outside the plugin", cfg: voice(), file: hears("../tail.wav", 5000), want: "leaves the install-root"},
		{name: "a bound shorter than the recording", cfg: voice(), file: hears("checks/tail.wav", 400), files: map[string][]byte{"checks/tail.wav": good}, want: "shorter than its recording"},
	} {
		t.Run(row.name, func(t *testing.T) {
			dir := t.TempDir()
			for rel, b := range map[string][]byte{"validation.json": []byte(row.file)} {
				if err := os.WriteFile(filepath.Join(dir, rel), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for rel, b := range row.files {
				if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(rel)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, rel), b, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			install := map[string][]byte{}
			raw, err := packValidation(dir, row.cfg, install, []byte(`[]`))
			switch {
			case row.want == "" && (err != nil || string(raw) != row.file):
				t.Fatalf("a good session check must be packed as written: %v", err)
			case row.want != "" && (err == nil || !strings.Contains(err.Error(), row.want)):
				t.Fatalf("want a refusal saying %q, got %v", row.want, err)
			}
			for rel, b := range row.files {
				if row.want == "" && !bytes.Equal(install[rel], b) {
					t.Fatalf("the recording %s must be packed as an install-root member", rel)
				}
			}
		})
	}
}
