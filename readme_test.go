package main

import (
	"os"
	"strings"
	"testing"
)

func TestReadmeContains8StagesAndMDMRandomization(t *testing.T) {
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	content := string(b)

	stages := []string{
		"## Stage 1: Welcome & Value Proposition",
		"## Stage 2: Quickstart 5-Minute Onboarding Guide",
		"## Stage 3: Base Image Management & OCI Registry Workflow",
		"## Stage 4: Daily Fleet Operations, Screen Sharing & Automation Scheduler",
		"## Stage 5: Jamf Pro & MDM Administrator Toolkit",
		"## Stage 6: Host Performance, Kernel Safeguards & Hardware Tuning",
		"## Stage 7: Automation & REST / SSE API Reference",
		"## Stage 8: Diagnostic Runbooks & Troubleshooting FAQ",
	}

	for _, s := range stages {
		if !strings.Contains(content, s) {
			t.Errorf("README.md missing stage heading: %s", s)
		}
	}

	runbooks := []string{
		"### Runbook 1: Jamf Device Record Collision & Serial Duplication Triage",
		"### Runbook 2: VM Reports \"No IP address after 60s\" / Bridge DHCP Timeout",
		"### Runbook 3: Guest Agent Reachability vs. SSH Fallback Failures",
		"### Runbook 4: Screen Sharing (VNC) Connection Errors",
		"### Runbook 5: LaunchAgent Daemon Management & Permissions",
		"### Runbook 6: Critical Memory Pressure Start Deferral",
	}

	for _, r := range runbooks {
		if !strings.Contains(content, r) {
			t.Errorf("README.md missing runbook heading: %s", r)
		}
	}

	if !strings.Contains(content, "--random-serial") || !strings.Contains(content, "--random-mac") {
		t.Errorf("README.md must explicitly document --random-serial and --random-mac for MDM cloning")
	}

	if !strings.Contains(content, "tart set") {
		t.Errorf("README.md must document tart set for configuring hardware randomization")
	}
}
