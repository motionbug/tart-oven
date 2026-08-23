package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestReleaseVersion131IsConsistent(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	changelog, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.31" {
		t.Fatalf("version = %q", version)
	}
	if !strings.Contains(string(readme), "Current release: **1.31**") {
		t.Fatal("README release mismatch")
	}
	if !strings.Contains(string(changelog), "## 1.31") {
		t.Fatal("CHANGELOG release missing")
	}
}

func TestReleaseBinaryReportsVersion131(t *testing.T) {
	output, err := exec.Command("./tart-oven", "-version").CombinedOutput()
	if err != nil {
		t.Fatalf("run tracked executable: %v\n%s", err, output)
	}
	if got, want := string(output), "1.31\n"; got != want {
		t.Fatalf("tracked executable version output = %q, want %q", got, want)
	}
}
