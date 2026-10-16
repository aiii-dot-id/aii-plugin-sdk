// Copyright 2026 AI Identity Incorporated <james@aiii.id>
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const verificationFixture = "VERIFIED T0 id.example.plugin 1.2.3\n  package_hash sha256:package\n  manifest_hash sha256:manifest\n"

func TestVerifierOutputBindsExactPackage(t *testing.T) {
	for _, tc := range []struct {
		name, output string
		ok           bool
	}{
		{"valid", verificationFixture, true},
		{"valid_t1", strings.Replace(verificationFixture, "T0", "T1", 1) + "  publisher id.example.publisher\n", true},
		{"valid_t2", strings.Replace(verificationFixture, "T0", "T2", 1) + "  publisher id.example.publisher\n  reviewed_capabilities [fs.private]\n", true},
		{"valid_t3", strings.Replace(verificationFixture, "T0", "T3", 1), true},
		{"missing_publisher", strings.Replace(verificationFixture, "T0", "T1", 1), false},
		{"missing_review", strings.Replace(verificationFixture, "T0", "T2", 1) + "  publisher id.example.publisher\n", false},
		{"malformed_publisher", strings.Replace(verificationFixture, "T0", "T1", 1) + "  publisher\n", false},
		{"malformed_review", strings.Replace(verificationFixture, "T0", "T2", 1) + "  publisher id.example.publisher\n  reviewed_capabilities wrong\n", false},

		{"windows_newlines", strings.ReplaceAll(verificationFixture, "\n", "\r\n"), true},
		{"diagnostic_on_stdout", "revocation: status\n" + verificationFixture, false},
		{"stderr_is_not_stdout", "", false},
		{"malformed_summary", strings.Replace(verificationFixture, "VERIFIED T0", "VERIFIED Trash", 1), false},
		{"unknown_tier", strings.Replace(verificationFixture, "T0", "T4", 1), false},
		{"wrong_plugin", strings.Replace(verificationFixture, "id.example.plugin", "id.other.plugin", 1), false},
		{"wrong_version", strings.Replace(verificationFixture, "1.2.3", "1.2.4", 1), false},
		{"wrong_package_hash", strings.Replace(verificationFixture, "sha256:package", "sha256:other", 1), false},
		{"wrong_manifest_hash", strings.Replace(verificationFixture, "sha256:manifest", "sha256:other", 1), false},
		{"truncated", "VERIFIED T0 id.example.plugin 1.2.3\n", false},
		{"extra_success", verificationFixture + verificationFixture, false},
		{"extra_prose", verificationFixture + "unrelated output\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := verifiedPackageOutput(tc.output, "id.example.plugin", "1.2.3", "sha256:package", "sha256:manifest")
			if (err == nil) != tc.ok {
				t.Fatalf("accepted=%v, want=%v: %v", err == nil, tc.ok, err)
			}
		})
	}
}

func TestVerifierCommandSeparatesDiagnosticsAndRequiresSuccessfulExit(t *testing.T) {
	for _, mode := range []string{"diagnostics_first", "nonzero_after_valid_output", "success_only_on_stderr", "malformed_stdout"} {
		t.Run(mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestVerifierHelperProcess$", "--")
			cmd.Env = append(os.Environ(), "AIISDK_VERIFIER_FIXTURE="+mode)
			summary, diagnostics, err := verifyPackageCommand(cmd, "id.example.plugin", "1.2.3", "sha256:package", "sha256:manifest")
			if mode == "diagnostics_first" {
				if err != nil || summary != "VERIFIED T0 id.example.plugin 1.2.3" {
					t.Fatalf("valid verification refused: %q %v", summary, err)
				}
			} else if err == nil {
				t.Fatalf("accepted %s", mode)
			}
			if !strings.Contains(diagnostics, "revocation fixture: epoch 1, 0 revoked") {
				t.Fatalf("lost stderr diagnostic: %q", diagnostics)
			}
			if mode == "nonzero_after_valid_output" && !strings.Contains(err.Error(), "exit status 7") {
				t.Fatalf("lost failure exit: %v", err)
			}
		})
	}
}

func TestVerifierHelperProcess(t *testing.T) {
	mode := os.Getenv("AIISDK_VERIFIER_FIXTURE")
	if mode == "" {
		return
	}
	fmt.Fprintln(os.Stderr, "revocation fixture: epoch 1, 0 revoked")
	switch mode {
	case "success_only_on_stderr":
		fmt.Fprint(os.Stderr, verificationFixture)
	case "malformed_stdout":
		fmt.Fprintln(os.Stdout, "VERIFIED Trash")
	default:
		fmt.Fprint(os.Stdout, verificationFixture)
	}
	if mode == "nonzero_after_valid_output" {
		os.Exit(7)
	}
	os.Exit(0)
}
