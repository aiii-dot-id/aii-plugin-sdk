//go:build !wasm_unknown

package aiiosdk

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type idleReader struct {
	entered, release chan struct{}
	once             sync.Once
}

func (r *idleReader) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	return 0, io.EOF
}

type failWhenReaderIdle struct {
	idle   <-chan struct{}
	failed chan struct{}
	once   sync.Once
}

func (w *failWhenReaderIdle) Write([]byte) (int, error) {
	<-w.idle
	w.once.Do(func() { close(w.failed) })
	return 0, errors.New("injected response write failure")
}

func TestAResponseFailureEndsAnIdleLane(t *testing.T) {
	var frame bytes.Buffer
	err := WriteFrame(&frame, []byte(`{"jsonrpc":"2.0","id":1,"method":"invoke.call","params":{"operation":"speech.session.open","arguments":{}}}`), MaxControlFrameBytes)
	if err != nil {
		t.Fatal(err)
	}
	idle := &idleReader{entered: make(chan struct{}), release: make(chan struct{})}
	writer := &failWhenReaderIdle{idle: idle.entered, failed: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		done <- New("test.voice").serveSession(io.MultiReader(&frame, idle), writer, answerWith(func(*Session, string, Object) (any, error) { return map[string]bool{"accepted": true}, nil }))
	}()
	<-writer.failed
	select {
	case err := <-done:
		close(idle.release)
		if err == nil {
			t.Fatal("write failure returned success")
		}
	case <-time.After(100 * time.Millisecond):
		close(idle.release)
		<-done
		t.Fatal("failed admission write left ServeSession blocked on idle input until external EOF")
	}
}

type reviewBlockedWriter struct {
	entered, release chan struct{}
	once             sync.Once
}

func (w *reviewBlockedWriter) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(p), nil
}

func TestAHostCallDeadlineCoversTheWrite(t *testing.T) {
	w := &reviewBlockedWriter{entered: make(chan struct{}), release: make(chan struct{})}
	s := &Session{out: w, pending: make(map[uint64]chan sessionReply), closed: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.HostCall(ctx, "voice.observe", map[string]any{}); done <- err }()
	<-w.entered
	cancel()
	select {
	case <-done:
		close(w.release)
	case <-time.After(100 * time.Millisecond):
		close(w.release)
		<-done
		t.Fatal("HostCall ignored cancellation while blocked writing its frame")
	}
}

type reviewDeliveredButErrorWriter struct {
	bytes.Buffer
	calls int
}

func (w *reviewDeliveredButErrorWriter) Write(p []byte) (int, error) {
	w.calls++
	n, _ := w.Buffer.Write(p)
	if w.calls == 2 {
		return n, errors.New("error after payload delivery")
	}
	return n, nil
}
func TestADeliveredHostCallIsNeverReportedUnsent(t *testing.T) {
	w := &reviewDeliveredButErrorWriter{}
	s := &Session{out: w, pending: make(map[uint64]chan sessionReply), closed: make(chan struct{})}
	_, err := s.HostCall(context.Background(), "voice.observe", map[string]any{})
	if _, readErr := ReadFrame(bytes.NewReader(w.Bytes()), MaxControlFrameBytes); readErr != nil {
		t.Fatalf("invalid fixture: complete frame not delivered: %v", readErr)
	}
	if err == nil || strings.Contains(err.Error(), "not sent") {
		t.Fatalf("complete hostcall reached peer yet reported safe to retry: %v", err)
	}
}

type faultWriter struct{}

func (faultWriter) Write([]byte) (int, error) {
	return 0, errors.New("output pipe failed")
}

func TestServeSessionReportsAResponseWriteFailure(t *testing.T) {
	var in bytes.Buffer
	if err := WriteFrame(&in, []byte(`{"jsonrpc":"2.0","id":1,"method":"invoke.call","params":{"operation":"speech.session.close","arguments":{}}}`), MaxControlFrameBytes); err != nil {
		t.Fatal(err)
	}
	called := false
	err := New("test.voice").serveSession(&in, faultWriter{}, answerWith(func(*Session, string, Object) (any, error) {
		called = true
		return map[string]bool{"accepted": true}, nil
	}))
	if !called {
		t.Fatal("fixture never exercised response write")
	}
	if err == nil {
		t.Fatal("ServeSession returned success after the admission response write failed")
	}
}

// .
// .
// .
// .
// .
// .
func TestAnUnansweredControlAlwaysEndsTheLaneWithItsReason(t *testing.T) {
	for i := 0; i < 300; i++ {
		var in bytes.Buffer
		if err := WriteFrame(&in, []byte(`{"jsonrpc":"2.0","id":1,"method":"invoke.call","params":{"operation":"speech.session.status","arguments":{}}}`), MaxControlFrameBytes); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		err := New("test.voice").serveSession(&in, &out, answerWith(func(*Session, string, Object) (any, error) {
			return map[string]bool{"accepted": true}, nil
		}))
		answered := strings.Contains(out.String(), `"accepted":true`)
		if !answered && err == nil {
			t.Fatalf("round %d: the host was never answered and the lane reported success", i)
		}
		if answered && err != nil {
			t.Fatalf("round %d: the host WAS answered, yet the lane failed: %v", i, err)
		}
	}
}
