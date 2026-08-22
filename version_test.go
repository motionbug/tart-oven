package main

import (
	"os"
	"strings"
	"testing"
)

func TestReleaseVersion130IsConsistent(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	changelog, err := os.ReadFile("CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.30" {
		t.Fatalf("version = %q", version)
	}
	if !strings.Contains(string(readme), "Current release: **1.30**") {
		t.Fatal("README release mismatch")
	}
	if !strings.Contains(string(changelog), "## 1.30") {
		t.Fatal("CHANGELOG release missing")
	}
}
