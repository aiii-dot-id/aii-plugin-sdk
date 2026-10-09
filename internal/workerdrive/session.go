package workerdrive

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	sdk "github.com/aiii-dot-id/aii-plugin-sdk/pkg/aiiosdk"
)

type SessionWorker struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stderr  *lockedBuffer
	banner  string
	handler Handler

	audioIn  *os.File
	audioOut *os.File

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  uint64
	pending map[string]chan []byte
	events  chan json.RawMessage
	ended   chan struct{}
	readErr error
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) WriteString(s string) {
	b.mu.Lock()
	b.buf.WriteString(s)
	b.mu.Unlock()
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

const sessionEventBuffer = 4096

func StartNativeSession(binary string, env []string, timeout time.Duration, handler Handler) (*SessionWorker, error) {
	cmd := exec.Command(binary)
	pair, env, err := prepareAudioPair(cmd, append([]string{}, env...))
	if err != nil {
		return nil, err
	}
	cmd.Env = env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		pair.closeAll()
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		pair.closeAll()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		pair.closeAll()
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		pair.closeAll()
		return nil, fmt.Errorf("start %s: %w", cmd.Path, err)
	}
	pair.childClose()
	w := &SessionWorker{cmd: cmd, stdin: stdin, stderr: &lockedBuffer{}, handler: handler,
		audioIn: pair.hostIn, audioOut: pair.hostOut,
		pending: map[string]chan []byte{}, events: make(chan json.RawMessage, sessionEventBuffer), ended: make(chan struct{})}
	bannerCh := make(chan string, 1)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
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
	case <-ended:
		select {
		case w.banner = <-bannerCh:
		default:
			pair.closeHost()
			return nil, exitedBeforeReady(cmd, w.stderr.String())
		}
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		pair.closeHost()
		return nil, fmt.Errorf("no ready banner within %s; stderr:\n%s", timeout, w.stderr.String())
	}
	go w.read(stdout)
	return w, nil
}

func (w *SessionWorker) Banner() string { return w.banner }

func (w *SessionWorker) Stderr() string { return w.stderr.String() }

func (w *SessionWorker) Events() <-chan json.RawMessage { return w.events }

func (w *SessionWorker) Ended() <-chan struct{} { return w.ended }

func (w *SessionWorker) read(stdout io.Reader) {
	defer func() {
		close(w.ended)
		close(w.events)
	}()
	for {
		frame, err := sdk.ReadFrame(stdout, sdk.MaxControlFrameBytes)
		if err != nil {
			w.mu.Lock()
			if !errors.Is(err, io.EOF) {
				w.readErr = err
			}
			w.mu.Unlock()
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method json.RawMessage `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(frame, &m) != nil {
			continue
		}
		switch {
		case len(m.Method) == 0 && len(m.ID) != 0:
			w.mu.Lock()
			ch := w.pending[string(m.ID)]
			delete(w.pending, string(m.ID))
			w.mu.Unlock()
			if ch != nil {
				ch <- frame
			}
		case len(m.Method) != 0 && len(m.ID) == 0:
			ev := m.Params
			if len(ev) == 0 {
				ev = frame
			}
			w.events <- ev
		case len(m.Method) != 0:
			var method string
			_ = json.Unmarshal(m.Method, &method)
			_ = w.answer(upstream{idRaw: string(m.ID), method: method, params: m.Params})
		}
	}
}

func (w *SessionWorker) answer(req upstream) error {
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
	return w.write(resp.Bytes())
}

func (w *SessionWorker) write(frame []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	return sdk.WriteFrame(w.stdin, frame, sdk.MaxControlFrameBytes)
}

func (w *SessionWorker) call(operation string, args json.RawMessage, timeout time.Duration) ([]byte, string, error) {
	w.mu.Lock()
	w.nextID++
	idRaw := fmt.Sprintf("%d", w.nextID)
	ch := make(chan []byte, 1)
	w.pending[idRaw] = ch
	w.mu.Unlock()
	encoded, _ := json.Marshal(operation)
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	frame := []byte(`{"jsonrpc":"2.0","id":` + idRaw + `,"method":"invoke.call","params":{"operation":` + string(encoded) + `,"arguments":` + string(args) + `}}`)
	forget := func() {
		w.mu.Lock()
		delete(w.pending, idRaw)
		w.mu.Unlock()
	}
	if err := w.write(frame); err != nil {
		forget()
		return nil, idRaw, fmt.Errorf("write control frame: %w", err)
	}
	select {
	case reply := <-ch:
		return reply, idRaw, nil
	case <-w.ended:
		forget()
		return nil, idRaw, fmt.Errorf("the lane ended before %s was answered (stderr:\n%s)", operation, w.Stderr())
	case <-time.After(timeout):
		forget()
		return nil, idRaw, fmt.Errorf("%w: %s, within %s", ErrNoAnswer, operation, timeout)
	}
}

type ControlRefusedError struct {
	Operation string
	Answer    json.RawMessage
}

func (e *ControlRefusedError) Error() string {
	return fmt.Sprintf("%s refused: %s", e.Operation, e.Answer)
}

func (w *SessionWorker) Control(operation string, args interface{}, timeout time.Duration) (json.RawMessage, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	reply, _, err := w.call(operation, raw, timeout)
	if err != nil {
		return nil, err
	}
	var m struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(reply, &m); err != nil {
		return nil, fmt.Errorf("%s: the reply is not a JSON object: %s", operation, reply)
	}
	if len(m.Error) != 0 {
		return nil, &ControlRefusedError{Operation: operation, Answer: m.Error}
	}
	return m.Result, nil
}

func (w *SessionWorker) Invoke(operation string, argsJSON json.RawMessage, timeout time.Duration) (*Reply, error) {
	reply, idRaw, err := w.call(operation, argsJSON, timeout)
	if err != nil {
		return nil, err
	}
	return decodeReply(reply, idRaw)
}

func (w *SessionWorker) WriteAudio(fr sdk.AudioFrame) error {
	return sdk.WriteAudioFrame(w.audioIn, fr)
}

func (w *SessionWorker) ReadAudio() (sdk.AudioFrame, error) { return sdk.ReadAudioFrame(w.audioOut) }

func (w *SessionWorker) Close(timeout time.Duration) error {
	_ = w.stdin.Close()
	_ = w.audioIn.Close()
	done := make(chan error, 1)
	go func() { done <- w.cmd.Wait() }()
	defer w.audioOut.Close()
	select {
	case err := <-done:
		if err == nil {
			return nil
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return fmt.Errorf("the carrier exited %d (want 0); stderr:\n%s", ee.ExitCode(), w.Stderr())
		}
		return fmt.Errorf("carrier wait: %w", err)
	case <-time.After(timeout):
		_ = w.cmd.Process.Kill()
		return fmt.Errorf("the carrier did not exit on stdin EOF within %s; stderr:\n%s", timeout, w.Stderr())
	}
}

type audioPair struct {
	hostIn, hostOut *os.File
	childClose      func()
}

func (p *audioPair) closeHost() {
	_ = p.hostIn.Close()
	_ = p.hostOut.Close()
}

func (p *audioPair) closeAll() {
	p.childClose()
	p.closeHost()
}
