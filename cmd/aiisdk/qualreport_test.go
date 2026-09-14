package main

import (
	"encoding/json"
	"testing"
)

// .
// .
// .
// .
func TestQualReportBoundariesAreHonest(t *testing.T) {
	rep := &report{id: "com.example.hello", version: "0.1.0"}
	rep.pass("build", "dist/x.wasm")
	rep.verified("verify", "VERIFIED T1")
	appendBoundaryChecks(rep, "sha256:aaaa", "sha256:bbbb")

	env := rep.envelope()
	if env.Schema != "aiisdk.qual.v1" {
		t.Fatalf("schema = %q, want aiisdk.qual.v1", env.Schema)
	}
	if env.Result != "pass" {
		t.Fatalf("result = %q, want pass (no local fails)", env.Result)
	}

	byName := map[string]checkEntry{}
	for _, c := range env.Checks {
		byName[c.Name] = c
		// .
		if c.Class == "host" && (c.Status == "pass") {
			t.Fatalf("host-scoped check %q must never be a pass: %+v", c.Name, c)
		}
	}

	// .
	for _, n := range []string{"host-activation", "host-receipt"} {
		c, ok := byName[n]
		if !ok {
			t.Fatalf("missing boundary check %q", n)
		}
		if c.Status != "not_run" || c.Class != "host" {
			t.Fatalf("%q = %+v, want not_run/host", n, c)
		}
		if c.Next == "" {
			t.Fatalf("%q must name the next host-side step", n)
		}
	}

	// .
	// .
	mh := byName["manifest_hash-consumer"]
	if mh.Status != "incomplete" || mh.Next == "" {
		t.Fatalf("manifest_hash-consumer = %+v, want incomplete with a follow-up", mh)
	}

	// .
	si := byName["signing-inputs"]
	if si.Class != "locally_verified" {
		t.Fatalf("signing-inputs class = %q, want locally_verified", si.Class)
	}
	if si.Status != "pass" {
		t.Fatalf("signing-inputs status = %q, want pass (a local read-back)", si.Status)
	}

	// .
	if byName["signature"].Status != "not_run" {
		t.Fatalf("signature = %+v, want not_run", byName["signature"])
	}
}

// .
// .
func TestQualReportResult(t *testing.T) {
	clean := &report{}
	clean.pass("build", "ok")
	appendBoundaryChecks(clean, "sha256:aa", "sha256:bb")
	if r, code := clean.result(); r != "pass" || code != exitPass {
		t.Fatalf("clean local run: result=%q code=%d, want pass/%d", r, code, exitPass)
	}

	failed := &report{}
	failed.fail("verify", "bad signature")
	if r, code := failed.result(); r != "fail" || code != exitFail {
		t.Fatalf("failed run: result=%q code=%d, want fail/%d", r, code, exitFail)
	}

	prereq := &report{}
	prereq.incomplete("prerequisite", "no tinygo", "install tinygo")
	prereq.incompletePrereq = true
	if r, code := prereq.result(); r != "incomplete" || code != exitIncomplete {
		t.Fatalf("prereq run: result=%q code=%d, want incomplete/%d", r, code, exitIncomplete)
	}
}

// .
func TestQualReportJSONStable(t *testing.T) {
	rep := &report{id: "com.example.hello", version: "0.1.0"}
	rep.pass("build", "ok")
	appendBoundaryChecks(rep, "sha256:aa", "sha256:bb")

	b1, err := json.MarshalIndent(rep.envelope(), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// .
	b2, _ := json.MarshalIndent(rep.envelope(), "", "  ")
	if string(b1) != string(b2) {
		t.Fatal("envelope marshaling is not deterministic")
	}
	var back qualEnvelope
	if err := json.Unmarshal(b1, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Schema != "aiisdk.qual.v1" || back.Command != "test" || len(back.Checks) == 0 {
		t.Fatalf("envelope did not round-trip: %+v", back)
	}
}

// .
// .
func TestQualReportWithoutStage(t *testing.T) {
	rep := &report{}
	appendBoundaryChecks(rep, "", "")
	var haveInputs, haveReceipt bool
	for _, c := range rep.entries {
		if c.Name == "signing-inputs" {
			haveInputs = true
		}
		if c.Name == "host-receipt" && c.Status == "not_run" {
			haveReceipt = true
		}
	}
	if haveInputs {
		t.Fatal("no stage => no signing-inputs line")
	}
	if !haveReceipt {
		t.Fatal("host-receipt must still be reported as not_run")
	}
}
