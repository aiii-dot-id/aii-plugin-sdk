package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
)

// WHICH KIT BUILT THIS? The command answers from what the toolchain
// recorded in the binary, so it is proved on binaries built here: a
// build with no VCS stamp says the revision is unknown and why, and a
// build made in a checkout names the checkout's commit.
func TestVersionCommandPrintsWhatTheBuildRecorded(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the CLI twice; skipped in -short")
	}
	repoRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	run := func(name string, buildFlags ...string) string {
		t.Helper()
		exe := filepath.Join(bin, exeName(name))
		build := exec.Command("go", append(append([]string{"build"}, buildFlags...), "-o", exe, "./cmd/aiisdk")...)
		build.Dir = repoRoot
		build.Env = append(os.Environ(), "GOWORK=off")
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("go build %v: %v\n%s", buildFlags, err, out)
		}
		cmd := exec.Command(exe, "version")
		var stderr strings.Builder
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("aiisdk version (%v): %v\nstderr: %s", buildFlags, err, stderr.String())
		}
		got := string(out)
		if strings.Count(got, "\n") != 1 || !strings.HasSuffix(got, "\n") {
			t.Fatalf("aiisdk version (%v) prints one line, got %q", buildFlags, got)
		}
		return strings.TrimSuffix(got, "\n")
	}

	unstamped := run("aiisdk-unstamped", "-buildvcs=false")
	if !strings.HasPrefix(unstamped, "aiisdk "+sdkModulePath+" version=(devel) revision=unknown (") {
		t.Fatalf("a build with no VCS stamp names no revision and says why: %q", unstamped)
	}

	head, err := exec.Command("git", "-C", repoRoot, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Skipf("no git checkout to stamp a revision from (%v); the unstamped half ran", err)
	}
	stamped := run("aiisdk-stamped")
	if !strings.Contains(stamped, " revision="+strings.TrimSpace(string(head))+" modified=") {
		t.Fatalf("a build made in the checkout names its commit %s: %q", strings.TrimSpace(string(head)), stamped)
	}
}

// The line for every record a build can carry: the version and the
// revision as recorded, and "unknown" with its reason where the record
// has none — never a value filled in by the kit.
func TestVersionLineSaysWhatTheRecordHolds(t *testing.T) {
	const (
		pseudo = "v0.1.15-0.20000304050607-123456789abc"
		rev    = "123456789abcdef0123456789abcdef0123456789"
	)
	build := func(version, sum string, vcs ...debug.BuildSetting) *debug.BuildInfo {
		return &debug.BuildInfo{Path: sdkModulePath + "/cmd/aiisdk", Main: debug.Module{Path: sdkModulePath, Version: version, Sum: sum}, Settings: vcs}
	}
	stamp := func(modified string) []debug.BuildSetting {
		return []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: rev}, {Key: "vcs.modified", Value: modified}}
	}
	const why = "Go stamps a revision only into a go build or go install run inside a checkout, without -buildvcs=false"
	noStamp := " (" + why + ")"
	for _, c := range []struct {
		name string
		bi   *debug.BuildInfo
		ok   bool
		want string
	}{
		{"built in a clean checkout", build(pseudo, "", stamp("false")...), true,
			"aiisdk " + sdkModulePath + " version=" + pseudo + " revision=" + rev + " modified=false"},
		{"built in a checkout with changes", build(pseudo+"+dirty", "", stamp("true")...), true,
			"aiisdk " + sdkModulePath + " version=" + pseudo + "+dirty revision=" + rev + " modified=true"},
		{"installed from a release", build("v0.1.14", "h1:x"), true,
			"aiisdk " + sdkModulePath + " version=v0.1.14 revision=unknown" + noStamp},
		{"built with -buildvcs=false or by go run", build("(devel)", ""), true,
			"aiisdk " + sdkModulePath + " version=(devel) revision=unknown" + noStamp},
		{"a record with no version", build("", ""), true,
			"aiisdk " + sdkModulePath + " version=unknown revision=unknown (the build recorded no module version; " + why + ")"},
		{"no build information at all", nil, false,
			"aiisdk version=unknown revision=unknown (this binary carries no build information)"},
	} {
		if got := versionLine(c.bi, c.ok); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}
