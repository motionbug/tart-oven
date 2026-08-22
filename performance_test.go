package main

import "testing"

func TestAppendPerformanceSampleKeepsNewest1440(t *testing.T) {
	var history []PerformanceSample
	for i := 0; i < performanceHistoryLimit+2; i++ {
		history = appendPerformanceSample(history, PerformanceSample{UptimeSeconds: uint64(i)})
	}
	if len(history) != performanceHistoryLimit {
		t.Fatalf("length = %d", len(history))
	}
	if history[0].UptimeSeconds != 2 || history[len(history)-1].UptimeSeconds != 1441 {
		t.Fatalf("wrong retained range: %d..%d", history[0].UptimeSeconds, history[len(history)-1].UptimeSeconds)
	}
}

func TestAppendPerformanceSampleDoesNotMutateInputAtCapacity(t *testing.T) {
	history := make([]PerformanceSample, performanceHistoryLimit, performanceHistoryLimit)
	history[0].UptimeSeconds = 7
	next := appendPerformanceSample(history, PerformanceSample{UptimeSeconds: 99})
	if history[0].UptimeSeconds != 7 || next[len(next)-1].UptimeSeconds != 99 {
		t.Fatal("input mutated")
	}
}
