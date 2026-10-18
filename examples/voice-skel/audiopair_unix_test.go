//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
)

// giveAudioPair hands cmd the session audio pair as the host's supervisor
// does on unix: two pipes, the child's ends inherited after stdio and any
// ExtraFiles already set, their descriptor numbers in AII_AUDIO_IN_FD and
// AII_AUDIO_OUT_FD (appended to cmd.Env). It returns the host's ends and
// started, which closes this process's copies of the child's ends once cmd
// has started; the host's ends close with the test.
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
	t.Cleanup(func() { childIn.Close(); childOut.Close(); hostIn.Close(); hostOut.Close() })
	base := 3 + len(cmd.ExtraFiles)
	cmd.ExtraFiles = append(cmd.ExtraFiles, childIn, childOut)
	cmd.Env = append(cmd.Env, fmt.Sprintf("AII_AUDIO_IN_FD=%d", base), fmt.Sprintf("AII_AUDIO_OUT_FD=%d", base+1))
	return hostIn, hostOut, func() { childIn.Close(); childOut.Close() }
}
