package main

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func verifyPackageCommand(cmd *exec.Cmd, id, version, packageHash, manifestHash string) (string, string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	diagnostics := strings.TrimSpace(stderr.String())
	if err != nil {
		return "", diagnostics, fmt.Errorf("verification command failed: %w; stdout: %s", err, strings.TrimSpace(stdout.String()))
	}
	summary, err := verifiedPackageOutput(stdout.String(), id, version, packageHash, manifestHash)
	if err != nil {
		return "", diagnostics, fmt.Errorf("%w; stdout: %s", err, strings.TrimSpace(stdout.String()))
	}
	return summary, diagnostics, nil
}

func verifiedPackageOutput(output, id, version, packageHash, manifestHash string) (string, error) {
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 3 {
		return "", fmt.Errorf("unexpected verifier output: expected a verification summary and both package hashes")
	}
	summary := strings.Fields(lines[0])
	if len(summary) != 4 || summary[0] != "VERIFIED" || summary[2] != id || summary[3] != version {
		return "", fmt.Errorf("verifier summary does not identify the requested plugin and version")
	}
	wantLines := 3
	switch summary[1] {
	case "T0", "T3":
	case "T1":
		wantLines = 4
	case "T2":
		wantLines = 5
	default:
		return "", fmt.Errorf("verifier summary has an unknown trust tier")
	}
	if len(lines) != wantLines {
		return "", fmt.Errorf("unexpected verifier output for %s", summary[1])
	}
	if wantLines >= 4 {
		publisher := strings.Fields(lines[3])
		if len(publisher) != 2 || publisher[0] != "publisher" {
			return "", fmt.Errorf("verifier publisher line is malformed")
		}
	}
	if wantLines == 5 {
		capabilities := strings.TrimSpace(lines[4])
		if !strings.HasPrefix(capabilities, "reviewed_capabilities [") || !strings.HasSuffix(capabilities, "]") {
			return "", fmt.Errorf("verifier reviewed capabilities line is malformed")
		}
	}
	for i, want := range []struct{ label, value string }{{"package_hash", packageHash}, {"manifest_hash", manifestHash}} {
		fields := strings.Fields(lines[i+1])
		if want.value == "" || len(fields) != 2 || fields[0] != want.label || fields[1] != want.value {
			return "", fmt.Errorf("verifier %s does not match the staged package", want.label)
		}
	}
	return strings.Join(summary, " "), nil
}
