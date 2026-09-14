package main

// .
// .
// .
// .
// .
// .
// .

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aiii-dot-id/aii-plugin-sdk/internal/workerdrive"
	"github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiospkg"
)

const (
	exitPass       = 0
	exitFail       = 1
	exitIncomplete = 3
)

type grantList []string

func (g *grantList) String() string     { return strings.Join(*g, ",") }
func (g *grantList) Set(v string) error { *g = append(*g, v); return nil }

// .
// .
type testCase struct {
	Name      string          `json:"name"`
	Operation string          `json:"operation"`
	Arguments json.RawMessage `json:"arguments"`
	// .
	// .
	// .
	Settings map[string]json.RawMessage `json:"settings"`
	// .
	// .
	// .
	// .
	// .
	// .
	OperatorAct bool `json:"operator_act"`
	Expect      struct {
		Status         string       `json:"status"`
		ReasonCode     string       `json:"reason_code"`
		ResultContains containsList `json:"result_contains"`
	} `json:"expect"`
}

// .
// .
// .
// .
// .
type checkEntry struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Class  string `json:"class"`
	Detail string `json:"detail,omitempty"`
	Next   string `json:"next,omitempty"`
}

type report struct {
	entries          []checkEntry
	fails            int
	checks           int
	cases            int
	id, version      string
	reportPath       string
	incompletePrereq bool
	packageHash      string
	manifestHash     string
	boundaryDone     bool
}

func (r *report) add(name, status, class, detail, next string) {
	r.entries = append(r.entries, checkEntry{Name: name, Status: status, Class: class, Detail: detail, Next: next})
}

// .
// .
// .
// .
func (r *report) pass(what, detail string) { r.checks++; r.add(what, "pass", "local", detail, "") }
func (r *report) verified(what, detail string) {
	r.checks++
	r.add(what, "pass", "locally_verified", detail, "")
}
func (r *report) fail(what, detail string) {
	r.checks++
	r.fails++
	r.add(what, "fail", "local", detail, "")
}
func (r *report) note(what, detail string)         { r.add(what, "note", "local", detail, "") }
func (r *report) notRun(what, detail, next string) { r.add(what, "not_run", "host", detail, next) }
func (r *report) incomplete(what, detail, next string) {
	r.add(what, "incomplete", "host", detail, next)
}

// .
// .
func (r *report) incompleteLocal(what, detail, next string) {
	r.add(what, "incomplete", "local", detail, next)
}

// .
// .
type qualEnvelope struct {
	Tool    string       `json:"tool"`
	Command string       `json:"command"`
	Schema  string       `json:"schema"`
	ID      string       `json:"id"`
	Version string       `json:"version"`
	Result  string       `json:"result"`
	Checks  []checkEntry `json:"checks"`
}

func (r *report) result() (string, int) {
	switch {
	case r.incompletePrereq:
		return "incomplete", exitIncomplete
	case r.fails > 0:
		return "fail", exitFail
	default:
		return "pass", exitPass
	}
}

func (r *report) envelope() qualEnvelope {
	res, _ := r.result()
	return qualEnvelope{Tool: "aiisdk", Command: "test", Schema: "aiisdk.qual.v1", ID: r.id, Version: r.version, Result: res, Checks: r.entries}
}

// .
// .
// .
func stagedSigningInputs(dir string, cfg *aiiospkg.AuthorConfig) (packageHash, manifestHash string) {
	tree, err := aiiospkg.ReadTree(stageDir(dir, cfg))
	if err != nil {
		return "", ""
	}
	manifest, ok := tree.Files["manifest.json"]
	if !ok {
		return "", ""
	}
	mh, err := aiiospkg.ManifestHash(manifest)
	if err != nil {
		return aiiospkg.PackageHash(tree.InstallFiles()), ""
	}
	return aiiospkg.PackageHash(tree.InstallFiles()), mh
}

// .
// .
// .
// .
// .
func appendBoundaryChecks(rep *report, packageHash, manifestHash string) {
	if packageHash != "" && manifestHash != "" {
		rep.verified("signing-inputs", fmt.Sprintf("package_hash=%s manifest_hash=%s (manifest_hash source: SDK \u00a73.5 canonicalizer)", packageHash, manifestHash))
	}
	rep.incomplete("manifest_hash-consumer",
		"the signing consumer (devsign) may canonicalize manifest_hash differently and was not consulted (devsign not on PATH)",
		"run 'devsign -payload-out' on the staged manifest.json, read manifest_hash back, and compare \u2014 fail closed on mismatch")
	rep.notRun("signature",
		"this command proves the plugin locally and does not sign it",
		"run 'aiisdk sign' to mint the T1-under-dev-root signature \u2014 still local evidence, never a host receipt")
	rep.notRun("host-activation",
		"activation is a host-side act; nothing here activates on any host",
		"activate on the host, then read the result back through the host's verified channel")
	rep.notRun("host-receipt",
		"a host receipt is host-produced data; this command never writes one",
		"obtain the receipt from the host after activation \u2014 only host-produced data is deployment evidence")
}

func cmdTest(args []string) int {
	fs := flag.NewFlagSet("aiisdk test", flag.ExitOnError)
	var grants grantList
	fs.Var(&grants, "grant", "a grant for this run: kv, memory, voice, embeddings, tools, net.outbound:host[:port|:*], net.local:<address|range|name>[:port|:*] for a device on your own network, or root:<name>=<path>[:rw] (repeatable)")
	var settings grantList
	fs.Var(&settings, "setting", "an operator value for a setting plugin.json declares: key=value (repeatable; a case file's settings override it)")
	cases := fs.String("cases", "tests", "directory of case files, one JSON object per file")
	timeout := fs.Duration("timeout", 30*time.Second, "deadline per call and per worker start")
	skipBuild := fs.Bool("skip-build", false, "use dist/<id>.wasm as it is instead of running aiisdk build")
	reportFlag := fs.String("report", "", "also write the qualification report as JSON (schema aiisdk.qual.v1) to this path")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: aiisdk test [-grant <g>]... [-cases dir] [-timeout d] [-skip-build]

Proves the plugin in the current directory on this machine:

  build      aiisdk build (TinyGo, the pinned recipe)
  package    aiisdk package (the canonical .aiiospkg)
  verify     aii plugin verify, the host's own verifier
  account    the module's aiii-plugin-describe equals the packaged descriptor
  admission  the worker's ready banner (bbb_protocol_version=2)
  cases      every tests/*.json case, driven over real BBB frames
  hostile    an undeclared operation is answered, never a crash
  exit       a clean stdin-EOF exit

Host calls the guest makes during a case are answered by a stand-in
host under the grants you name; its storage dies with the run and its
observations are printed as such — they are not receipts, and no
receipt is written by this command.

Every case's arguments carry the host's clock, _host_now_ms, as the host
injects it on every call; a case that names its own keeps it.

Oracles: AII_OS_BIN (a directory holding aii-plugin-worker or aii), or
aii-plugin-worker and aii on PATH. Missing prerequisites end the run
as INCOMPLETE (exit 3), not as a pass. Exit 0 pass, 1 fail.

Case file shape:
  {"name": "…", "operation": "core.echo", "arguments": {…},
   "expect": {"status": "succeeded", "reason_code": "", "result_contains": "" | ["…", "…"]}}
`)
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}
	dir, err := os.Getwd()
	if err != nil {
		return fail("%v", err)
	}
	cfg, err := loadConfigHere(dir)
	if err != nil {
		return fail("%v", err)
	}
	fmt.Printf("aiisdk test: %s %s\n", cfg.ID, cfg.Version)
	rep := &report{id: cfg.ID, version: cfg.Version, reportPath: *reportFlag}

	// .
	var missing []string
	tinygo := ""
	if !*skipBuild {
		if tg, terr := findTinygo(); terr != nil {
			missing = append(missing, terr.Error())
		} else {
			tinygo = tg
		}
	}
	oracle := describeOracle()
	if oracle == nil {
		missing = append(missing, "no worker oracle: set AII_OS_BIN to a directory holding aii-plugin-worker (or aii), or put aii-plugin-worker on PATH")
	}
	verifier := findVerifier()
	if verifier == "" {
		missing = append(missing, "no verifier: set AII_OS_BIN to a directory holding aii, or put aii on PATH")
	}
	if len(missing) > 0 {
		for _, m := range missing {
			rep.incompleteLocal("prerequisite", m, "install the missing tool or point AII_OS_BIN at it, then re-run")
		}
		rep.incompletePrereq = true
		return finish(rep, nil)
	}
	fmt.Printf("  pins: %s · %s · kit %s · oracle %s · verifier %s\n", tinygoVersion(tinygo), runtime.Version(), kitRevision(), strings.Join(oracle, " "), verifier)
	var h *harness
	if h, err = newHarness(grants, cfg.Settings, settings); err != nil {
		return fail("%v", err)
	}
	h.pluginID = cfg.ID

	// .
	module := wasmPath(dir, cfg)
	if *skipBuild {
		if fi, err := os.Stat(module); err != nil || fi.Size() == 0 {
			rep.fail("build", fmt.Sprintf("-skip-build, and no module at %s", module))
			return finish(rep, h)
		}
		rep.pass("build", fmt.Sprintf("%s (as built; -skip-build)", module))
	} else if code := cmdBuild(nil); code != 0 {
		rep.fail("build", "aiisdk build failed (above)")
		return finish(rep, h)
	} else {
		fi, _ := os.Stat(module)
		rep.pass("build", fmt.Sprintf("%s (%d bytes)", module, fi.Size()))
	}
	if code := cmdPackage(nil); code != 0 {
		rep.fail("package", "aiisdk package failed (above)")
		return finish(rep, h)
	}
	pkg := filepath.Join(dir, "dist", cfg.ID+"-"+cfg.Version+".aiiospkg")
	rep.pass("package", pkg)
	rep.packageHash, rep.manifestHash = stagedSigningInputs(dir, cfg)

	// .
	out, verr := exec.Command(verifier, "plugin", "verify", pkg).CombinedOutput()
	first := strings.TrimSpace(firstLineOf(string(out)))
	if verr != nil || !strings.HasPrefix(first, "VERIFIED T") {
		rep.fail("verify", fmt.Sprintf("%s: %s", verifier, first))
		return finish(rep, h)
	}
	rep.verified("verify", first+" (aii plugin verify)")

	// .
	account, aerr := runOracleDescribe(oracle, module)
	staged, _ := filepath.Glob(filepath.Join(dir, "dist", "pkg", cfg.ID+"-"+cfg.Version, "install-root", "interfaces", "*.schema.json"))
	switch {
	case aerr != nil:
		rep.fail("account", "the module answers no account: "+aerr.Error())
	case len(staged) != 1:
		rep.fail("account", fmt.Sprintf("expected one packaged descriptor file, found %d", len(staged)))
	default:
		packaged, _ := os.ReadFile(staged[0])
		if string(packaged) != string(account) {
			rep.fail("account", "the module's aiii-plugin-describe and the packaged descriptor differ")
		} else {
			rep.verified("account", "the module's aiii-plugin-describe equals the packaged descriptor")
		}
	}

	// .
	w, werr := workerdrive.StartForward(oracle[0], oracle[1:], module, *timeout, h.answer)
	if werr != nil {
		rep.fail("admission", werr.Error())
		return finish(rep, h)
	}
	if !strings.Contains(w.Banner(), "bbb_protocol_version=2") {
		rep.fail("admission", "ready banner without bbb_protocol_version=2: "+w.Banner())
	} else {
		rep.pass("admission", "ready banner, bbb_protocol_version=2")
	}
	files, _ := filepath.Glob(filepath.Join(*cases, "*.json"))
	sort.Strings(files)
	if len(files) == 0 {
		rep.note("cases", fmt.Sprintf("no case files under %s/ — only the built-in checks ran", *cases))
	}
	for i, f := range files {
		runCase(rep, h, w, i, f, *timeout)
	}
	// .
	// .
	reply, herr := w.Invoke(`"hostile-1"`, "harness.undeclared", nil, *timeout)
	switch {
	case herr != nil:
		rep.fail("hostile", "an undeclared operation broke the session: "+herr.Error())
	case reply.IsError || reply.Status == "failed" || reply.Status == "denied":
		rep.pass("hostile", "an undeclared operation is answered, not crashed")
	default:
		rep.fail("hostile", "an undeclared operation reported success: "+string(reply.Raw))
	}
	if cerr := w.Close(*timeout); cerr != nil {
		rep.fail("exit", cerr.Error())
	} else {
		rep.pass("exit", "clean stdin-EOF exit 0")
	}
	h.closeFiles()
	return finish(rep, h)
}

func runCase(rep *report, h *harness, w *workerdrive.Worker, i int, path string, timeout time.Duration) {
	rep.cases++
	raw, err := os.ReadFile(path)
	if err != nil {
		rep.fail("case", path+": "+err.Error())
		return
	}
	var c testCase
	if err := json.Unmarshal(raw, &c); err != nil || c.Operation == "" {
		rep.fail("case", fmt.Sprintf("%s: not a case (needs operation): %v", path, err))
		return
	}
	name := c.Name
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), ".json")
	}
	caseSettings, serr := caseSettingValues(h.decls, c.Settings)
	if serr != nil {
		rep.fail("case", fmt.Sprintf("%s: %v", name, serr))
		return
	}
	h.caseSettings = caseSettings
	defer func() { h.caseSettings = nil }()
	want := c.Expect.Status
	if want == "" {
		want = "succeeded"
	}
	started := time.Now()
	reply, err := w.Invoke(fmt.Sprintf(`"case-%d"`, i+1), c.Operation, withOperatorAct(withHostClock(c.Arguments), c.OperatorAct, c.Name), timeout)
	if err != nil {
		rep.fail("case "+name, err.Error())
		return
	}
	got, reason := "error", ""
	var payload string
	if reply.IsError {
		var eo struct {
			Data struct {
				ReasonCode string `json:"reasonCode"`
			} `json:"data"`
		}
		_ = json.Unmarshal(reply.Error, &eo)
		reason, payload = eo.Data.ReasonCode, string(reply.Error)
	} else {
		got, reason, payload = reply.Status, reply.ReasonCode, string(reply.OperationResult)
		if reply.Status == "" {
			got = "succeeded"
		}
	}
	switch {
	case got != want:
		rep.fail("case "+name, fmt.Sprintf("want status %s, got %s (%s)", want, got, excerpt(payload)))
	case c.Expect.ReasonCode != "" && reason != c.Expect.ReasonCode:
		rep.fail("case "+name, fmt.Sprintf("want reason_code %s, got %q", c.Expect.ReasonCode, reason))
	case c.Expect.ResultContains.missingFrom(payload) != "":
		rep.fail("case "+name, fmt.Sprintf("result lacks %q: %s", c.Expect.ResultContains.missingFrom(payload), excerpt(payload)))
	default:
		rep.pass("case "+name, fmt.Sprintf("%s (%d ms)", got, time.Since(started).Milliseconds()))
	}
}

// .
// .
// .
func (r *report) ensureBoundary() {
	if r.boundaryDone {
		return
	}
	appendBoundaryChecks(r, r.packageHash, r.manifestHash)
	r.boundaryDone = true
}

func finish(rep *report, h *harness) int {
	rep.ensureBoundary()
	result, code := rep.result()
	for _, e := range rep.entries {
		switch e.Status {
		case "pass":
			fmt.Printf("  PASS  %-12s %s\n", e.Name, e.Detail)
		case "fail":
			fmt.Printf("  FAIL  %-12s %s\n", e.Name, e.Detail)
		case "note":
			fmt.Printf("  NOTE  %-12s %s\n", e.Name, e.Detail)
		case "incomplete":
			fmt.Printf("  INCOMPLETE  %-16s %s\n", e.Name, e.Detail)
			if e.Next != "" {
				fmt.Printf("              \u2192 %s\n", e.Next)
			}
		case "not_run":
			fmt.Printf("  NOT-RUN     %-16s %s\n", e.Name, e.Detail)
			if e.Next != "" {
				fmt.Printf("              \u2192 %s\n", e.Next)
			}
		}
	}
	if h != nil {
		for _, o := range h.observations {
			fmt.Printf("  harness observed: %s\n", o)
		}
	}
	fmt.Println("  harness observations are local and are not receipts; a host writes receipts, this command does not.")
	fmt.Println("  scope: local = ran here · locally_verified = a check read back here · host = a boundary this command does not cross.")
	if rep.reportPath != "" {
		if b, err := json.MarshalIndent(rep.envelope(), "", "  "); err == nil {
			if werr := os.WriteFile(rep.reportPath, append(b, '\n'), 0o644); werr != nil {
				fmt.Printf("  (could not write -report %s: %v)\n", rep.reportPath, werr)
			} else {
				fmt.Printf("  report: %s (schema aiisdk.qual.v1)\n", rep.reportPath)
			}
		}
	}
	switch result {
	case "incomplete":
		fmt.Println("RESULT: INCOMPLETE — prerequisites missing; nothing was proven")
	case "fail":
		fmt.Printf("RESULT: FAIL (%d of %d checks failed, %d cases) — local qualification\n", rep.fails, rep.checks, rep.cases)
	default:
		fmt.Printf("RESULT: PASS (%d checks, %d cases) — LOCAL qualification only; signature, host activation and host receipt were not run (this is not deployment evidence)\n", rep.checks, rep.cases)
	}
	return code
}

// .
// .
func findVerifier() string {
	if bin := os.Getenv("AII_OS_BIN"); bin != "" {
		for _, n := range []string{"aii", "aii.exe"} {
			if p := filepath.Join(bin, n); fileExists(p) {
				return p
			}
		}
	}
	if p, err := exec.LookPath("aii"); err == nil {
		return p
	}
	return ""
}

func tinygoVersion(tinygo string) string {
	if tinygo == "" {
		return "tinygo (not used: -skip-build)"
	}
	out, err := exec.Command(tinygo, "version").Output()
	if err != nil {
		return "tinygo (version unknown)"
	}
	return strings.TrimSpace(firstLineOf(string(out)))
}

func kitRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, mod := "", ""
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			mod = s.Value
		}
	}
	if rev == "" {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		return "unversioned build"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if mod == "true" {
		rev += "+modified"
	}
	return rev
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func excerpt(s string) string {
	if len(s) > 160 {
		return s[:160] + "…"
	}
	return s
}

// .
// .
// .
// .
// .
func withHostClock(args json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if len(args) == 0 || json.Unmarshal(args, &m) != nil || m == nil {
		m = map[string]json.RawMessage{}
	}
	if _, has := m["_host_now_ms"]; !has {
		m["_host_now_ms"] = json.RawMessage(strconv.FormatInt(time.Now().UnixMilli(), 10))
	}
	out, err := json.Marshal(m)
	if err != nil {
		return args
	}
	return out
}

// .
// .
// .
func withOperatorAct(args json.RawMessage, on bool, caseName string) json.RawMessage {
	if !on {
		return args
	}
	var m map[string]json.RawMessage
	if len(args) == 0 || json.Unmarshal(args, &m) != nil || m == nil {
		m = map[string]json.RawMessage{}
	}
	stamp, _ := json.Marshal(map[string]string{"id": "harness-act-" + caseName, "confirmed_at": time.Now().UTC().Format(time.RFC3339)})
	m["_host_operator_act"] = stamp
	out, err := json.Marshal(m)
	if err != nil {
		return args
	}
	return out
}
