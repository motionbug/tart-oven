package main

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"syscall"
)

var ociImageRegex = regexp.MustCompile(`^[a-zA-Z0-9_.-]+(/[a-zA-Z0-9_.-]+)+(:[a-zA-Z0-9_.-]+)?$`)

// minPullFreeBytes is 25 GiB
const minPullFreeBytes uint64 = 25 * 1024 * 1024 * 1024

// validateOCIImageURI validates and sanitizes an OCI image reference.
func validateOCIImageURI(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("image URI cannot be empty")
	}
	if strings.ContainsAny(raw, " \t\n\r;|&`$><(){}[]\\\"'") {
		return "", errors.New("image URI contains invalid characters")
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return "", errors.New("image URI must not include http:// or https:// protocol scheme")
	}
	if !ociImageRegex.MatchString(raw) {
		return "", fmt.Errorf("invalid OCI image reference format: %q", raw)
	}
	return raw, nil
}

// checkFreeDiskSpace checks available bytes on the filesystem backing path.
func checkFreeDiskSpace(path string, minBytes uint64) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	if freeBytes < minBytes {
		return freeBytes, fmt.Errorf("insufficient disk space: %d GB available, requires at least %d GB", freeBytes/(1024*1024*1024), minBytes/(1024*1024*1024))
	}
	return freeBytes, nil
}
