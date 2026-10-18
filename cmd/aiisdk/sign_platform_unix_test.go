//go:build !windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadSigningFileRejectsFIFOWithoutOpening(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-a-package")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	// There is deliberately no FIFO writer. Opening first would block
	// indefinitely; the deadline bounds that failure, not a scheduling race.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReadSigningFileRejectsFIFOWithoutOpening$")
	cmd.Env = append(os.Environ(), "AIISDK_PLATFORM_FIFO_PROBE="+path)
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatal("readSigningFile opened the FIFO instead of refusing its type")
	}
	if err != nil {
		t.Fatalf("FIFO was not refused as a non-regular file: %v %s", err, output)
	}
}
