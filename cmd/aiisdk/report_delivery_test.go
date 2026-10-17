package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aiii-dot-id/aii-plugin-sdk/internal/fileperm"
)

// A REPORT ASKED FOR AND NOT DELIVERED FAILS THE RUN. The caller that
// passed -report reads that path next; exit 0 tells it the file there is
// this run's.
func TestAPassingRunWhoseReportIsNotWrittenFails(t *testing.T) {
	dir := t.TempDir()
	for name, path := range map[string]string{
		"the directory does not exist": filepath.Join(dir, "no-such-dir", "report.json"),
		"the path is a directory":      dir,
	} {
		rep := &report{id: "com.example.hello", version: "0.1.0", reportPath: path}
		rep.pass("build", "ok")
		if code := finish(rep, nil); code != exitFail {
			t.Errorf("%s: a passing run exited %d with no report written, want %d", name, code, exitFail)
		}
		if left, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".aiisdk-report-*")); len(left) != 0 {
			t.Errorf("%s: the failed delivery left its work file behind: %v", name, left)
		}
	}
}

// AND ITS ABSENCE NEVER CHANGES A VERDICT ALREADY NOT A PASS.
func TestAMissingReportNeverChangesAFailingOrIncompleteVerdict(t *testing.T) {
	nowhere := filepath.Join(t.TempDir(), "no-such-dir", "report.json")

	failing := &report{reportPath: nowhere}
	failing.fail("case-a", "wrong output")
	if code := finish(failing, nil); code != exitFail {
		t.Errorf("a failing run exited %d, want %d", code, exitFail)
	}
	prereq := &report{reportPath: nowhere}
	prereq.incomplete("prerequisite", "no tinygo", "install tinygo")
	prereq.incompletePrereq = true
	if code := finish(prereq, nil); code != exitIncomplete {
		t.Errorf("an incomplete run exited %d, want %d", code, exitIncomplete)
	}
}

// A DELIVERED REPORT IS THE RUN'S, WHOLE, and a failed delivery leaves
// what was at the path as it was — never half of a new one.
func TestTheReportArrivesWholeOrNotAtAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "report.json")
	rep := &report{id: "com.example.hello", version: "0.1.0", reportPath: path}
	rep.pass("build", "ok")
	if code := finish(rep, nil); code != exitPass {
		t.Fatalf("a passing run with a writable report exited %d", code)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back qualEnvelope
	if err := json.Unmarshal(raw, &back); err != nil || back.Schema != "aiisdk.qual.v1" || back.Result != "pass" {
		t.Fatalf("the report does not read back as this run's: %v %+v", err, back)
	}
	// As before: the access the report had when it was written in place
	// at 0644, not the work file's 0600 — on unix the mode, on Windows the
	// read-only attribute and the DACL the folder hands down.
	direct := filepath.Join(dir, "direct.json")
	if err := os.WriteFile(direct, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(direct, 0o644); err != nil { // past the umask, as the delivery's own chmod is
		t.Fatal(err)
	}
	got, err := fileperm.Access(path)
	if err != nil {
		t.Fatal(err)
	}
	if want, err := fileperm.Access(direct); err != nil || got != want {
		t.Errorf("report access %s, want %s as before (%v)", got, want, err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, ".aiisdk-report-*"))
	if len(left) != 0 {
		t.Errorf("the work file was left behind: %v", left)
	}
}
