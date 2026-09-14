package aiiosdk

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
)

// .
// .
// .
// .
// .

// .
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

// .
// .
func TestReadinessIsAnnouncedBeforeTheFirstFrame(t *testing.T) {
	p := New("org.example.voice").Handle("noop", func(Call) (any, error) { return "ok", nil })
	host, stderr, _ := servePlugin(t, p)

	host.send(t, `{"jsonrpc":"2.0","id":1,"method":"invoke.call","params":{"operation":"noop","arguments":{}}}`)
	host.recv(t)

	if !strings.Contains(stderr.String(), "child-ready") {
		t.Fatalf("no readiness mark on stderr; the supervisor would wait forever: %q", stderr.String())
	}
}

// .
// .
// .
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

	// .
	// .
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

// .
// .
// .
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

// .
// .
func TestStdinCloseEndsTheSessionCleanly(t *testing.T) {
	p := New("org.example.voice").Handle("noop", func(Call) (any, error) { return "ok", nil })
	host, _, await := servePlugin(t, p)
	host.toPlugin.Close()
	if err := await(); err != nil {
		t.Fatalf("a closed stdin was reported as an error: %v", err)
	}
}

func TestReadyLineCarriesTheThreeFields(t *testing.T) {
	line := ReadyLine("child-ready", ReadyReport{ModelsLoaded: 4, Accelerator: "mlx", ProbeMS: 38})
	if line != "child-ready event=ready models_loaded=4 accelerator=mlx probe_ms=38" {
		t.Fatalf("ready line: %q", line)
	}
}

// .
// .
// .
// .
// .
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
