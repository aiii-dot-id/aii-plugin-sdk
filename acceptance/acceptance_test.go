//go:build acceptance

package acceptance

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
)

// The scaffold's identity and first operation (aiisdk init.go /
// mainGoTemplate): id "helloworld", one operation "core.echo" that
// returns its arguments unchanged. Those are the strings the proof
// asserts against.
const (
	pluginID      = "helloworld"
	pluginVersion = "0.1.0"
	echoOp        = "core.echo"
)

// env knobs (all optional) — the same seams e2e/e2e_test.go uses.
func sourceRepo(t *testing.T) string {
	t.Helper()
	// ACCEPT_SRC_REPO lets a pre-commit validation run clone a throwaway
	// committed copy; by default the clone source is THIS repo, whose
	// committed HEAD is what `make acceptance` proves.
	if v := os.Getenv("ACCEPT_SRC_REPO"); v != "" {
		return v
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func sibling(t *testing.T) string {
	t.Helper()
	for _, c := range []string{os.Getenv("AII_OS_DIR"), "../../aii-os"} {
		if c == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "cmd", "aii", "main.go")); err == nil {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	t.Skip("acceptance: no sibling aii-os checkout (set AII_OS_DIR) — the oracles cannot be built")
	return ""
}

func siblingGo(t *testing.T) string {
	t.Helper()
	dir := os.Getenv("AII_OS_GO")
	if dir == "" {
		if p, err := exec.LookPath("go"); err == nil {
			dir = filepath.Dir(p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "go")); err != nil {
		t.Skipf("acceptance: the host's Go toolchain was not found (set AII_OS_GO to its bin directory)")
	}
	return dir
}

func tinygo(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("TINYGO"); v != "" {
		return v
	}
	if p, err := exec.LookPath("tinygo"); err == nil {
		return p
	}
	t.Skip("acceptance: tinygo not found (set TINYGO) — the guest build needs it")
	return ""
}

// stage logs a headline so the transcript reads as a proof narrative.
func stage(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Logf("\n=== %s", fmt.Sprintf(format, args...))
}

// runIn runs name+args in dir with extra env, failing the test (with the
// full combined output) on error. It returns stdout+stderr.
func runIn(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s\n(dir %s)\nfailed: %v\n%s", name, strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// TestAcceptance is the whole proof, in order, but for the examples: once
// the shared setup stands, each example's block runs beside the others. A
// failure at any stage stops the run with the offending command's output;
// a failed example lets the other examples finish first, each with its own.
func TestAcceptance(t *testing.T) {
	src := sourceRepo(t)
	sib := sibling(t)
	goDir := siblingGo(t)
	tg := tinygo(t)

	work := t.TempDir()
	clone := filepath.Join(work, "clone")
	pluginParent := filepath.Join(work, "author")
	binDir := filepath.Join(work, "bin")
	if err := os.MkdirAll(pluginParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// ---- 1. A genuinely fresh git clone, outside the working tree. ----
	stage(t, "clone the standalone SDK into a fresh location")
	runIn(t, work, nil, "git", "clone", "--quiet", src, clone)
	// Independence check: the clone must not reference the source tree.
	assertNoSourceReference(t, clone, src)

	// ---- 2. Build the aiisdk CLI from the clone, offline. ----
	stage(t, "build the aiisdk CLI from the clone (offline)")
	aiisdk := filepath.Join(binDir, "aiisdk")
	runIn(t, clone, []string{"GOFLAGS=-buildvcs=false", "GOPROXY=off", "GO111MODULE=on"},
		"go", "build", "-mod=readonly", "-o", aiisdk, "./cmd/aiisdk")

	// ---- 3. Scaffold the helloworld plugin, replace-wired to the clone. ----
	stage(t, "aiisdk init %s (replace-wired to the clone — no source-tree reference)", pluginID)
	runIn(t, pluginParent, nil, aiisdk, "init", "-sdk", clone, pluginID)
	pluginDir := filepath.Join(pluginParent, pluginID)
	if _, err := os.Stat(filepath.Join(pluginDir, "plugin.json")); err != nil {
		t.Fatalf("scaffold produced no plugin.json: %v", err)
	}

	// ---- 4. Build the guest wasm with TinyGo (the pinned recipe). ----
	stage(t, "aiisdk build (TinyGo wasm-unknown, offline)")
	runIn(t, pluginDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
	wasm := filepath.Join(pluginDir, "dist", pluginID+".wasm")
	if fi, err := os.Stat(wasm); err != nil || fi.Size() == 0 {
		t.Fatalf("guest module missing or empty at %s: %v", wasm, err)
	}

	// ---- 5. Package the canonical T0 .aiiospkg (the SDK's own writer). ----
	stage(t, "aiisdk package (canonical .aiiospkg, unsigned T0)")
	runIn(t, pluginDir, []string{"GOPROXY=off"}, aiisdk, "package")
	pkg := filepath.Join(pluginDir, "dist", pluginID+"-"+pluginVersion+".aiiospkg")
	if _, err := os.Stat(pkg); err != nil {
		t.Fatalf("package missing at %s: %v", pkg, err)
	}

	// ---- 5b. THE CATALOG ENTRY: publish emits a portable entry. ----
	stage(t, "aiisdk publish -> one portable catalog entry for aiios-plugins.md")
	pubOut := runIn(t, pluginDir, nil, aiisdk, "publish", "-url", "https://example.invalid/"+pluginID+".aiiospkg", "-tier", "T1")
	var entry struct {
		ID       string `json:"id"`
		Version  string `json:"version"`
		Tier     string `json:"tier"`
		Packages []struct {
			Platform, Arch, URL, SHA256 string
			Size                        int64
		} `json:"packages"`
	}
	if err := json.Unmarshal([]byte(pubOut), &entry); err != nil {
		t.Fatalf("publish must emit a JSON catalog entry: %v\n%s", err, pubOut)
	}
	if entry.ID != pluginID || entry.Version != pluginVersion || entry.Tier != "T1" {
		t.Fatalf("catalog entry identity: %+v", entry)
	}
	if len(entry.Packages) != 1 || entry.Packages[0].Platform != "*" || entry.Packages[0].Arch != "*" {
		t.Fatalf("a WASM plugin publishes one portable package: %+v", entry.Packages)
	}
	if !strings.HasPrefix(entry.Packages[0].SHA256, "sha256:") || entry.Packages[0].Size == 0 {
		t.Fatalf("the entry carries the package hash and size: %+v", entry.Packages[0])
	}

	// ---- 6. Build the host oracles from the sibling aii-os. ----
	stage(t, "build the host oracles (aii, aii-plugin-worker) from the sibling aii-os")
	aii := filepath.Join(binDir, "aii")
	worker := filepath.Join(binDir, "aii-plugin-worker")
	oracleEnv := []string{
		"PATH=" + goDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GOTOOLCHAIN=local",
	}
	runIn(t, sib, oracleEnv, filepath.Join(goDir, "go"), "build", "-buildvcs=false", "-o", aii, "./cmd/aii")
	runIn(t, sib, oracleEnv, filepath.Join(goDir, "go"), "build", "-buildvcs=false", "-o", worker, "./cmd/aii-plugin-worker")

	// ---- 7. THE T0 HEADLINE: the independent host verifier accepts it. ----
	stage(t, "aii plugin verify -> VERIFIED T0 (the independent host verifier)")
	wantT0 := fmt.Sprintf("VERIFIED T0 %s %s", pluginID, pluginVersion)
	out := runIn(t, pluginDir, nil, aii, "plugin", "verify", pkg)
	if !strings.Contains(out, wantT0) {
		t.Fatalf("host verify did not report %q:\n%s", wantT0, out)
	}
	t.Logf("host verifier: %s", firstLine(out))

	// ---- 8. THE T0 HEADLINE: the module RUNS on the host worker. ----
	stage(t, "run the module on aii-plugin-worker and round-trip %s", echoOp)
	proveRunsAndEchoes(t, worker, wasm)

	// ---- 8a. THE ARTIFACT'S OWN ACCOUNT: the module's describe export is the packaged descriptor. ----
	// The package was written before any oracle existed here, so its
	// descriptor came through the fallback door (go run . under a
	// scrubbed environment); the export is the other door, and the host
	// holds a package to it at activation. Two doors, one account.
	stage(t, "aii-plugin-worker -describe equals the packaged descriptor file (two doors, one account)")
	account, err := exec.Command(worker, "-describe", wasm).Output()
	if err != nil {
		t.Fatalf("the module must answer its own account: %v", err)
	}
	staged, _ := filepath.Glob(filepath.Join(pluginDir, "dist", "pkg", pluginID+"-"+pluginVersion, "install-root", "interfaces", "*.schema.json"))
	if len(staged) != 1 {
		t.Fatalf("expected one packaged descriptor file, found %v", staged)
	}
	packaged, err := os.ReadFile(staged[0])
	if err != nil {
		t.Fatal(err)
	}
	if string(account) != string(packaged) {
		t.Fatalf("the artifact's account and the packaged descriptor differ:\n%s\n---\n%s", account, packaged)
	}

	// ---- 8b. THE AUTHOR'S OWN PROOF: aiisdk test against the distributed oracles. ----
	stage(t, "aiisdk test -skip-build -grant kv (the hermetic harness verb, oracles from AII_OS_BIN)")
	testOut := runIn(t, pluginDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, "test", "-skip-build", "-grant", "kv")
	for _, want := range []string{"PASS  case echo returns its arguments", "PASS  account", "PASS  hostile", "PASS  exit", "harness observations are local", "RESULT: PASS"} {
		if !strings.Contains(testOut, want) {
			t.Fatalf("aiisdk test must report %q:\n%s", want, testOut)
		}
	}

	// ---- 8c-8l. THE EXAMPLES, side by side. ----
	// Each block below is one example's whole proof and stands alone: it
	// reads the clone, the CLI and the oracles built above, and writes only
	// under its own examples/<name>. So the blocks run as parallel subtests,
	// as many at once as -parallel allows (the processors, by default); each
	// names its stage and reports its own failure whatever the others do.
	// The group returns when every block has finished, and a failed block
	// stops the run there, as a failed stage always has: the signing stages
	// below change the scaffold's .keys and stay after the group, in order.
	examples := t.Run("examples", func(t *testing.T) {
		// ---- 8i. THE NATIVE MATRIX: the skeleton built for three desktop platforms, packaged as one native variant each, and refused by the host below T3 — native code is the platform signature's lane. ----
		t.Run("native-skel", func(t *testing.T) {
			t.Parallel()
			stage(t, "examples/native-skel: build for linux/x86_64, macos/arm64, windows/x86_64; aiisdk package; aii plugin verify refuses below T3 (WASM_BASELINE_MISSING)")
			exDir := filepath.Join(clone, "examples", "native-skel")
			for _, target := range [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}} {
				out := map[[2]string]string{{"linux", "amd64"}: "dist/native-skel-linux-x86_64", {"darwin", "arm64"}: "dist/native-skel-macos-arm64", {"windows", "amd64"}: "dist/native-skel-windows-x86_64.exe"}[target]
				runIn(t, exDir, []string{"PATH=" + goDir + string(os.PathListSeparator) + os.Getenv("PATH"), "GOTOOLCHAIN=local", "GOFLAGS=-buildvcs=false", "CGO_ENABLED=0", "GOOS=" + target[0], "GOARCH=" + target[1], "GOPROXY=off"}, filepath.Join(goDir, "go"), "build", "-o", out, ".")
			}
			runIn(t, exDir, []string{"GOPROXY=off"}, aiisdk, "package")
			nativePkg := filepath.Join(exDir, "dist", "org.example.native-skel-0.1.0.aiiospkg")
			// The host's tier contract: T0 needs a wasm baseline, and a native
			// variant is admitted only at T3 — the platform's signature, which the
			// kit never mints. The honest word for an unsigned native-only package
			// is therefore a refusal with that reason, not a T0 pass.
			vcmd := exec.Command(aii, "plugin", "verify", nativePkg)
			vcmd.Dir = exDir
			vbytes, verr := vcmd.CombinedOutput()
			vout := string(vbytes)
			if verr == nil || !strings.Contains(vout, "WASM_BASELINE_MISSING") {
				t.Fatalf("an unsigned native-only package is refused below T3 with WASM_BASELINE_MISSING (err=%v):\n%s", verr, vout)
			}
			for _, rel := range []string{"variants/linux-x86_64-native/plugin", "variants/macos-arm64-native/plugin", "variants/windows-x86_64-native/plugin"} {
				matches, _ := filepath.Glob(filepath.Join(exDir, "dist", "pkg", "org.example.native-skel-0.1.0", "install-root", rel+"*"))
				if len(matches) == 0 {
					t.Fatalf("the package carries the %s artifact", rel)
				}
			}
		})

		// ---- 8j. THE SPEECH ENGINE SHAPE: the resident-session skeleton built for three desktop platforms, packaged as one native variant each with the seven controls as its declared operations, and refused by the host below T3. ----
		t.Run("voice-skel", func(t *testing.T) {
			t.Parallel()
			stage(t, "examples/voice-skel: build for linux/x86_64, macos/arm64, windows/x86_64; aiisdk package; aii plugin verify refuses below T3 (WASM_BASELINE_MISSING)")
			exDir := filepath.Join(clone, "examples", "voice-skel")
			for _, target := range [][2]string{{"linux", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}} {
				out := map[[2]string]string{{"linux", "amd64"}: "dist/voice-skel-linux-x86_64", {"darwin", "arm64"}: "dist/voice-skel-macos-arm64", {"windows", "amd64"}: "dist/voice-skel-windows-x86_64.exe"}[target]
				runIn(t, exDir, []string{"PATH=" + goDir + string(os.PathListSeparator) + os.Getenv("PATH"), "GOTOOLCHAIN=local", "GOFLAGS=-buildvcs=false", "CGO_ENABLED=0", "GOOS=" + target[0], "GOARCH=" + target[1], "GOPROXY=off"}, filepath.Join(goDir, "go"), "build", "-o", out, ".")
			}
			runIn(t, exDir, []string{"GOPROXY=off"}, aiisdk, "package")
			voicePkg := filepath.Join(exDir, "dist", "com.example.voice-skel-0.1.0.aiiospkg")
			vcmd := exec.Command(aii, "plugin", "verify", voicePkg)
			vcmd.Dir = exDir
			vbytes, verr := vcmd.CombinedOutput()
			vout := string(vbytes)
			if verr == nil || !strings.Contains(vout, "WASM_BASELINE_MISSING") {
				t.Fatalf("an unsigned native-only voice package is refused below T3 with WASM_BASELINE_MISSING (err=%v):\n%s", verr, vout)
			}
			// The descriptor emission named the seven controls — the host reads
			// them as the session's controls, never as tools.
			schema, err := os.ReadFile(filepath.Join(exDir, "dist", "pkg", "com.example.voice-skel-0.1.0", "install-root", "interfaces", "speech.session.v1.schema.json"))
			if err != nil {
				t.Fatalf("the package carries the descriptor emission: %v", err)
			}
			for _, op := range []string{"speech.session.open", "speech.session.synthesize", "speech.session.cancel_synthesis", "speech.session.stop_playback", "speech.session.finish_input", "speech.session.close", "speech.session.status"} {
				if !strings.Contains(string(schema), `"`+op+`"`) {
					t.Fatalf("the descriptor emission names %s", op)
				}
			}
		})

		// ---- 8g. THE LOGGING PLUGIN AND THE MCP CONNECTOR: events delivered and read back; tools published from a list. ----
		for _, ex := range []struct {
			dir    string
			grants []string
			wants  []string
		}{
			{"logging", nil, []string{"PASS  case a delivered tool event becomes a line", "PASS  case the log reads back the recent lines", "PASS  case a call without a topic is not an event", "RESULT: PASS"}},
			{"mcp-connector", []string{"-grant", "tools"}, []string{"PASS  case a server's tool list is published", "PASS  case a published tool's call is forwarded and denied without a grant", "PASS  case refreshing from the server is denied without a grant", "RESULT: PASS"}},
		} {
			t.Run(ex.dir, func(t *testing.T) {
				t.Parallel()
				stage(t, "examples/%s: aiisdk build, then aiisdk test %v", ex.dir, ex.grants)
				exDir := filepath.Join(clone, "examples", ex.dir)
				runIn(t, exDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
				args := append([]string{"test", "-skip-build"}, ex.grants...)
				out := runIn(t, exDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, args...)
				for _, want := range ex.wants {
					if !strings.Contains(out, want) {
						t.Fatalf("examples/%s: aiisdk test must report %q:\n%s", ex.dir, want, out)
					}
				}
				if ex.dir == "logging" {
					if _, err := os.Stat(filepath.Join(clone, "examples", "logging", "dist", "pkg", "com.aiii.examples.logging-0.1.0", "install-root", "subscriptions.json")); err != nil {
						t.Fatalf("the logging package carries its subscriptions: %v", err)
					}
				}
			})
		}

		// ---- 8f. THE SMS ADAPTER: built from the clone; describe, an inbound webhook, the refusals. ----
		t.Run("twilio-sms", func(t *testing.T) {
			t.Parallel()
			stage(t, "examples/twilio-sms: aiisdk build, then aiisdk test with no grant")
			exDir := filepath.Join(clone, "examples", "twilio-sms")
			runIn(t, exDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
			out := runIn(t, exDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, "test", "-skip-build")
			for _, want := range []string{"PASS  case the adapter names its channel and receives by webhook", "PASS  case an inbound Twilio webhook becomes an arrival and a TwiML reply", "PASS  case a body that is not a Twilio message is refused", "PASS  case sending is denied without a grant", "PASS  case receive says this adapter is push", "RESULT: PASS"} {
				if !strings.Contains(out, want) {
					t.Fatalf("examples/twilio-sms: aiisdk test must report %q:\n%s", want, out)
				}
			}
			if _, err := os.Stat(filepath.Join(exDir, "dist", "pkg", "com.aiii.examples.twilio-sms-0.2.0", "install-root", "webhooks.json")); err != nil {
				t.Fatalf("the package carries the webhook declaration: %v", err)
			}
		})

		// ---- 8g. THE TELEGRAM ADAPTER: built from the clone; describe, the address and token refusals, the denials without a grant. ----
		t.Run("telegram", func(t *testing.T) {
			t.Parallel()
			stage(t, "examples/telegram: aiisdk build, then aiisdk test with no grant")
			exDir := filepath.Join(clone, "examples", "telegram")
			runIn(t, exDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
			out := runIn(t, exDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, "test", "-skip-build")
			for _, want := range []string{"PASS  case the adapter names its channel and receives by long poll within a budget", "PASS  case an address that is not a chat id or @username is refused before any call", "PASS  case sending without the bot_token handle is refused by name", "PASS  case sending is denied without a grant", "PASS  case receiving is denied without a grant, and dials nothing", "RESULT: PASS"} {
				if !strings.Contains(out, want) {
					t.Fatalf("examples/telegram: aiisdk test must report %q:\n%s", want, out)
				}
			}
		})

		// ---- 8h. THE CALENDAR CONNECTOR: built from the clone; the handle refusal, the denials without a grant, the writes waiting for the operator, the zone's rules in the guest. ----
		t.Run("google-calendar", func(t *testing.T) {
			t.Parallel()
			stage(t, "examples/google-calendar: aiisdk build, then aiisdk test with no grant")
			exDir := filepath.Join(clone, "examples", "google-calendar")
			runIn(t, exDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
			out := runIn(t, exDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, "test", "-skip-build")
			for _, want := range []string{"PASS  case today without the google handle is refused by name", "PASS  case today is denied without a grant, and dials nothing", "PASS  case upcoming is denied without a grant", "PASS  case a create the operator has not confirmed is refused before anything is asked", "PASS  case a create at a time the clock skips is refused by the zone's own rules, before any call", "PASS  case a confirmed create reaches the call, and is denied without a grant", "RESULT: PASS"} {
				if !strings.Contains(out, want) {
					t.Fatalf("examples/google-calendar: aiisdk test must report %q:\n%s", want, out)
				}
			}
		})

		// ---- 8k. THE GMAIL CONNECTOR: built from the clone; the handle refusal, the denials without a grant, a draft that needs no operator, the send waiting for the operator, the headers a card cannot show refused in the guest. ----
		t.Run("gmail", func(t *testing.T) {
			t.Parallel()
			stage(t, "examples/gmail: aiisdk build, then aiisdk test with no grant")
			exDir := filepath.Join(clone, "examples", "gmail")
			runIn(t, exDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
			out := runIn(t, exDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, "test", "-skip-build")
			for _, want := range []string{"PASS  case a search without the google handle is refused by name", "PASS  case a search is denied without a grant, and dials nothing", "PASS  case a draft whose subject smuggles a header after a line break is refused before any call", "PASS  case a draft needs no operator, reaches the call, and is denied without a grant", "PASS  case a send the operator has not confirmed is refused before anything is asked", "PASS  case a confirmed send with a second address inside one recipient is refused before any call", "PASS  case a confirmed send naming a bcc the card cannot show is refused before any call", "PASS  case a confirmed send saves its draft first, and is denied without a grant", "PASS  case a setup the operator has not confirmed is refused before anything is created", "PASS  case a Pub/Sub push is decoded by the wasm guest", "PASS  confirmation", "RESULT: PASS"} {
				if !strings.Contains(out, want) {
					t.Fatalf("examples/gmail: aiisdk test must report %q:\n%s", want, out)
				}
			}
		})

		// ---- 8e. THE DOCUMENT PLUGIN: built from the clone; the identity sandbox read into RING4, the private index, the refusals. ----
		t.Run("document-ingest", func(t *testing.T) {
			t.Parallel()
			stage(t, "examples/document-ingest: aiisdk build, then aiisdk test -grant kv -grant files=sample")
			exDir := filepath.Join(clone, "examples", "document-ingest")
			runIn(t, exDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
			out := runIn(t, exDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, "test", "-skip-build", "-grant", "kv", "-grant", "files=sample")
			for _, want := range []string{"PASS  case the identity's sandbox is listed", "PASS  case a file from the identity's sandbox is stored in chunks", "PASS  case recall finds the chunk about the ledger", "PASS  case a missing file is an honest failure", "PASS  case a path that leaves the sandbox is refused before anything is opened", "PASS  case an absolute path outside the sandbox is refused", "PASS  case a file ingested again in fewer chunks leaves none of the old ones behind", "PASS  case recall scans the one chunk the file now is", "PASS  case a chunk is cut by characters, never inside one", "RESULT: PASS"} {
				if !strings.Contains(out, want) {
					t.Fatalf("examples/document-ingest: aiisdk test must report %q:\n%s", want, out)
				}
			}
		})

		// ---- 8d. THE CONNECTOR: built from the clone; hermetic cases prove the verbs' honesty without the network. ----
		t.Run("github-issues", func(t *testing.T) {
			t.Parallel()
			stage(t, "examples/github-issues: aiisdk build, then aiisdk test with no grant (every call denied by name)")
			exDir := filepath.Join(clone, "examples", "github-issues")
			runIn(t, exDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
			out := runIn(t, exDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, "test", "-skip-build")
			for _, want := range []string{"PASS  case listing issues is denied without a grant", "PASS  case creating an issue is denied without a grant", "PASS  case streaming issues is denied without a grant", "PASS  case no repository set is an honest failure", "PASS  case an issue without a title is refused before any call", "PASS  confirmation", "RESULT: PASS"} {
				if !strings.Contains(out, want) {
					t.Fatalf("examples/github-issues: aiisdk test must report %q:\n%s", want, out)
				}
			}
		})

		// ---- 8c. THE MEMORY SLICE: both architectures, built from the clone, proven by aiisdk test. ----
		for _, ex := range []struct {
			dir    string
			grants []string
			also   []string // cases only this example has
		}{
			{"memory-embeddings", []string{"-grant", "kv", "-grant", "embeddings"}, []string{"PASS  case a memory stored under another model is passed over, and counted", "PASS  case and is found again under the model that stored it"}},
			{"memory-keywords", []string{"-grant", "kv"}, nil},
		} {
			t.Run(ex.dir, func(t *testing.T) {
				t.Parallel()
				exDir := filepath.Join(clone, "examples", ex.dir)
				stage(t, "examples/%s: aiisdk build, then aiisdk test %s", ex.dir, strings.Join(ex.grants, " "))
				runIn(t, exDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
				args := append([]string{"test", "-skip-build"}, ex.grants...)
				out := runIn(t, exDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, args...)
				for _, want := range []string{"PASS  case remember stores the first text", "PASS  case recall ranks the text about truth first", "PASS  case forget removes m:2", "PASS  case an empty query is refused", "PASS  case recall honors the operator's limit", "RESULT: PASS"} {
					if !strings.Contains(out, want) {
						t.Fatalf("examples/%s: aiisdk test must report %q:\n%s", ex.dir, want, out)
					}
				}
				for _, want := range ex.also {
					if !strings.Contains(out, want) {
						t.Fatalf("examples/%s: aiisdk test must report %q:\n%s", ex.dir, want, out)
					}
				}
			})
		}

		// ---- 8l. THE COMPAT PLUGIN: built from the clone; every operation answers its refusal as its own result. The host's compatibility suite proves the granted paths. ----
		t.Run("compat", func(t *testing.T) {
			t.Parallel()
			stage(t, "examples/compat: aiisdk build, then aiisdk test with no grant")
			exDir := filepath.Join(clone, "examples", "compat")
			runIn(t, exDir, []string{"TINYGO=" + tg, "GOPROXY=off"}, aiisdk, "build")
			out := runIn(t, exDir, []string{"AII_OS_BIN=" + binDir, "GOPROXY=off"}, aiisdk, "test", "-skip-build")
			for _, want := range []string{"PASS  case remember is denied without a grant, and says so by the host's code", "PASS  case recall is denied without a grant", "PASS  case interactions is denied by a stand-in that serves no history", "PASS  case overreach is denied, and returns the denial", "PASS  case spin is denied before it spins", "PASS  case a remember with no text is refused before any host call", "RESULT: PASS"} {
				if !strings.Contains(out, want) {
					t.Fatalf("examples/compat: aiisdk test must report %q:\n%s", want, out)
				}
			}
		})
	})
	if !examples {
		t.FailNow()
	}

	// ---- 9. THE T1 SIGNING PATH: devcert + sign -> VERIFIED T1. ----
	stage(t, "aiisdk devcert + sign, then aii plugin verify -certifier-key -trust-dir -> VERIFIED T1")
	proveT1(t, aiisdk, aii, pluginDir, pkg)

	// ---- 10. THE REVOCATION PATH: revoke -> TRUST_PAYLOAD_REVOKED. ----
	stage(t, "aiisdk revoke, then the same verify -> refused with TRUST_PAYLOAD_REVOKED")
	proveRevocation(t, aiisdk, aii, pluginDir, pkg)

	// ---- 11. The PASS line: name exactly what was proven. ----
	t.Log("\n" + strings.Repeat("-", 72))
	t.Logf("PASS  %s %s", pluginID, pluginVersion)
	t.Log("  cloned fresh from a standalone repo (no source-tree reference)")
	t.Log("  built the aiisdk CLI offline, and the guest wasm with TinyGo")
	t.Log("  packaged by the SDK's OWN canonical writer (imports nothing from aii-os)")
	t.Logf("  VERIFIED T0 by the independent host verifier (%s)", filepath.Base(aii))
	t.Logf("  RAN on %s: %s round-tripped, clean stdin-EOF exit", filepath.Base(worker), echoOp)
	t.Log("  the artifact's own account (aiii-plugin-describe) equals the packaged descriptor")
	t.Log("  aiisdk test PASSED on the scaffold against the distributed oracles (cases, hostile, exit)")
	t.Log("  the memory slice: memory-embeddings and memory-keywords built from the clone and proven by aiisdk test")
	t.Log("  the connector: github-issues built from the clone; its denials, settings and argument checks proven by aiisdk test without the network")
	t.Log("  the document plugin: document-ingest built from the clone; the identity's sandbox ingested into RING4 and recalled, the refusals proven by aiisdk test")
	t.Log("  the SMS adapter: twilio-sms built from the clone with its webhook declaration packaged; describe, the inbound webhook and the refusals proven by aiisdk test")
	t.Log("  the Telegram adapter: telegram built from the clone; describe, the address and token refusals and the denials without a grant proven by aiisdk test")
	t.Log("  the calendar connector: google-calendar built from the clone; the handle refusal, the denials without a grant, the writes waiting for the operator and the zone's rules in the guest proven by aiisdk test")
	t.Log("  the Gmail connector: gmail built from the clone; the handle refusal, the denials without a grant, a draft needing no operator, the send waiting for the operator and the headers a card cannot show refused in the guest, proven by aiisdk test")
	t.Log("  the compat plugin: compat built from the clone; each of its five operations answered its refusal as its own result, proven by aiisdk test with no grant")
	t.Log("  events and dynamic tools: logging built from the clone with its subscriptions packaged and its events delivered and read back; mcp-connector publishing a server's tools from a list")
	t.Log("  the native matrix: native-skel built for linux/x86_64, macos/arm64 and windows/x86_64, packaged as one native variant each, refused by the host below T3 with WASM_BASELINE_MISSING — the platform signature is the only door for native code")
	t.Log("  the speech engine shape: voice-skel built for the same three platforms, packaged with the seven session controls as its declared operations, refused below T3 the same way")
	t.Log("  the catalog entry: aiisdk publish emitted a portable catalog entry with the package hash and size, ready for aiios-plugins.md")
	t.Log("  VERIFIED T1 after a devcert-rooted dual-PQ signature + empty revocation snapshot")
	t.Log("  REVOKED and refused: aiisdk revoke flipped the same verify to TRUST_PAYLOAD_REVOKED")
	t.Log(strings.Repeat("-", 72))
}

// proveRunsAndEchoes drives the worker on the built module and asserts
// the admission banner, an echo round-trip, and a clean EOF exit.
func proveRunsAndEchoes(t *testing.T, worker, wasm string) {
	t.Helper()
	w, err := workerdrive.Start(worker, wasm, 60*time.Second)
	if err != nil {
		t.Fatalf("start worker: %v", err)
	}
	if !strings.Contains(w.Banner(), "bbb_protocol_version=2") {
		t.Fatalf("ready banner must pin bbb_protocol_version=2: %s", w.Banner())
	}
	t.Logf("worker ready: %s", strings.TrimSpace(w.Banner()))

	args := json.RawMessage(`{"name":"acceptance","n":7}`)
	reply, err := w.Invoke(`"accept-echo-1"`, echoOp, args, 30*time.Second)
	if err != nil {
		t.Fatalf("invoke %s: %v", echoOp, err)
	}
	if reply.IsError {
		t.Fatalf("%s returned a JSON-RPC error envelope: %s", echoOp, reply.Error)
	}
	if reply.Status != "succeeded" {
		t.Fatalf("%s status = %q (want succeeded): %s", echoOp, reply.Status, reply.Raw)
	}
	if string(reply.OperationResult) != string(args) {
		t.Fatalf("%s did not round-trip its arguments:\n got %s\nwant %s", echoOp, reply.OperationResult, args)
	}
	t.Logf("%s -> operation_result %s (round-trip verbatim)", echoOp, reply.OperationResult)

	if err := w.Close(15 * time.Second); err != nil {
		t.Fatalf("clean shutdown: %v", err)
	}
	t.Log("worker exited 0 on stdin EOF")
}

// proveT1 mints the dev chain, signs the exact release, and asserts the
// host verifier reports VERIFIED T1 against the pinned dev root plus
// the devcert-minted revocation snapshot (-trust-dir .keys).
func proveT1(t *testing.T, aiisdk, aii, pluginDir, pkg string) {
	t.Helper()
	// devcert and sign are slow (SLH-DSA keygen + signing); give them
	// room. They run in the plugin dir (os.Getwd), writing .keys/.
	runIn(t, pluginDir, nil, aiisdk, "devcert")
	runIn(t, pluginDir, nil, aiisdk, "sign")

	keys := filepath.Join(pluginDir, ".keys")
	root := filepath.Join(keys, "certifier-root.pub.json")
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("devcert produced no pin file at %s: %v", root, err)
	}
	status := filepath.Join(keys, "aiii_plugin_publisher_certifier_status.json")
	if _, err := os.Stat(status); err != nil {
		t.Fatalf("devcert produced no empty revocation snapshot at %s: %v", status, err)
	}
	wantT1 := fmt.Sprintf("VERIFIED T1 %s %s", pluginID, pluginVersion)
	out := runIn(t, pluginDir, nil, aii, "plugin", "verify", "-certifier-key", root, "-trust-dir", keys, pkg)
	if !strings.Contains(out, wantT1) {
		t.Fatalf("host verify did not report %q against the pinned dev root:\n%s", wantT1, out)
	}
	t.Logf("host verifier (dev root pinned): %s", firstLine(out))

	// Fail-closed cross-check: WITHOUT the pinned root, the same package
	// carries publisher evidence that must not soft-pass — the verifier
	// rejects it rather than silently dropping to T0 (unverifiable is not
	// unsigned, verify.go / trust.go). A green line here would mean the
	// signature was being ignored.
	cmd := exec.Command(aii, "plugin", "verify", pkg)
	cmd.Dir = pluginDir
	if unpinned, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("a signed package verified with NO pinned root — publisher evidence soft-passed:\n%s", unpinned)
	}
	t.Log("without the pinned root, the signed package is refused (evidence never soft-passes)")

	// Fail-closed cross-check two: root pinned but NO snapshot (-trust-
	// dir omitted) = the certifier tier is unavailable; signed evidence
	// rejects rather than downgrading.
	cmd = exec.Command(aii, "plugin", "verify", "-certifier-key", root, pkg)
	cmd.Dir = pluginDir
	if noStatus, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("a signed package verified without the revocation snapshot — the tier did not fail closed:\n%s", noStatus)
	} else if !strings.Contains(string(noStatus), "REVOCATION_STATUS_UNAVAILABLE") {
		t.Fatalf("snapshot-less refusal did not name REVOCATION_STATUS_UNAVAILABLE:\n%s", noStatus)
	}
	t.Log("without the revocation snapshot, the signed package is refused (tier fails closed)")
}

// proveRevocation revokes the just-verified release in the dev
// snapshot and asserts the SAME package + SAME roots now refuse with
// TRUST_PAYLOAD_REVOKED — enforcement end to end, and the epoch bump
// riding along.
func proveRevocation(t *testing.T, aiisdk, aii, pluginDir, pkg string) {
	t.Helper()
	out := runIn(t, pluginDir, nil, aiisdk, "revoke")
	if !strings.Contains(out, "revoked plugin.manifest sha256:") {
		t.Fatalf("revoke did not report the revoked release:\n%s", out)
	}
	t.Logf("revoke: %s", firstLine(out))

	keys := filepath.Join(pluginDir, ".keys")
	root := filepath.Join(keys, "certifier-root.pub.json")
	cmd := exec.Command(aii, "plugin", "verify", "-certifier-key", root, "-trust-dir", keys, pkg)
	cmd.Dir = pluginDir
	revokedOut, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a REVOKED release still verified:\n%s", revokedOut)
	}
	if !strings.Contains(string(revokedOut), "TRUST_PAYLOAD_REVOKED") {
		t.Fatalf("revoked refusal did not name TRUST_PAYLOAD_REVOKED:\n%s", revokedOut)
	}
	t.Log("after 'aiisdk revoke', the same package + same roots refuse with TRUST_PAYLOAD_REVOKED")

	// Idempotence: revoking again is a no-op at the same epoch.
	again := runIn(t, pluginDir, nil, aiisdk, "revoke")
	if !strings.Contains(again, "already revoked") {
		t.Fatalf("second revoke was not idempotent:\n%s", again)
	}
}

// assertNoSourceReference greps the clone's Go/build files for the
// absolute source path — the clone must stand on its own.
func assertNoSourceReference(t *testing.T, clone, src string) {
	t.Helper()
	abs, err := filepath.Abs(src)
	if err != nil {
		return
	}
	// A path reference could only hide in a tracked text file; walk the
	// clone (minus .git) and fail if the source path appears.
	_ = filepath.Walk(clone, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() && info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		if strings.Contains(string(data), abs) {
			t.Fatalf("clone file %s references the source tree path %s", path, abs)
		}
		return nil
	})
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
