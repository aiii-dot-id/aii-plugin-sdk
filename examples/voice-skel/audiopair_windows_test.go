//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"testing"
)

// giveAudioPair hands cmd the session audio pair as the host's supervisor
// does on Windows, where a child inherits handles, not descriptors, and
// exec.Cmd.ExtraFiles is refused ("not supported by windows"): each of
// the child's ends is duplicated as an inheritable handle, listed in
// SysProcAttr.AdditionalInheritedHandles and named by its value in
// AII_AUDIO_IN_FD and AII_AUDIO_OUT_FD (appended to cmd.Env); the pipes
// themselves stay non-inheritable. It returns the host's ends and started,
// which closes the duplicates once cmd has started; the host's ends close
// with the test.
func giveAudioPair(t *testing.T, cmd *exec.Cmd) (hostIn, hostOut *os.File, started func()) {
	t.Helper()
	childIn, hostIn, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	hostOut, childOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hostIn.Close(); hostOut.Close() })
	self, err := syscall.GetCurrentProcess()
	if err != nil {
		t.Fatal(err)
	}
	var inherit []syscall.Handle
	for _, f := range []*os.File{childIn, childOut} {
		var dup syscall.Handle
		if err := syscall.DuplicateHandle(self, syscall.Handle(f.Fd()), self, &dup, 0, true, syscall.DUPLICATE_SAME_ACCESS); err != nil {
			t.Fatalf("audio pair: inheritable handle: %v", err)
		}
		inherit = append(inherit, dup)
	}
	childIn.Close()
	childOut.Close()
	var once sync.Once
	release := func() {
		once.Do(func() {
			for _, h := range inherit {
				_ = syscall.CloseHandle(h)
			}
		})
	}
	t.Cleanup(release)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.AdditionalInheritedHandles = append(cmd.SysProcAttr.AdditionalInheritedHandles, inherit...)
	cmd.Env = append(cmd.Env, fmt.Sprintf("AII_AUDIO_IN_FD=%d", uint64(inherit[0])), fmt.Sprintf("AII_AUDIO_OUT_FD=%d", uint64(inherit[1])))
	return hostIn, hostOut, release
}
