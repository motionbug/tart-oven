package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

var sudoPattern = regexp.MustCompile(`(^|[\s;&|(])sudo(\s)`)

// rewriteSudoForStdin makes every sudo in the command read its password from stdin
// instead of a TTY, so a non-interactive session can run it. Shared by the guest
// agent and SSH paths so both behave identically.
func rewriteSudoForStdin(command string) string {
	return sudoPattern.ReplaceAllString(command, `${1}sudo -S -p ''${2}`)
}

// exitCodeError carries a guest command's exit status.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }
func (e *exitCodeError) ExitCode() int { return e.code }

// agentExecUnavailable reports whether a `tart exec` failure means the guest agent
// is not usable, as opposed to the guest command itself failing. A guest command
// that exits non-zero is a successful agent call and must NOT fall back to SSH —
// otherwise a legitimately failing command would be silently re-run over another
// transport.
func agentExecUnavailable(err error, stderr string) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(stderr)
	for _, marker := range []string{
		"guest agent",
		"connection refused",
		"is only available on macos",
		"failed to connect",
		"vm is not running",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// execViaAgent runs command inside the guest through the Tart guest agent. The
// second return value reports whether the agent handled the call at all; when it is
// false the caller should fall back to SSH.
func (m *Manager) execViaAgent(ctx context.Context, name, command, sudoPassword string) (execResult, bool) {
	m.mu.Lock()
	home := m.cfg.VMStoragePath
	m.mu.Unlock()

	remote := command
	if sudoPassword != "" {
		remote = rewriteSudoForStdin(command)
	}

	cmd := m.tartCmdCtx(ctx, home, "exec", name, "/bin/sh", "-c", remote)
	if sudoPassword != "" {
		cmd.Stdin = strings.NewReader(sudoPassword + "\n")
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res := execResult{Stdout: stdout.String(), Stderr: stderr.String()}
	if err == nil {
		return res, true
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		res.Error = ctxErr.Error()
		return res, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if agentExecUnavailable(err, res.Stderr) {
			return execResult{}, false
		}
		// The command ran and returned a status: a successful agent call.
		res.ExitCode = exitErr.ExitCode()
		return res, true
	}
	// tart itself could not be launched.
	return execResult{}, false
}

// execInGuest is the single way to run a command inside a guest. It prefers the
// Tart guest agent (no SSH, no key, no guest network) and falls back to SSH for
// images that do not ship the agent.
func (m *Manager) execInGuest(ctx context.Context, name, command, sudoPassword string) execResult {
	if res, handled := m.execViaAgent(ctx, name, command, sudoPassword); handled {
		m.setAgentOK(name, true)
		return res
	}
	m.setAgentOK(name, false)
	return m.sshExecContext(ctx, name, command, sudoPassword)
}

// setAgentOK records whether the guest agent answered, for display only.
func (m *Manager) setAgentOK(name string, ok bool) {
	m.mu.Lock()
	if vm := m.vms[name]; vm != nil {
		vm.AgentOK = ok
	}
	m.mu.Unlock()
}
