//go:build !wasm_unknown

package aiiosdk

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

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
// .
// .
// .
// .
// .
// .

// .
// .
// .
var nativeTransport atomic.Pointer[stdioTransport]

type stdioTransport struct {
	in     io.Reader
	out    io.Writer
	nextID atomic.Uint64
}

// .
// .
// .
// .
// .
// .
// .
// .
func (p *Plugin) Serve(readyMark string) error {
	if describeAsked() {
		return p.writeDescriptors(os.Stdout)
	}
	return p.serve(os.Stdin, os.Stdout, os.Stderr, readyMark)
}

// .
// .
// .
// .
// .
// .
// .
const DescribeEnv = "AIISDK_DESCRIBE"

func describeAsked() bool { return os.Getenv(DescribeEnv) == "1" }

func (p *Plugin) writeDescriptors(w io.Writer) error {
	out, err := p.DescriptorsJSON()
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

// .
// .
// .
// .
// .
type ReadyReport struct {
	ModelsLoaded int
	Accelerator  string
	ProbeMS      int
}

// .
func ReadyLine(mark string, r ReadyReport) string {
	return fmt.Sprintf("%s event=ready models_loaded=%d accelerator=%s probe_ms=%d", mark, r.ModelsLoaded, r.Accelerator, r.ProbeMS)
}

// .
// .
// .
func (p *Plugin) ServeReady(mark string, r ReadyReport) error {
	if describeAsked() {
		return p.writeDescriptors(os.Stdout)
	}
	return p.serve(os.Stdin, os.Stdout, os.Stderr, ReadyLine(mark, r))
}

func (p *Plugin) serve(in io.Reader, out, errw io.Writer, readyMark string) error {
	t := &stdioTransport{in: in, out: out}
	if !nativeTransport.CompareAndSwap(nil, t) {
		return fmt.Errorf("aiiosdk: Serve called twice; one process carries one plugin")
	}
	defer nativeTransport.Store(nil)

	if readyMark != "" {
		// .
		if _, err := fmt.Fprintf(errw, "%s\n", readyMark); err != nil {
			return fmt.Errorf("aiiosdk: announce readiness: %w", err)
		}
	}

	for {
		frame, err := ReadFrame(in, MaxControlFrameBytes)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("aiiosdk: read frame: %w", err)
		}
		// .
		// .
		// .
		reply := p.respond(frame)
		if len(reply) == 0 {
			// .
			// .
			reply = errorResponse(bestEffortID(frame), codeInternal, msgNotRegistered, reasonNotRegistered)
		}
		if err := WriteFrame(out, reply, MaxControlFrameBytes); err != nil {
			return fmt.Errorf("aiiosdk: write reply: %w", err)
		}
	}
}

// .
// .
// .
// .
// .
// .
func (t *stdioTransport) hostInvokeNative(params []byte) ([]byte, error) {
	id := t.nextID.Add(1)
	req := append([]byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"invoke.call","params":`, id)), params...)
	req = append(req, '}')
	if err := WriteFrame(t.out, req, MaxControlFrameBytes); err != nil {
		return nil, fmt.Errorf("aiiosdk: write hostcall: %w", err)
	}
	frame, err := ReadFrame(t.in, MaxControlFrameBytes)
	if err != nil {
		return nil, fmt.Errorf("aiiosdk: read hostcall reply: %w", err)
	}
	var reply struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	if err := json.Unmarshal(frame, &reply); err != nil {
		return nil, fmt.Errorf("aiiosdk: hostcall reply is not an object: %w", err)
	}
	// .
	// .
	// .
	var gotID uint64
	if err := json.Unmarshal(reply.ID, &gotID); err != nil || gotID != id {
		return nil, fmt.Errorf("aiiosdk: hostcall reply id %s does not match request %d", reply.ID, id)
	}
	if len(reply.Error) > 0 {
		return reply.Error, nil
	}
	return reply.Result, nil
}
