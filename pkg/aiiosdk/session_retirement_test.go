//go:build !wasm_unknown

package aiiosdk

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

type retirementBlockingCloser struct {
	io.Reader
	entered, release, finished chan struct{}
	idle                       *idleReader
	session                    *Session
}

func (r *retirementBlockingCloser) Close() error {
	close(r.entered)
	<-r.release
	close(r.idle.release)
	close(r.finished)
	return nil
}

func retirementFrame(t *testing.T) *bytes.Buffer {
	t.Helper()
	var frame bytes.Buffer
	if err := WriteFrame(&frame, []byte(`{"jsonrpc":"2.0","id":1,"method":"invoke.call","params":{"operation":"speech.session.close","arguments":{}}}`), MaxControlFrameBytes); err != nil {
		t.Fatal(err)
	}
	return &frame
}

func TestSessionFaultDoesNotWaitForBlockingReaderClose(t *testing.T) {
	idle := &idleReader{entered: make(chan struct{}), release: make(chan struct{})}
	in := &retirementBlockingCloser{Reader: io.MultiReader(retirementFrame(t), idle), idle: idle, entered: make(chan struct{}), release: make(chan struct{}), finished: make(chan struct{})}
	defer func() { close(in.release); <-in.finished }()
	faulted := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- New("test.blocking-close").serveSession(in, io.Discard, func(c *Control) {
			in.session = c.Session
			c.Answer(true, nil)
			close(faulted)
		})
	}()
	<-idle.entered
	<-faulted
	// .
	// .
	in.session.setFault(errors.New("the engine failed while the reader was blocked"))
	select {
	case <-in.entered:
	case <-time.After(time.Second):
		t.Fatal("reader close was not attempted")
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "the engine failed while the reader was blocked") || !strings.Contains(err.Error(), "reader interruption still pending") {
			t.Fatalf("fault/retirement truth lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("lane fault waited behind the reader's blocking Close")
	}
}

func TestAdmissionRetirementTimeoutCannotClaimSettlement(t *testing.T) {
	entered := make(chan *Session, 1)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- New("test.admission-retirement").serveSession(retirementFrame(t), io.Discard, func(c *Control) { entered <- c.Session; <-release; c.Answer(true, nil) })
	}()
	s := <-entered
	defer func() {
		close(release)
		select {
		case <-s.writerDone:
		case <-time.After(time.Second):
			t.Error("released producer/writer did not retire")
		}
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "never returned") {
			t.Fatalf("missing retirement failure: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("admission retirement ignored its bound")
	}
	select {
	case <-s.settled:
		t.Fatal("timeout falsely declared producers settled")
	default:
	}
}
