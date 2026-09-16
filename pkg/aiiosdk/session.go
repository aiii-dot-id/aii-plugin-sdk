//go:build !wasm_unknown

package aiiosdk

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
// .
// .
// .
// .
// .
// .
// .

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// .
// .
// .
const (
	OpSessionOpen         = "speech.session.open"
	OpSessionSynthesize   = "speech.session.synthesize"
	OpSessionCancelSynth  = "speech.session.cancel_synthesis"
	OpSessionStopPlayback = "speech.session.stop_playback"
	OpSessionFinishInput  = "speech.session.finish_input"
	OpSessionClose        = "speech.session.close"
	OpSessionStatus       = "speech.session.status"
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	OpSessionPlaybackReport = "speech.session.playback_report"
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

const (
	// .
	// .
	// .
	sessionAdmitQueue = 64
	// .
	sessionMaxPending = 64
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
type Session struct {
	in      io.Reader
	out     io.Writer
	nextID  uint64
	pendMu  sync.Mutex
	pending map[uint64]chan sessionReply

	wOnce      sync.Once
	outQ       chan *outbound
	writerDone chan struct{}
	// .
	// .
	// .
	outMu      sync.Mutex
	outClosed  bool
	outChanged chan struct{}

	closeOnce sync.Once
	closed    chan struct{}

	faultMu        sync.Mutex
	fault          error
	intrOnce       sync.Once
	intrDone       chan struct{}
	intrErr        error
	admissionSlots chan struct{}
	// .
	// .
	// .
	// .
	// .
	// .
	// .
	settled chan struct{}
}

// .
// .
var alreadyClosed = func() chan struct{} { c := make(chan struct{}); close(c); return c }()

func (s *Session) settledCh() <-chan struct{} {
	if s.settled != nil {
		return s.settled
	}
	return alreadyClosed
}

// .
const sessionOutQueue = 64

// .
var errNotSent = errors.New("not sent")

// .
// .
// .
var errOutQueueFull = errors.New("outbound queue saturated")

// .
type outbound struct {
	frame   []byte
	claimed atomic.Bool
	done    chan error
}

// .
// .
func (s *Session) end() {
	s.closeOnce.Do(func() {
		s.outMu.Lock()
		close(s.closed)
		if s.settled == nil {
			s.outClosed = true
		}
		s.outMu.Unlock()
	})
}

// .
func (s *Session) setFault(err error) {
	s.faultMu.Lock()
	if s.fault == nil {
		s.fault = err
	}
	s.faultMu.Unlock()
	s.end()
}

func (s *Session) faultErr() error {
	s.faultMu.Lock()
	defer s.faultMu.Unlock()
	return s.fault
}

// .
// .
// .
func (s *Session) interruptRead() {
	s.intrOnce.Do(func() {
		s.intrDone = make(chan struct{})
		go func() {
			defer close(s.intrDone)
			if c, ok := s.in.(io.Closer); ok && s.in != nil {
				s.intrErr = c.Close()
			}
		}()
	})
}

type sessionReply struct {
	result json.RawMessage
	errObj json.RawMessage
}

// .
// .
// .
type Control struct {
	// .
	// .
	Session *Session
	// .
	Op   string
	Args Object

	reply *controlReply
}

// .
// .
type controlReply struct {
	id       json.RawMessage
	s        *Session
	answered atomic.Bool
}

// .
// .
// .
// .
// .
// .
// .
func (c *Control) Answer(result any, err error) {
	if c == nil || c.reply == nil || !c.reply.answered.CompareAndSwap(false, true) {
		return
	}
	r := c.reply
	defer func() { <-r.s.admissionSlots }()
	if derr := r.s.respondAdmission(r.id, result, err); derr != nil && !errors.Is(derr, errNotSent) {
		r.s.setFault(fmt.Errorf("aiiosdk: admission response not delivered: %w", derr))
	}
}

// .
// .
// .
type SessionAdmit func(*Control)

// .
// .
// .
func (p *Plugin) ServeSession(admit SessionAdmit) error {
	if describeAsked() {
		return p.writeDescriptors(os.Stdout)
	}
	return p.serveSession(pollableStdin(), os.Stdout, admit)
}

func (p *Plugin) serveSession(in io.Reader, out io.Writer, admit SessionAdmit) error {
	s := &Session{in: in, out: out, pending: make(map[uint64]chan sessionReply), closed: make(chan struct{}),
		admissionSlots: make(chan struct{}, sessionAdmitQueue), settled: make(chan struct{})}
	s.startWriter()

	// .
	// .
	// .
	// .
	// .
	admitCh := make(chan []byte, sessionAdmitQueue)
	admitDone := make(chan struct{})
	go func() {
		defer close(admitDone)
		for {
			var frame []byte
			var open bool
			select {
			case frame, open = <-admitCh:
			case <-s.closed:
				if s.faultErr() != nil {
					return
				}
				// .
				// .
				frame, open = <-admitCh
			}
			if !open {
				return
			}
			if s.faultErr() != nil {
				return
			}
			if err := s.admitOne(admit, frame); err != nil {
				s.setFault(fmt.Errorf("aiiosdk: admission response not delivered: %w", err))
			}
		}
	}()

	// .
	// .
	// .
	readDone := make(chan error, 1)
	go func() {
		err := s.readLoop(in, admitCh)
		close(admitCh)
		readDone <- err
	}()
	var readErr error
	select {
	case readErr = <-readDone:
	case <-s.closed:
		// .
		// .
		// .
		// .
		s.interruptRead()
		select {
		case readErr = <-readDone:
		default:
		}
	}
	// .
	// .
	// .
	s.end()
	// .
	// .
	// .
	// .
	// .
	// .
	go func() {
		<-admitDone
		s.outMu.Lock()
		s.outClosed = true
		close(s.settled)
		s.outMu.Unlock()
	}()
	var retirementErr error
	select {
	case <-s.settled:
	case <-time.After(2 * time.Second):
		retirementErr = errors.New("aiiosdk: an admission handler never returned; the lane ended without it")
		s.setFault(retirementErr)
	}
	select {
	case <-s.writerDone:
	case <-time.After(2 * time.Second):
		err := errors.New("aiiosdk: response delivery and writer retirement unresolved after lane ended")
		retirementErr = errors.Join(retirementErr, err)
		s.setFault(err)
	}
	if s.intrDone != nil {
		select {
		case <-s.intrDone:
			retirementErr = errors.Join(retirementErr, s.intrErr)
		default:
			retirementErr = errors.Join(retirementErr, errors.New("aiiosdk: reader interruption still pending at lane retirement"))
		}
	}
	return errors.Join(readErr, s.faultErr(), retirementErr)
}

// .
// .
// .
// .
// .
func (s *Session) readLoop(in io.Reader, admitCh chan<- []byte) error {
	for {
		if ferr := s.faultErr(); ferr != nil {
			return ferr
		}
		frame, err := ReadFrame(in, MaxControlFrameBytes)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if ferr := s.faultErr(); ferr != nil {
				return ferr
			}
			return fmt.Errorf("aiiosdk: session read: %w", err)
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method json.RawMessage `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(frame, &m) != nil {
			continue
		}
		switch {
		case len(m.Method) == 0 && len(m.ID) != 0:
			s.deliverReply(m.ID, sessionReply{result: m.Result, errObj: m.Error})
		case len(m.Method) != 0 && len(m.ID) != 0:
			// .
			// .
			// .
			// .
			select {
			case admitCh <- frame:
			default:
				return fmt.Errorf("aiiosdk: admission queue saturated (%d unanswered controls) — the lane is ended", sessionAdmitQueue)
			}
		default:
			// .
			// .
		}
	}
}

func (s *Session) admitOne(admit SessionAdmit, frame []byte) error {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Params struct {
			Operation string          `json:"operation"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	_ = json.Unmarshal(frame, &req)
	// .
	// .
	// .
	select {
	case s.admissionSlots <- struct{}{}:
	default:
		return errors.New("too many controls awaiting their answer")
	}
	c := &Control{Session: s, Op: req.Params.Operation, Args: Object(req.Params.Arguments), reply: &controlReply{id: req.ID, s: s}}
	// .
	// .
	admit(c)
	return nil
}

func (s *Session) respondAdmission(id json.RawMessage, result any, err error) error {
	var response []byte
	if err != nil {
		response = sessionErrorFrame(id, err.Error())
	} else if payload, merr := json.Marshal(result); merr != nil {
		response = sessionErrorFrame(id, "admission result is not encodable: "+merr.Error())
	} else {
		response = sessionResultFrame(id, payload)
	}
	s.startWriter()
	ob := &outbound{frame: response, done: make(chan error, 1)}
	_, qerr := s.tryEnqueue(ob, true)
	// .
	// .
	return qerr
}

// .
func (s *Session) startWriter() {
	s.wOnce.Do(func() {
		if s.outQ == nil {
			s.outQ = make(chan *outbound, sessionOutQueue)
		}
		s.outChanged = make(chan struct{})
		s.writerDone = make(chan struct{})
		go s.writer()
	})
}

// .
// .
// .
// .
func (s *Session) writer() {
	defer close(s.writerDone)
	for {
		select {
		case ob := <-s.outQ:
			s.writeQueued(ob)
		case <-s.closed:
			// .
			// .
			// .
			// .
			// .
			// .
			for {
				select {
				case ob := <-s.outQ:
					s.writeQueued(ob)
				case <-s.settledCh():
					for {
						select {
						case ob := <-s.outQ:
							s.writeQueued(ob)
						default:
							return
						}
					}
				}
			}
		}
	}
}

// .
// .
func (s *Session) writeQueued(ob *outbound) {
	s.outMu.Lock()
	close(s.outChanged)
	s.outChanged = make(chan struct{})
	s.outMu.Unlock()
	s.write(ob)
}

// .
// .
// .
func (s *Session) tryEnqueue(ob *outbound, admission bool) (<-chan struct{}, error) {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	if s.outClosed || s.faultErr() != nil {
		return nil, errNotSent
	}
	if !admission {
		select {
		case <-s.closed:
			return nil, errNotSent
		default:
		}
	}
	select {
	case s.outQ <- ob:
		return nil, nil
	default:
		return s.outChanged, errOutQueueFull
	}
}

func (s *Session) write(ob *outbound) {
	if !ob.claimed.CompareAndSwap(false, true) {
		return
	}
	if s.faultErr() != nil {
		ob.done <- errNotSent
		return
	}
	if err := WriteFrame(s.out, ob.frame, MaxControlFrameBytes); err != nil {
		s.setFault(fmt.Errorf("aiiosdk: transport write failed: %w", err))
		ob.done <- err
		return
	}
	ob.done <- nil
}

// .
// .
func (s *Session) enqueue(ctx context.Context, frame []byte) (*outbound, error) {
	s.startWriter()
	ob := &outbound{frame: frame, done: make(chan error, 1)}
	for {
		if ctx.Err() != nil {
			return nil, errNotSent
		}
		changed, err := s.tryEnqueue(ob, false)
		if !errors.Is(err, errOutQueueFull) {
			if err != nil {
				return nil, err
			}
			return ob, nil
		}
		select {
		case <-changed:
		case <-s.closed:
			return nil, errNotSent
		case <-ctx.Done():
			return nil, errNotSent
		}
	}
}

// .
// .
// .
// .
// .
func (s *Session) Emit(event any) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("aiiosdk: encode event: %w", err)
	}
	frame := append([]byte(`{"jsonrpc":"2.0","method":"session.event","params":`), payload...)
	frame = append(frame, '}')
	if _, err := s.enqueue(context.Background(), frame); err != nil {
		return fmt.Errorf("aiiosdk: event not sent: the lane has ended")
	}
	return nil
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
func (s *Session) HostCall(ctx context.Context, operation string, args any) (Object, error) {
	return s.HostCallTo(ctx, operation, nil, args)
}

// .
// .
// .
func (s *Session) HostCallTo(ctx context.Context, operation string, target any, args any) (Object, error) {
	argsRaw, err := json.Marshal(args)
	if err != nil {
		return nil, fmt.Errorf("aiiosdk: encode hostcall args: %w", err)
	}
	targetRaw := []byte(nil)
	if target != nil {
		if targetRaw, err = json.Marshal(target); err != nil {
			return nil, fmt.Errorf("aiiosdk: encode hostcall target: %w", err)
		}
	}
	s.pendMu.Lock()
	if len(s.pending) >= sessionMaxPending {
		s.pendMu.Unlock()
		return nil, fmt.Errorf("aiiosdk: too many outstanding host calls")
	}
	s.nextID++
	id := s.nextID
	ch := make(chan sessionReply, 1)
	s.pending[id] = ch
	s.pendMu.Unlock()
	defer func() {
		s.pendMu.Lock()
		delete(s.pending, id)
		s.pendMu.Unlock()
	}()

	op, merr := marshalValue(operation)
	if merr != nil {
		return nil, fmt.Errorf("aiiosdk: operation name cannot be written into a frame: %w", merr)
	}
	var frame []byte
	if targetRaw != nil {
		frame = []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"invoke.call","params":{"operation":%s,"target":%s,"arguments":%s}}`, id, op, targetRaw, argsRaw))
	} else {
		frame = []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"invoke.call","params":{"operation":%s,"arguments":%s}}`, id, op, argsRaw))
	}
	ob, qerr := s.enqueue(ctx, frame)
	if qerr != nil {
		return nil, fmt.Errorf("aiiosdk: hostcall not sent: the lane ended, or the call was cancelled, before the frame was queued")
	}
	select {
	case werr := <-ob.done:
		if errors.Is(werr, errNotSent) {
			return nil, fmt.Errorf("aiiosdk: hostcall not sent: the lane ended before the frame was written")
		}
		if werr != nil {
			return nil, fmt.Errorf("aiiosdk: hostcall outcome unknown: the write failed after it began: %w", werr)
		}
	case <-ctx.Done():
		if ob.claimed.CompareAndSwap(false, true) {
			return nil, fmt.Errorf("aiiosdk: hostcall not sent: cancelled before the transport took the frame")
		}
		return nil, fmt.Errorf("aiiosdk: hostcall outcome unknown: cancelled while the transport was writing the frame")
	case <-s.closed:
		if ob.claimed.CompareAndSwap(false, true) {
			return nil, fmt.Errorf("aiiosdk: hostcall not sent: the lane ended before the frame was written")
		}
		return nil, fmt.Errorf("aiiosdk: hostcall outcome unknown: the lane ended while the transport was writing the frame")
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("aiiosdk: hostcall outcome unknown: sent, no reply before cancellation")
	case <-s.closed:
		return nil, fmt.Errorf("aiiosdk: hostcall outcome unknown: sent, the session ended before the host answered")
	case reply := <-ch:
		if len(reply.errObj) != 0 {
			return nil, fmt.Errorf("aiiosdk: host refused: %s", reply.errObj)
		}
		return Object(reply.result), nil
	}
}

func (s *Session) deliverReply(idRaw json.RawMessage, reply sessionReply) {
	var id uint64
	if json.Unmarshal(idRaw, &id) != nil {
		return
	}
	s.pendMu.Lock()
	ch := s.pending[id]
	s.pendMu.Unlock()
	if ch != nil {
		select {
		case ch <- reply:
		default:
		}
	}
}

func sessionResultFrame(idRaw json.RawMessage, result json.RawMessage) []byte {
	if len(idRaw) == 0 {
		idRaw = json.RawMessage("null")
	}
	frame := append([]byte(`{"jsonrpc":"2.0","id":`), idRaw...)
	frame = append(frame, []byte(`,"result":`)...)
	frame = append(frame, result...)
	return append(frame, '}')
}

func sessionErrorFrame(idRaw json.RawMessage, message string) []byte {
	if len(idRaw) == 0 {
		idRaw = json.RawMessage("null")
	}
	msg, _ := json.Marshal(message)
	frame := append([]byte(`{"jsonrpc":"2.0","id":`), idRaw...)
	frame = append(frame, []byte(`,"error":{"code":-32000,"message":`)...)
	frame = append(frame, msg...)
	return append(frame, []byte(`}}`)...)
}

// .
var SessionControls = []string{OpSessionOpen, OpSessionSynthesize, OpSessionCancelSynth, OpSessionStopPlayback, OpSessionFinishInput, OpSessionClose, OpSessionStatus, OpSessionPlaybackReport}

// .
// .
// .
// .
// .
// .
func (p *Plugin) DeclareSession() *Plugin {
	for _, op := range SessionControls {
		p.Handle(op, func(Call) (any, error) {
			return nil, fmt.Errorf("aiiosdk: %s is a resident-session control — admitted on the session lane, never invoked", op)
		})
	}
	return p
}

// .
// .
// .
// .
// .
func (p *Plugin) ServeSessionReady(mark string, r ReadyReport, admit SessionAdmit) error {
	if describeAsked() {
		return p.writeDescriptors(os.Stdout)
	}
	fmt.Fprintln(os.Stderr, ReadyLine(mark, r))
	return p.serveSession(pollableStdin(), os.Stdout, admit)
}
