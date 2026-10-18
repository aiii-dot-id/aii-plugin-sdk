package aiiosdk

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"testing"
)

// The native lane: a plugin that is a process, not a module. The host's
// supervisor execs it and speaks framed BBB over stdio — 4-byte
// big-endian length, JSON payload — and until Serve existed the SDK
// could not write one, so a native T3 author hand-rolled the framing,
// the loop, and the nested-hostcall protocol.

// hostSide is the supervisor's half, written to the same wire.
type hostSide struct {
	toPlugin   *io.PipeWriter
	fromPlugin *io.PipeReader
}

func (h *hostSide) send(t *testing.T, frame string) {
	t.Helper()
	if err := WriteFrame(h.toPlugin, []byte(frame), MaxControlFrameBytes); err != nil {
		t.Fatalf("host write: %v", err)
	}
}

func (h *hostSide) recv(t *testing.T) map[string]json.RawMessage {
	t.Helper()
	frame, err := ReadFrame(h.fromPlugin, MaxControlFrameBytes)
	if err != nil {
		t.Fatalf("host read: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(frame, &m); err != nil {
		t.Fatalf("host got a non-object frame: %s", frame)
	}
	return m
}

// servePlugin runs one plugin and returns the host's half, its stderr,
// and an await that reports how Serve ended.
//
// AWAIT IS IDEMPOTENT, and both halves of that matter. Cleanup must wait
// for Serve to RETURN, not merely for stdin to close: Serve releases the
// process-wide native transport on its way out, and returning early left
// it claimed — so the next test's Serve refused, its plugin never read,
// and its first write blocked forever on a pipe with no reader. A
// deadlock that only appears when the package runs as a suite.
//
// And a test that awaits the result itself must not leave cleanup
// waiting on a channel already drained, which is the same hang wearing
// the other hat.
func servePlugin(t *testing.T, p *Plugin) (*hostSide, *bytes.Buffer, func() error) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	var stderr bytes.Buffer
	done := make(chan error, 1)
	go func() { done <- p.serve(inR, outW, &stderr, "child-ready") }()

	var once sync.Once
	var serveErr error
	await := func() error {
		once.Do(func() { serveErr = <-done })
		return serveErr
	}
	t.Cleanup(func() { inW.Close(); await() })
	return &hostSide{toPlugin: inW, fromPlugin: outR}, &stderr, await
}

func TestANativePluginAnswersTheHost(t *testing.T) {
	p := New("org.example.voice").Handle("describe", func(c Call) (any, error) {
		return map[string]any{"channel": "voice"}, nil
	})
	host, _, _ := servePlugin(t, p)

	host.send(t, `{"jsonrpc":"2.0","id":7,"method":"invoke.call","params":{"operation":"describe","arguments":{}}}`)
	reply := host.recv(t)

	if string(reply["id"]) != "7" {
		t.Fatalf("the reply does not answer the request it was sent: id=%s", reply["id"])
	}
	if _, isErr := reply["error"]; isErr {
		t.Fatalf("a registered operation returned an error: %s", reply["error"])
	}
	if !strings.Contains(string(reply["result"]), "voice") {
		t.Fatalf("the handler's value did not reach the host: %s", reply["result"])
	}
}

// The supervisor will not send work until it sees the mark on stderr —
// which is how a plugin that loads models says "after, not before".
func TestReadinessIsAnnouncedBeforeTheFirstFrame(t *testing.T) {
	p := New("org.example.voice").Handle("noop", func(Call) (any, error) { return "ok", nil })
	host, stderr, _ := servePlugin(t, p)

	host.send(t, `{"jsonrpc":"2.0","id":1,"method":"invoke.call","params":{"operation":"noop","arguments":{}}}`)
	host.recv(t)

	if !strings.Contains(stderr.String(), "child-ready") {
		t.Fatalf("no readiness mark on stderr; the supervisor would wait forever: %q", stderr.String())
	}
}

// The nested hostcall: a handler calls the host WHILE the host waits for
// that handler. Getting this wrong is the deadlock the control-pair
// discipline exists to prevent.
func TestAHandlerCanCallTheHostMidRequest(t *testing.T) {
	p := New("org.example.voice").Handle("ask", func(Call) (any, error) {
		res, err := InvokeCall("kv.get", nil, map[string]any{"key": "model_path"})
		if err != nil {
			return nil, err
		}
		return map[string]any{"saw": res.Status}, nil
	})
	host, _, _ := servePlugin(t, p)

	host.send(t, `{"jsonrpc":"2.0","id":11,"method":"invoke.call","params":{"operation":"ask","arguments":{}}}`)

	// The plugin's upstream request arrives while the host's own request
	// is still outstanding — the whole point.
	up := host.recv(t)
	if string(up["method"]) != `"invoke.call"` {
		t.Fatalf("expected an upstream invoke.call, got method=%s", up["method"])
	}
	if !strings.Contains(string(up["params"]), "kv.get") {
		t.Fatalf("the operation did not travel: %s", up["params"])
	}
	host.send(t, `{"jsonrpc":"2.0","id":`+string(up["id"])+`,"result":{"status":"succeeded","value":{"value":"/models/stt"}}}`)

	reply := host.recv(t)
	if string(reply["id"]) != "11" {
		t.Fatalf("the outer request was answered with id=%s", reply["id"])
	}
	if !strings.Contains(string(reply["result"]), "succeeded") {
		t.Fatalf("the hostcall result did not reach the handler: %s", reply["result"])
	}
}

// A reply carrying someone else's id means the stream desynchronized.
// Answering the caller with it would be worse than failing, because the
// caller would believe it.
func TestAMismatchedHostcallReplyIsRefused(t *testing.T) {
	var got error
	p := New("org.example.voice").Handle("ask", func(Call) (any, error) {
		_, got = InvokeCall("kv.get", nil, map[string]any{"key": "k"})
		return "done", nil
	})
	host, _, _ := servePlugin(t, p)

	host.send(t, `{"jsonrpc":"2.0","id":3,"method":"invoke.call","params":{"operation":"ask","arguments":{}}}`)
	up := host.recv(t)
	_ = up
	host.send(t, `{"jsonrpc":"2.0","id":9999,"result":{"status":"succeeded"}}`)
	host.recv(t)

	if got == nil {
		t.Fatal("a reply with the wrong id was accepted as this call's result")
	}
	if !strings.Contains(got.Error(), "does not match") {
		t.Fatalf("the refusal does not say what went wrong: %v", got)
	}
}

// Closing stdin is the host saying the session is over. That is an
// ordinary end, not a failure.
func TestStdinCloseEndsTheSessionCleanly(t *testing.T) {
	p := New("org.example.voice").Handle("noop", func(Call) (any, error) { return "ok", nil })
	host, _, await := servePlugin(t, p)
	host.toPlugin.Close()
	if err := await(); err != nil {
		t.Fatalf("a closed stdin was reported as an error: %v", err)
	}
}

// The three fields the host requires, then the kit that built the
// plugin. This test binary's main module is the kit itself, and go test
// records its version as "(devel)": the value is read, not written in.
func TestReadyLineCarriesTheThreeFieldsAndTheKit(t *testing.T) {
	line := ReadyLine("child-ready", ReadyReport{ModelsLoaded: 4, Accelerator: "mlx", ProbeMS: 38})
	if line != "child-ready event=ready models_loaded=4 accelerator=mlx probe_ms=38 sdk=(devel)" {
		t.Fatalf("ready line: %q", line)
	}
}

// WHICH KIT BUILT THE PLUGIN is what the plugin's own build recorded
// for the kit: a release's tag, a pseudo-version naming a commit, the
// kit's own version when the plugin is built inside the kit, "(devel)"
// for a checkout wired in by replace, the target of a replacement by
// another module — and "unknown" where the record holds nothing usable.
// The records below are the shapes go1.27 writes (go version -m).
func TestTheReadyLineNamesTheKitThePluginsBuildRecorded(t *testing.T) {
	const pseudo = "v0.1.15-0.20000304050607-123456789abc"
	plugin := func(deps ...*debug.Module) *debug.BuildInfo {
		return &debug.BuildInfo{Path: "example.com/engine", Main: debug.Module{Path: "example.com/engine", Version: "(devel)"},
			Deps: append([]*debug.Module{{Path: "golang.org/x/sys", Version: "v0.30.0"}}, deps...)}
	}
	kit := func(version string, replace *debug.Module) *debug.Module {
		return &debug.Module{Path: kitModule, Version: version, Replace: replace}
	}
	for _, c := range []struct {
		name string
		bi   *debug.BuildInfo
		ok   bool
		want string
	}{
		{"required at a release", plugin(kit("v0.1.14", nil)), true, "v0.1.14"},
		{"required at a commit", plugin(kit(pseudo, nil)), true, pseudo},
		{"a checkout wired in by replace", plugin(kit("v0.0.0-00010101000000-000000000000",
			&debug.Module{Path: "/example/A Path/aii-plugin-sdk", Version: "(devel)"})), true, "(devel)"},
		{"replaced by another module", plugin(kit("v0.1.14",
			&debug.Module{Path: "example.com/fork/aii-plugin-sdk", Version: "v0.1.14-fork.1"})), true, "example.com/fork/aii-plugin-sdk@v0.1.14-fork.1"},
		{"built inside the kit, stamped from its checkout", &debug.BuildInfo{Main: debug.Module{Path: kitModule, Version: pseudo + "+dirty"}}, true, pseudo + "+dirty"},
		{"built inside the kit by go run", &debug.BuildInfo{Main: debug.Module{Path: kitModule, Version: "(devel)"}}, true, "(devel)"},
		{"the record does not name the kit", plugin(), true, "unknown"},
		{"a version with a space", plugin(kit("v0.1.14 x", nil)), true, "unknown"},
		{"an empty version", plugin(kit("", nil)), true, "unknown"},
		{"no build information", nil, false, "unknown"},
	} {
		if got := kitVersion(c.bi, c.ok); got != c.want {
			t.Errorf("%s: sdk=%s, want sdk=%s", c.name, got, c.want)
		}
	}
}

// The packager runs `go run .` with AIISDK_DESCRIBE=1 and reads stdout
// as the descriptor surface. A native main ends in Serve or ServeReady;
// under that variable both print the descriptors and return without
// claiming the transport or announcing readiness — a native skeleton
// once printed nothing, and this is the door.
func TestServeUnderThePackagerPrintsTheDescriptorsAndReturns(t *testing.T) {
	t.Setenv(DescribeEnv, "1")
	p := New("org.example.describe-door")
	p.Handle("echo", func(c Call) (any, error) { return c.Args(), nil })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	serr := p.ServeReady("never-printed", ReadyReport{ModelsLoaded: 1, Accelerator: "cpu", ProbeMS: 1})
	os.Stdout = old
	w.Close()
	got, _ := io.ReadAll(r)
	if serr != nil {
		t.Fatalf("ServeReady under %s=1 answers with the descriptors: %v", DescribeEnv, serr)
	}
	want, err := p.DescriptorsJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("stdout must be DescriptorsJSON exactly:\n%s", got)
	}
	if nativeTransport.Load() != nil {
		t.Fatal("the describe door claims no transport")
	}
}
