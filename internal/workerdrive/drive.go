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
package workerdrive

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

// .
// .
// .
// .
type Handler func(method string, params json.RawMessage) (result, errObj json.RawMessage)

// .
type Worker struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.Reader
	stderr  *bytes.Buffer
	banner  string
	handler Handler
}

// .
// .
// .
// .
func Start(workerBin, module string, timeout time.Duration) (*Worker, error) {
	return launch(workerBin, []string{module}, timeout, nil)
}

// .
// .
// .
// .
// .
func StartForward(workerBin string, prefix []string, module string, timeout time.Duration, handler Handler) (*Worker, error) {
	args := append(append([]string{}, prefix...), "-forward", module)
	return launch(workerBin, args, timeout, handler)
}

func launch(workerBin string, args []string, timeout time.Duration, handler Handler) (*Worker, error) {
	cmd := exec.Command(workerBin, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start worker: %w", err)
	}
	w := &Worker{cmd: cmd, stdin: stdin, stdout: stdout, stderr: &bytes.Buffer{}, handler: handler}

	bannerCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderrPipe)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			line := sc.Text()
			w.stderr.WriteString(line + "\n")
			if strings.Contains(line, "event=ready") {
				select {
				case bannerCh <- line:
				default:
				}
			}
		}
	}()

	select {
	case w.banner = <-bannerCh:
		return w, nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return nil, fmt.Errorf("no ready banner within %s; worker stderr:\n%s", timeout, w.stderr.String())
	}
}

// .
func (w *Worker) Banner() string { return w.banner }

// .
func (w *Worker) Stderr() string { return w.stderr.String() }

// .
// .
// .
type Reply struct {
	Raw             []byte
	IDRaw           string
	IsError         bool
	Error           json.RawMessage
	Status          string
	OperationResult json.RawMessage
	Reason          string
	ReasonCode      string
}

// .
// .
// .
// .
// .
func (w *Worker) Invoke(idRaw, operation string, argsJSON json.RawMessage, timeout time.Duration) (*Reply, error) {
	var req bytes.Buffer
	req.WriteString(`{"jsonrpc":"2.0","id":`)
	req.WriteString(idRaw)
	req.WriteString(`,"method":"invoke.call","params":{"operation":`)
	encoded, _ := json.Marshal(operation)
	req.Write(encoded)
	if len(argsJSON) > 0 {
		req.WriteString(`,"arguments":`)
		req.Write(argsJSON)
	}
	req.WriteString(`}}`)

	if err := sdk.WriteFrame(w.stdin, req.Bytes(), sdk.MaxControlFrameBytes); err != nil {
		return nil, fmt.Errorf("write request frame: %w (worker stderr:\n%s)", err, w.stderr.String())
	}

	// .
	// .
	// .
	// .
	deadline := time.Now().Add(timeout)
	for {
		payload, err := w.readFrame(time.Until(deadline))
		if err != nil {
			return nil, err
		}
		req, isRequest := upstreamRequest(payload)
		if !isRequest {
			return decodeReply(payload, idRaw)
		}
		if err := w.answer(req); err != nil {
			return nil, err
		}
	}
}

// .
// .
func (w *Worker) readFrame(timeout time.Duration) ([]byte, error) {
	type result struct {
		payload []byte
		err     error
	}
	ch := make(chan result, 1)
	go func() {
		p, err := sdk.ReadFrame(w.stdout, sdk.MaxControlFrameBytes)
		ch <- result{p, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("read frame: %w (worker stderr:\n%s)", r.err, w.stderr.String())
		}
		return r.payload, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("no frame within %s (worker stderr:\n%s)", timeout, w.stderr.String())
	}
}

// .
type upstream struct {
	idRaw  string
	method string
	params json.RawMessage
}

func upstreamRequest(payload []byte) (upstream, bool) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(payload, &members); err != nil {
		return upstream{}, false
	}
	methodRaw, has := members["method"]
	if !has {
		return upstream{}, false
	}
	var method string
	_ = json.Unmarshal(methodRaw, &method)
	return upstream{idRaw: string(members["id"]), method: method, params: members["params"]}, true
}

// .
// .
// .
func (w *Worker) answer(req upstream) error {
	var result, errObj json.RawMessage
	if w.handler == nil {
		errObj = json.RawMessage(`{"code":-32000,"message":"no host attached to this driver; call denied","data":{"reasonCode":"POLICY_DENY"}}`)
	} else {
		result, errObj = w.handler(req.method, req.params)
	}
	var resp bytes.Buffer
	resp.WriteString(`{"jsonrpc":"2.0","id":`)
	resp.WriteString(req.idRaw)
	if len(errObj) > 0 {
		resp.WriteString(`,"error":`)
		resp.Write(errObj)
	} else {
		if len(result) == 0 {
			result = json.RawMessage("null")
		}
		resp.WriteString(`,"result":`)
		resp.Write(result)
	}
	resp.WriteString(`}`)
	return sdk.WriteFrame(w.stdin, resp.Bytes(), sdk.MaxControlFrameBytes)
}

// .
// .
func decodeReply(payload []byte, wantIDRaw string) (*Reply, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(payload, &members); err != nil {
		return nil, fmt.Errorf("response is not a JSON object: %s", payload)
	}
	if string(members["jsonrpc"]) != `"2.0"` {
		return nil, fmt.Errorf("response jsonrpc is not \"2.0\": %s", payload)
	}
	if _, has := members["method"]; has {
		return nil, fmt.Errorf("a response must carry no method member: %s", payload)
	}
	if got := string(members["id"]); got != wantIDRaw {
		return nil, fmt.Errorf("id echo not byte-form verbatim: got %s want %s", got, wantIDRaw)
	}
	_, hasResult := members["result"]
	_, hasError := members["error"]
	if hasResult == hasError {
		return nil, fmt.Errorf("response must carry exactly one of result|error: %s", payload)
	}

	reply := &Reply{Raw: payload, IDRaw: string(members["id"])}
	if hasError {
		reply.IsError = true
		reply.Error = members["error"]
		return reply, nil
	}
	var ir struct {
		Status          string          `json:"status"`
		OperationResult json.RawMessage `json:"operation_result"`
		Reason          string          `json:"reason"`
		ReasonCode      string          `json:"reasonCode"`
	}
	if err := json.Unmarshal(members["result"], &ir); err != nil {
		return nil, fmt.Errorf("result is not the invoke-result shape: %v in %s", err, payload)
	}
	reply.Status = ir.Status
	reply.OperationResult = ir.OperationResult
	reply.Reason = ir.Reason
	reply.ReasonCode = ir.ReasonCode
	return reply, nil
}

// .
// .
// .
func (w *Worker) Close(timeout time.Duration) error {
	_ = w.stdin.Close()
	done := make(chan error, 1)
	go func() { done <- w.cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("worker exited %d (want 0); stderr:\n%s", ee.ExitCode(), w.stderr.String())
		}
		return fmt.Errorf("worker wait: %w", err)
	case <-time.After(timeout):
		_ = w.cmd.Process.Kill()
		return fmt.Errorf("worker did not exit on stdin EOF within %s; stderr:\n%s", timeout, w.stderr.String())
	}
}
