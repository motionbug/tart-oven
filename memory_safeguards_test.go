package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMaybeReleaseGoMemoryOnlyReleasesMeaningfulIdleHeap(t *testing.T) {
	tests := []struct {
		name         string
		idleBytes    uint64
		releaseBytes uint64
		wantFreed    bool
	}{
		{
			name:         "retained heap above threshold",
			idleBytes:    96 << 20,
			releaseBytes: 16 << 20,
			wantFreed:    true,
		},
		{
			name:         "small retained heap",
			idleBytes:    32 << 20,
			releaseBytes: 8 << 20,
			wantFreed:    false,
		},
		{
			name:         "idle heap already returned",
			idleBytes:    96 << 20,
			releaseBytes: 96 << 20,
			wantFreed:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			memory := &fakeGoMemory{idle: tt.idleBytes, released: tt.releaseBytes}
			gotBytes, gotFreed := maybeReleaseGoMemory(memory)
			if gotFreed != tt.wantFreed {
				t.Fatalf("released = %v, want %v", gotFreed, tt.wantFreed)
			}
			wantBytes := tt.idleBytes - tt.releaseBytes
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
	idle      uint64
	released  uint64
	freeCalls int
}

func (f *fakeGoMemory) HeapMemory() (uint64, uint64) { return f.idle, f.released }
func (f *fakeGoMemory) FreeOSMemory()                { f.freeCalls++ }

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

func TestVMRecoveryActionRoutesRequireAName(t *testing.T) {
	m := newTestManager(t)
	for _, path := range []string{"/api/suspend"} {
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
