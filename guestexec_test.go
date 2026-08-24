package main

import (
	"strings"
	"testing"
)

func TestAgentExecUnavailableOnlyForLaunchFailures(t *testing.T) {
	// A guest command exiting non-zero is a SUCCESSFUL agent call. Treating it as a
	// missing agent would silently re-run the command over a different transport.
	if agentExecUnavailable(&exitCodeError{code: 7}, "") {
		t.Fatal("a non-zero guest exit must not be treated as a missing agent")
	}
	if agentExecUnavailable(&exitCodeError{code: 1}, "ld: symbol not found\n") {
		t.Fatal("ordinary guest command stderr must not be treated as a missing agent")
	}
	for _, stderr := range []string{
		"Error: guest agent is not running",
		"Requires Tart Guest Agent running in a guest VM",
		"connection refused",
	} {
		if !agentExecUnavailable(&exitCodeError{code: 1}, stderr) {
			t.Fatalf("stderr %q should indicate a missing agent", stderr)
		}
	}
	if agentExecUnavailable(nil, "guest agent") {
		t.Fatal("a nil error is never an agent failure")
	}
}

func TestSudoRewriteMatchesTheSSHPath(t *testing.T) {
	got := rewriteSudoForStdin("sudo softwareupdate -l; echo done")
	if !strings.Contains(got, "sudo -S -p ''") {
		t.Fatalf("rewritten = %q", got)
	}
	if strings.Contains(rewriteSudoForStdin("echo pseudonym"), "-S") {
		t.Fatal("must not rewrite the substring 'sudo' inside another word")
	}
}
