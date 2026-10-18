package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/internal/workerdrive"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

// readChecks is a validation.json read against members, as the packer
// reads it.
func readChecks(t *testing.T, file string, members map[string]string) []aiiospkg.Check {
	t.Helper()
	v, err := aiiospkg.ParseValidation([]byte(file), func(rel string) ([]byte, bool, error) {
		raw, ok := members[rel]
		return []byte(raw), ok, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return v.Checks
}

const probesMember = `{"dimension":2,"form":1,"floor":0.3,"model":"m.onnx","probe_bound":0.95,"probes":[{"kind":"cue","text":"where is the harbor","vector":[0.6,0.8]},{"kind":"memory","text":"the ship reached the harbor at dusk","vector":[0.8,0.6]}]}`

// EVERY CHECK IS JUDGED AS THE HOST JUDGES IT, AND EVERY ONE IS REPORTED.
// The calls here answer from a table; the clock moves only when a call
// says the plugin took time. A right answer past its bound is a timeout,
// a wrong one is wrong at any speed, an error in place of a vector is not
// held against the install, a reply that is no vector is wrong, and a call
// past its deadline ends the session for the checks after it.
func TestTheChecksAreJudgedAsTheHostJudgesThem(t *testing.T) {
	checks := readChecks(t, `{"checks":[
		{"name":"embeds the cue","sets":["*"],"lane":"embeddings","input":{"kind":{"$ref":"embeddings.json#/probes/0/kind"},"text":{"$ref":"embeddings.json#/probes/0/text"}},"expect":{"vector_cosine_at_least":{"reference":{"$ref":"embeddings.json#/probes/0/vector"},"min":{"$ref":"embeddings.json#/probe_bound"}}},"within_ms":500},
		{"name":"embeds the memory","sets":["*"],"lane":"embeddings","input":{"kind":"memory","text":"the ship"},"expect":{"vector_cosine_at_least":{"reference":[0.8,0.6],"min":0.95}},"within_ms":500},
		{"name":"embeds late","sets":["*"],"lane":"embeddings","input":{"kind":"cue","text":"late"},"expect":{"vector_cosine_at_least":{"reference":[0.6,0.8],"min":0.95}},"within_ms":500},
		{"name":"answers with an error","sets":["*"],"lane":"embeddings","input":{"kind":"cue","text":"error"},"expect":{"vector_cosine_at_least":{"reference":[0.6,0.8],"min":0.95}},"within_ms":500},
		{"name":"answers no vector","sets":["*"],"lane":"embeddings","input":{"kind":"cue","text":"words"},"expect":{"vector_cosine_at_least":{"reference":[0.6,0.8],"min":0.95}},"within_ms":500},
		{"name":"finds the harbor","sets":["*"],"lane":"operation","call":{"operation":"search","arguments":{"q":"harbor"}},"expect":{"result_contains":["harbor","dusk"]},"within_ms":500},
		{"name":"counts its hits","sets":["*"],"lane":"operation","call":{"operation":"search"},"expect":{"json":{"path":"/hits","at_least":4}},"within_ms":500},
		{"name":"never answers","sets":["*"],"lane":"operation","call":{"operation":"stall"},"expect":{"result_contains":"x"},"within_ms":500},
		{"name":"after the stall","sets":["*"],"lane":"operation","call":{"operation":"search"},"expect":{"result_contains":"harbor"},"within_ms":500}]}`,
		map[string]string{"embeddings.json": probesMember})
	now := time.Unix(1000, 0)
	clock := func() time.Time { return now }
	result := func(v interface{}) *workerdrive.Reply {
		raw, _ := json.Marshal(v)
		return &workerdrive.Reply{Status: "succeeded", OperationResult: raw}
	}
	call := func(op string, args map[string]interface{}) (*workerdrive.Reply, error) {
		switch op {
		case "embed":
			switch args["text"] {
			case "late":
				now = now.Add(600 * time.Millisecond)
				return result(map[string]interface{}{"vector": []float64{0.6, 0.8}}), nil
			case "error":
				return &workerdrive.Reply{IsError: true, Error: json.RawMessage(`{"code":-32000,"message":"the worker is starting"}`)}, nil
			case "words":
				return result(map[string]interface{}{"vector": "none"}), nil
			}
			if args["kind"] == "cue" {
				return result(map[string]interface{}{"vector": []float64{0.6, 0.8}}), nil
			}
			return result(map[string]interface{}{"vector": []float64{1, 0}}), nil
		case "search":
			return result(map[string]interface{}{"text": "the harbor at dusk", "hits": 3}), nil
		}
		now = now.Add(30 * time.Second)
		return nil, fmt.Errorf("%w: no frame within 30s", workerdrive.ErrNoAnswer)
	}
	got := runChecks(checks, call, nil, 2, clock)
	want := []struct{ outcome, words string }{
		{checkPassed, ""},
		{checkWrong, "came back at 0.800 of its reference, under the 0.950 it asks"},
		{checkTimedOut, "answered as you say in 600 ms, past its bound of 500 ms"},
		{checkUnanswered, "an error in place of a vector"},
		{checkWrong, "not a vector"},
		{checkPassed, ""},
		{checkWrong, `the value at "/hits" is 3, under 4`},
		{checkTimedOut, "did not answer within the 30000 ms a call is given"},
		{checkUnanswered, "stopped answering at an earlier check"},
	}
	if len(got) != len(want) {
		t.Fatalf("every check is run and reported: %d of %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].outcome != w.outcome || !strings.Contains(got[i].words, w.words) {
			t.Errorf("%s: %s %q, want %s %q", got[i].name, got[i].outcome, got[i].words, w.outcome, w.words)
		}
	}
	if got[0].cosine == nil || *got[0].cosine < 0.999 || got[1].cosine == nil {
		t.Fatalf("the cosine reached is reported: %v %v", got[0].cosine, got[1].cosine)
	}
	rep := &report{}
	reportChecks(rep, got)
	if rep.fails != 7 || !strings.Contains(rep.entries[0].Detail, "cosine 1.000") || !strings.Contains(rep.entries[len(rep.entries)-1].Detail, `"embeds the memory"`) {
		t.Fatalf("the report names each check and the first failure: %d fails, %+v", rep.fails, rep.entries)
	}
}

// buildCheckChild builds the native test child, a carrier as an author
// builds one.
func buildCheckChild(t *testing.T) string {
	t.Helper()
	gobin := os.Getenv("GO")
	if gobin == "" {
		gobin = "go"
	}
	out := filepath.Join(t.TempDir(), exeName("child"))
	cmd := exec.Command(gobin, "build", "-o", out, "./testdata/checkchild")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the native test child: %v\n%s", err, b)
	}
	return out
}

// A NATIVE PLUGIN'S CHECKS RUN AGAINST ITS CARRIER, STARTED AS THE HOST
// STARTS IT: the carrier this machine's variant names, with exactly the
// host's environment, its ready line awaited (it names the kit that built
// it), and then every check of the staged validation.json for its set.
// Readiness is not a check; a check reads nothing of an identity's (the
// stand-in refuses the plugin's own storage during one, though the run
// grants it); the run passes only when every check passes.
func TestANativePluginsChecksRunAgainstItsCarrier(t *testing.T) {
	child := buildCheckChild(t)
	platform, arch := hostPlatformArch()
	stage := func(t *testing.T, validation string) (string, *aiiospkg.AuthorConfig) {
		dir := t.TempDir()
		raw, err := os.ReadFile(child)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, exeName("child")), raw, 0o755); err != nil {
			t.Fatal(err)
		}
		cfg := &aiiospkg.AuthorConfig{ID: "org.example.checked", Version: "0.1.0", ValidationFile: "validation.json",
			Variants: []aiiospkg.AuthorVariant{{VariantID: "native-here", Platform: platform, Arch: arch, ExecutionRuntime: "native_t3_component", Artifact: exeName("child")}}}
		root := filepath.Join(stageDir(dir, cfg), "install-root")
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, body := range map[string]string{aiiospkg.ValidationFile: validation, aiiospkg.EmbeddingsFile: probesMember} {
			if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir, cfg
	}
	failing := `{"checks":[
		{"name":"embeds the cue","sets":["*"],"lane":"embeddings","input":{"kind":{"$ref":"embeddings.json#/probes/0/kind"},"text":{"$ref":"embeddings.json#/probes/0/text"}},"expect":{"vector_cosine_at_least":{"reference":{"$ref":"embeddings.json#/probes/0/vector"},"min":{"$ref":"embeddings.json#/probe_bound"}}},"within_ms":5000},
		{"name":"embeds the memory","sets":["native-here"],"lane":"embeddings","input":{"kind":{"$ref":"embeddings.json#/probes/1/kind"},"text":{"$ref":"embeddings.json#/probes/1/text"}},"expect":{"vector_cosine_at_least":{"reference":{"$ref":"embeddings.json#/probes/1/vector"},"min":0.95}},"within_ms":5000},
		{"name":"embeds slowly","sets":["*"],"lane":"embeddings","input":{"kind":"cue","text":"say it slowly"},"expect":{"vector_cosine_at_least":{"reference":[0.6,0.8],"min":0.95}},"within_ms":50},
		{"name":"finds the harbor","sets":["*"],"lane":"operation","call":{"operation":"search","arguments":{"q":"harbor"}},"expect":{"result_contains":"harbor"},"within_ms":5000},
		{"name":"reads nothing of the identity","sets":["*"],"lane":"operation","call":{"operation":"peek"},"expect":{"json":{"path":"/refused","equals":true}},"within_ms":5000},
		{"name":"is for another set","sets":["elsewhere"],"lane":"operation","call":{"operation":"search"},"expect":{"result_contains":"never"},"within_ms":5000}]}`
	dir, cfg := stage(t, failing)
	h, err := newHarness([]string{"kv"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rep := &report{}
	runNative(dir, cfg, rep, h, &cfg.Variants[0], 20*time.Second, filepath.Join(dir, "models"), filepath.Join(dir, "runtime"))
	byName := map[string]checkEntry{}
	checks := 0
	for _, e := range rep.entries {
		byName[e.Name] = e
		if strings.HasPrefix(e.Name, "check ") {
			checks++
		}
	}
	if e := byName["readiness"]; e.Status != "pass" || !strings.Contains(e.Detail, "event=ready") || !strings.Contains(e.Detail, "sdk=") {
		t.Fatalf("readiness first, its line naming the kit: %+v", e)
	}
	for name, want := range map[string]string{
		"check embeds the cue":                "pass",
		"check embeds the memory":             "fail",
		"check embeds slowly":                 "fail",
		"check finds the harbor":              "pass",
		"check reads nothing of the identity": "pass",
	} {
		if e := byName[name]; e.Status != want {
			t.Errorf("%s: %+v, want %s", name, e, want)
		}
	}
	if !strings.Contains(byName["check embeds the memory"].Detail, "wrong: came back at 0.800") || !strings.Contains(byName["check embeds slowly"].Detail, "timed_out: answered as you say in") {
		t.Fatalf("how each ended: %+v / %+v", byName["check embeds the memory"], byName["check embeds slowly"])
	}
	if checks != 5 || byName["exit"].Status != "pass" {
		t.Fatalf("the five checks for this set and no other, readiness no check, a clean exit: %d checks, %+v", checks, rep.entries)
	}
	if !strings.Contains(byName["checks"].Detail, `"embeds the memory"`) {
		t.Fatalf("the first failure is named: %+v", byName["checks"])
	}
	if code := finish(rep, nil); code != exitFail {
		t.Fatalf("a run with a failing check exits %d, want %d", code, exitFail)
	}

	// The same carrier with checks that all pass: exit 0.
	passing := `{"checks":[{"name":"finds the harbor","sets":["*"],"lane":"operation","call":{"operation":"search"},"expect":{"result_contains":"harbor"},"within_ms":5000}]}`
	dir, cfg = stage(t, passing)
	rep = &report{}
	runNative(dir, cfg, rep, h, &cfg.Variants[0], 20*time.Second, "", "")
	if code := finish(rep, nil); code != exitPass {
		t.Fatalf("a run whose checks all pass exits %d: %+v", code, rep.entries)
	}
}
