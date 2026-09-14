//go:build e2e

package e2e

// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .
// .

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func findTinygo(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("TINYGO"); v != "" {
		return v
	}
	if p, err := exec.LookPath("tinygo"); err == nil {
		return p
	}
	t.Skip("e2e: tinygo not found (set TINYGO); skipping — the guest build needs it")
	return ""
}

func findSibling(t *testing.T) string {
	t.Helper()
	candidates := []string{os.Getenv("AII_OS_DIR"), filepath.Join(repoRoot(t), "..", "aii-os")}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(filepath.Join(c, "cmd", "aii-plugin-worker", "main.go")); err == nil {
			return c
		}
	}
	t.Skip("e2e: no sibling aii-os checkout (set AII_OS_DIR); skipping — independence means the oracle is optional, never vendored")
	return ""
}

func buildExample(t *testing.T) string {
	t.Helper()
	tinygo := findTinygo(t)
	dir := filepath.Join(repoRoot(t), "examples", "memory-skel")
	cmd := exec.Command("/bin/sh", "./build.sh")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "TINYGO="+tinygo)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build.sh failed: %v\n%s", err, out)
	}
	return filepath.Join(dir, "memory-skel.wasm")
}

func buildWorker(t *testing.T) string {
	t.Helper()
	sibling := findSibling(t)
	goDir := os.Getenv("AII_OS_GO")
	if goDir == "" {
		if p, err := exec.LookPath("go"); err == nil {
			goDir = filepath.Dir(p)
		}
	}
	goBin := filepath.Join(goDir, "go")
	if _, err := os.Stat(goBin); err != nil {
		t.Skipf("e2e: the host's Go toolchain was not found (set AII_OS_GO to its bin directory)")
	}
	tools := filepath.Join(repoRoot(t), ".tools")
	if err := os.MkdirAll(tools, 0o755); err != nil {
		t.Fatalf("mkdir .tools: %v", err)
	}
	worker := filepath.Join(tools, "aii-plugin-worker")
	cmd := exec.Command(goBin, "build", "-buildvcs=false", "-o", worker, "./cmd/aii-plugin-worker")
	cmd.Dir = sibling
	// .
	// .
	// .
	cmd.Env = append(os.Environ(),
		"PATH="+goDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GOTOOLCHAIN=local",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build worker oracle: %v\n%s", err, out)
	}
	return worker
}

// .
type oracle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.Reader
	banner string
	stderr *bytes.Buffer
}

func startOracle(t *testing.T, worker, module string) *oracle {
	t.Helper()
	cmd := exec.Command(worker, module)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout: %v", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		t.Fatalf("stderr: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	o := &oracle{cmd: cmd, stdin: stdin, stdout: stdout, stderr: &bytes.Buffer{}}

	// .
	bannerCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderrPipe)
		for sc.Scan() {
			line := sc.Text()
			o.stderr.WriteString(line + "\n")
			if strings.Contains(line, "event=ready") {
				select {
				case bannerCh <- line:
				default:
				}
			}
		}
	}()
	select {
	case o.banner = <-bannerCh:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("no ready banner within 30s; stderr so far:\n%s", o.stderr.String())
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_, _ = cmd.Process.Wait()
	})
	return o
}

// .
// .
func (o *oracle) roundTrip(t *testing.T, frame []byte) []byte {
	t.Helper()
	if err := sdk.WriteFrame(o.stdin, frame, sdk.MaxControlFrameBytes); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	type res struct {
		payload []byte
		err     error
	}
	ch := make(chan res, 1)
	go func() {
		p, err := sdk.ReadFrame(o.stdout, sdk.MaxControlFrameBytes)
		ch <- res{p, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("read frame: %v\nworker stderr:\n%s", r.err, o.stderr.String())
		}
		return r.payload
	case <-time.After(30 * time.Second):
		t.Fatalf("no response frame within 30s; worker stderr:\n%s", o.stderr.String())
		return nil
	}
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   json.RawMessage `json:"error"`
}

type invokeResult struct {
	Status          string          `json:"status"`
	OperationResult json.RawMessage `json:"operation_result"`
	Reason          string          `json:"reason"`
	ReasonCode      string          `json:"reasonCode"`
}

// .
// .
func decodeResponse(t *testing.T, payload []byte, wantIDRaw string) (invokeResult, json.RawMessage) {
	t.Helper()
	var members map[string]json.RawMessage
	if err := json.Unmarshal(payload, &members); err != nil {
		t.Fatalf("response is not an object: %s", payload)
	}
	if string(members["jsonrpc"]) != `"2.0"` {
		t.Fatalf("jsonrpc: %s", payload)
	}
	if _, has := members["method"]; has {
		t.Fatalf("a response must carry no method member: %s", payload)
	}
	if got := string(members["id"]); got != wantIDRaw {
		t.Fatalf("id echo not byte-form verbatim: got %s want %s", got, wantIDRaw)
	}
	_, hasResult := members["result"]
	_, hasError := members["error"]
	if hasResult == hasError {
		t.Fatalf("exactly one of result|error: %s", payload)
	}
	if hasError {
		t.Fatalf("unexpected error response: %s", payload)
	}
	var ir invokeResult
	if err := json.Unmarshal(members["result"], &ir); err != nil {
		t.Fatalf("result: %v in %s", err, payload)
	}
	return ir, members["result"]
}

func TestMemorySkelAgainstWorkerOracle(t *testing.T) {
	worker := buildWorker(t)
	module := buildExample(t)
	o := startOracle(t, worker, module)

	// .
	// .
	// .
	if !strings.Contains(o.banner, "bbb_protocol_version=2") {
		t.Fatalf("banner must pin bbb_protocol_version=2: %s", o.banner)
	}
	if !strings.Contains(o.banner, "artifact_class=core-module") {
		t.Fatalf("TinyGo emits a raw core module: %s", o.banner)
	}

	t.Run("focus.echo round-trips the arguments", func(t *testing.T) {
		args := `{"hello":"world","n":7}`
		resp := o.roundTrip(t, []byte(`{"jsonrpc":"2.0","id":"e2e-1","method":"invoke.call","params":{"operation":"focus.echo","arguments":`+args+`}}`))
		ir, _ := decodeResponse(t, resp, `"e2e-1"`)
		if ir.Status != "succeeded" {
			t.Fatalf("status: %+v (%s)", ir, resp)
		}
		if string(ir.OperationResult) != args {
			t.Fatalf("operation_result must carry the arguments verbatim:\n got %s\nwant %s", ir.OperationResult, args)
		}
	})

	t.Run("focus.set reports the deny-all wall gracefully", func(t *testing.T) {
		resp := o.roundTrip(t, []byte(`{"jsonrpc":"2.0","id":42,"method":"invoke.call","params":{"operation":"focus.set","arguments":{"value":"remember-me"}}}`))
		ir, _ := decodeResponse(t, resp, "42")
		if ir.Status != "succeeded" {
			t.Fatalf("the denial must be DATA, not a failed invoke: %s", resp)
		}
		var report struct {
			Stored     bool   `json:"stored"`
			Denied     bool   `json:"denied"`
			ReasonCode string `json:"reasonCode"`
		}
		if err := json.Unmarshal(ir.OperationResult, &report); err != nil {
			t.Fatalf("report: %v in %s", err, ir.OperationResult)
		}
		if report.Stored || !report.Denied || report.ReasonCode != "POLICY_DENY" {
			// .
			// .
			// .
			t.Fatalf("want the typed POLICY_DENY report, got %s", ir.OperationResult)
		}
	})

	t.Run("focus.get reports the same wall", func(t *testing.T) {
		resp := o.roundTrip(t, []byte(`{"jsonrpc":"2.0","id":"e2e-3","method":"invoke.call","params":{"operation":"focus.get"}}`))
		ir, _ := decodeResponse(t, resp, `"e2e-3"`)
		if ir.Status != "succeeded" || !strings.Contains(string(ir.OperationResult), `"denied":true`) {
			t.Fatalf("got %s", resp)
		}
	})

	t.Run("unknown operation is the audited failed result", func(t *testing.T) {
		resp := o.roundTrip(t, []byte(`{"jsonrpc":"2.0","id":"e2e-4","method":"invoke.call","params":{"operation":"focus.nope"}}`))
		var members map[string]json.RawMessage
		if err := json.Unmarshal(resp, &members); err != nil {
			t.Fatalf("resp: %v", err)
		}
		var ir invokeResult
		if err := json.Unmarshal(members["result"], &ir); err != nil {
			t.Fatalf("result: %v", err)
		}
		if ir.Status != "failed" || ir.ReasonCode != "OPERATION_NOT_FOUND" {
			t.Fatalf("got %s", resp)
		}
	})

	t.Run("clean shutdown on stdin EOF", func(t *testing.T) {
		_ = o.stdin.Close()
		done := make(chan error, 1)
		go func() { done <- o.cmd.Wait() }()
		select {
		case err := <-done:
			var ee *exec.ExitError
			if err != nil && (!errors.As(err, &ee) || ee.ExitCode() != 0) {
				t.Fatalf("worker exit: %v\nstderr:\n%s", err, o.stderr.String())
			}
		case <-time.After(15 * time.Second):
			_ = o.cmd.Process.Kill()
			t.Fatalf("worker did not exit on stdin EOF")
		}
	})
}

// .
// .
// .
func TestVectorsMatchSibling(t *testing.T) {
	sibling := findSibling(t)
	for _, name := range []string{"json_domain.json", "framing.json"} {
		ours, err := os.ReadFile(filepath.Join(repoRoot(t), "vectors", name))
		if err != nil {
			t.Fatalf("read our copy of %s: %v", name, err)
		}
		theirs, err := os.ReadFile(filepath.Join(sibling, "spec", "bbb", "vectors", name))
		if err != nil {
			t.Fatalf("read sibling %s: %v", name, err)
		}
		if !bytes.Equal(ours, theirs) {
			t.Fatalf("vectors/%s drifted from the sibling's spec/bbb/vectors/%s — re-copy and update vectors/README.md", name, name)
		}
	}
}
