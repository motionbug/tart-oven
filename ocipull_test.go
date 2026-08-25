package main

import (
	"os"
	"strings"
	"testing"
)

func TestValidateOCIImageURI(t *testing.T) {
	valid := []string{
		"ghcr.io/cirruslabs/macos-sonoma-base:latest",
		"ghcr.io/cirruslabs/macos-sequoia-base:latest",
		"ghcr.io/cirruslabs/macos-tahoe-base:latest",
		"docker.io/library/macos:15",
		"registry.internal.corp/team/macos-runner:v1.2",
	}
	for _, u := range valid {
		got, err := validateOCIImageURI(u)
		if err != nil || got != u {
			t.Errorf("validateOCIImageURI(%q) unexpected error: %v, got: %q", u, err, got)
		}
	}

	invalid := []string{
		"",
		"   ",
		"ghcr.io/cirruslabs/macos-sonoma-base; rm -rf /",
		"image with spaces",
		"ghcr.io/`touch bad`",
		"image$(whoami)",
		"http://ghcr.io/image:latest",
	}
	for _, u := range invalid {
		if _, err := validateOCIImageURI(u); err == nil {
			t.Errorf("validateOCIImageURI(%q) expected error but passed", u)
		}
	}
}

func TestCheckFreeDiskSpace(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "ocipull-disk-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Host should have at least 1000 bytes free
	free, err := checkFreeDiskSpace(tempDir, 1000)
	if err != nil {
		t.Fatalf("checkFreeDiskSpace unexpected error: %v", err)
	}
	if free == 0 {
		t.Errorf("expected non-zero free space")
	}

	// Insufficient space check with an impossibly large requirement (100 Petabytes)
	const impossible = 100 * 1024 * 1024 * 1024 * 1024 * 1024
	_, err = checkFreeDiskSpace(tempDir, impossible)
	if err == nil || !strings.Contains(err.Error(), "insufficient disk space") {
		t.Errorf("expected insufficient disk space error, got: %v", err)
	}
}
