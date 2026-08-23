package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMaybeReleaseGoMemoryOnlyReleasesMeaningfulIdleHeap(t *testing.T) {
	tests := []struct {
		name      string
		stats     runtime.MemStats
		wantFreed bool
	}{
		{
			name:      "retained heap above threshold",
			stats:     runtime.MemStats{HeapIdle: 96 << 20, HeapReleased: 16 << 20},
			wantFreed: true,
		},
		{
			name:      "small retained heap",
			stats:     runtime.MemStats{HeapIdle: 32 << 20, HeapReleased: 8 << 20},
			wantFreed: false,
		},
		{
			name:      "idle heap already returned",
			stats:     runtime.MemStats{HeapIdle: 96 << 20, HeapReleased: 96 << 20},
			wantFreed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory := &fakeGoMemory{stats: tt.stats}
			gotBytes, gotFreed := maybeReleaseGoMemory(memory)
			if gotFreed != tt.wantFreed {
				t.Fatalf("released = %v, want %v", gotFreed, tt.wantFreed)
			}
			wantBytes := tt.stats.HeapIdle - tt.stats.HeapReleased
			if gotBytes != wantBytes {
				t.Fatalf("releasable bytes = %d, want %d", gotBytes, wantBytes)
			}
			wantCalls := 0
			if tt.wantFreed {
				wantCalls = 1
			}
			if memory.freeCalls != wantCalls {
				t.Fatalf("FreeOSMemory calls = %d, want %d", memory.freeCalls, wantCalls)
			}
		})
	}
}

type fakeGoMemory struct {
	stats     runtime.MemStats
	freeCalls int
}

func (f *fakeGoMemory) ReadMemStats() runtime.MemStats { return f.stats }
func (f *fakeGoMemory) FreeOSMemory()                  { f.freeCalls++ }

func TestDoRunDefersStartDuringCriticalMemoryPressure(t *testing.T) {
	m := newTestManager(t)
	m.vms["base"] = &VM{Name: "base", State: "stopped"}
	m.performanceHistory = []PerformanceSample{{
		Timestamp:         time.Now(),
		MemoryPressure:    "critical",
		PressureAvailable: true,
	}}

	m.doRun("base", "manual")

	vm := m.vms["base"]
	if vm.State != "stopped" {
		t.Fatalf("state = %q, want stopped", vm.State)
	}
	if !strings.Contains(vm.LastError, "critical memory pressure") {
		t.Fatalf("LastError = %q, want critical-pressure explanation", vm.LastError)
	}
	if m.busy["base"] {
		t.Fatal("critical-pressure deferral left VM busy")
	}
	if len(m.history) != 0 {
		t.Fatalf("history entries = %d, want no attempted run", len(m.history))
	}
}

func TestVMStartPressureGateOnlyBlocksAvailableCriticalState(t *testing.T) {
	tests := []struct {
		name   string
		sample PerformanceSample
		want   bool
	}{
		{"critical", PerformanceSample{MemoryPressure: "critical", PressureAvailable: true}, true},
		{"warning", PerformanceSample{MemoryPressure: "warning", PressureAvailable: true}, false},
		{"normal", PerformanceSample{MemoryPressure: "normal", PressureAvailable: true}, false},
		{"unavailable critical", PerformanceSample{MemoryPressure: "critical", PressureAvailable: false}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deferVMStartForPressure(tt.sample); got != tt.want {
				t.Fatalf("deferVMStartForPressure(%+v) = %v, want %v", tt.sample, got, tt.want)
			}
		})
	}
}

func TestVMStartPressureGateKeepsLastAvailableStateAcrossCollectionFailure(t *testing.T) {
	history := []PerformanceSample{
		{MemoryPressure: "critical", PressureAvailable: true},
		{MemoryPressure: "", PressureAvailable: false},
	}
	if !deferVMStartForHistory(history) {
		t.Fatal("unavailable sample cleared last available critical pressure")
	}

	history = append(history, PerformanceSample{MemoryPressure: "warning", PressureAvailable: true})
	if deferVMStartForHistory(history) {
		t.Fatal("available warning sample did not clear critical-pressure deferral")
	}
}

func TestDoSuspendUsesTartSuspendAndMarksVMResumable(t *testing.T) {
	m := newTestManager(t)
	m.vms["base"] = &VM{Name: "base", State: "running"}
	var gotArgs []string
	m.tartOperation = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		gotArgs = append([]string(nil), args...)
		return nil, nil
	}

	m.doSuspend("base")

	if got := strings.Join(gotArgs, " "); got != "suspend base" {
		t.Fatalf("tart operation = %q, want %q", got, "suspend base")
	}
	if got := m.vms["base"].State; got != "suspended" {
		t.Fatalf("state = %q, want suspended", got)
	}
	if m.busy["base"] {
		t.Fatal("suspend left VM busy")
	}
}

func TestDoGracefulShutdownNeverFallsBackToTartStop(t *testing.T) {
	m := newTestManager(t)
	m.cfg.ShutdownCommand = "sudo shutdown -h now"
	m.cfg.ShutdownWaitSec = 5
	m.cfg.SSHPassword = "admin"
	m.vms["base"] = &VM{Name: "base", State: "running", IP: "192.0.2.10", SSHOK: true}

	var gotCommand, gotPassword string
	m.sshOperation = func(ctx context.Context, name, command, password string) execResult {
		if name != "base" {
			t.Fatalf("SSH target = %q, want base", name)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 5*time.Second {
			t.Fatalf("SSH context deadline = %v, want active shutdown budget", deadline)
		}
		gotCommand, gotPassword = command, password
		return execResult{}
	}
	m.runningProbe = func(string) (bool, error) { return false, nil }
	m.tartOperation = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		t.Fatalf("graceful shutdown called tart %s", strings.Join(args, " "))
		return nil, nil
	}

	m.doGracefulShutdown("base")

	if gotCommand != "sudo shutdown -h now" || gotPassword != "admin" {
		t.Fatalf("SSH command/password = %q/%q", gotCommand, gotPassword)
	}
	if got := m.vms["base"].State; got != "stopped" {
		t.Fatalf("state = %q, want stopped", got)
	}
	if m.busy["base"] {
		t.Fatal("graceful shutdown left VM busy")
	}
}

func TestDoGracefulShutdownLeavesVMRunningWhenStateProbeFails(t *testing.T) {
	m := newTestManager(t)
	m.cfg.ShutdownCommand = "sudo shutdown -h now"
	m.cfg.ShutdownWaitSec = 5
	m.vms["base"] = &VM{Name: "base", State: "running", IP: "192.0.2.10"}
	m.sshOperation = func(context.Context, string, string, string) execResult { return execResult{} }
	m.runningProbe = func(string) (bool, error) { return false, context.DeadlineExceeded }

	m.doGracefulShutdown("base")

	vm := m.vms["base"]
	if vm.State != "running" {
		t.Fatalf("state = %q, want running when status cannot be confirmed", vm.State)
	}
	if !strings.Contains(vm.LastError, "could not confirm shutdown") {
		t.Fatalf("LastError = %q, want probe failure", vm.LastError)
	}
	if m.busy["base"] {
		t.Fatal("probe failure left VM busy")
	}
}

func TestSSHOnDemandIPResolutionUsesCallerShutdownDeadline(t *testing.T) {
	m := newTestManager(t)
	m.vms["base"] = &VM{Name: "base", State: "running"}
	called := false
	m.tartOutputOperation = func(ctx context.Context, _ string, args ...string) (string, error) {
		called = true
		if got := strings.Join(args, " "); got != "ip base --wait 10 --resolver arp" {
			t.Fatalf("resolver command = %q", got)
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 100*time.Millisecond {
			t.Fatalf("resolver deadline = %v, want caller budget", deadline)
		}
		return "", context.DeadlineExceeded
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	result := m.sshExecContext(ctx, "base", "true", "")

	if !called {
		t.Fatal("on-demand Tart IP resolution was not called")
	}
	if !strings.Contains(result.Error, "could not resolve IP") {
		t.Fatalf("error = %q, want resolver failure", result.Error)
	}
}

func TestVMRecoveryActionRoutesRequireAName(t *testing.T) {
	m := newTestManager(t)
	for _, path := range []string{"/api/suspend", "/api/graceful-shutdown"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			res := httptest.NewRecorder()

			m.routes().ServeHTTP(res, req)

			if res.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusBadRequest, res.Body.String())
			}
		})
	}
}

func TestSuspendedVMRemainsActiveForConfigurationChanges(t *testing.T) {
	m := newTestManager(t)
	m.vms["saved"] = &VM{Name: "saved", State: "suspended"}
	m.vms["off"] = &VM{Name: "off", State: "stopped"}

	if !m.isActive("saved") {
		t.Fatal("suspended VM was treated as editable stopped VM")
	}
	if m.isActive("off") {
		t.Fatal("stopped VM was treated as active")
	}
}

func TestExistingStopPathRejectsSuspendedSnapshotState(t *testing.T) {
	if stopAllowedForState("suspended") {
		t.Fatal("suspended VM can enter destructive Stop fallback")
	}
	for _, state := range []string{"running", "stopped"} {
		if !stopAllowedForState(state) {
			t.Fatalf("existing Stop behavior changed for %q", state)
		}
	}
}
