package main

import (
	"strings"
	"testing"
)

func TestDashboardContainsJamfProfileControls(t *testing.T) {
	b, err := content.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{
		`id="jamfBaseUrl"`, `id="jamfInvitationCode"`, `id="sshUser"`,
		`id="sshPassword"`, `id="mdmTarget"`, `id="saveJamfBtn"`,
		`id="copyMdmBtn"`, `/api/vm/mdm-profile`, `~/Desktop/mdm_enroll.mobileconfig`,
		`placeholder="https://tenant.jamfcloud.com"`, `Enter the value after invitation=, not the full URL`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}

func TestDashboardExplainsPreparedBaseCloneWorkflow(t *testing.T) {
	b, err := content.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{
		"Do not install or enroll the base VM",
		"Install the profile separately inside each clone",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing Jamf base workflow guidance %q", want)
		}
	}
}

func TestDashboardKeepsMdmCopyDisabledWhileInFlight(t *testing.T) {
	b, err := content.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{
		"let mdmCopyInFlight = false;",
		"copyBtn.disabled = mdmCopyInFlight || !hasRunningVM;",
		"if (mdmCopyInFlight) return;",
		"mdmCopyInFlight = true;",
		"mdmCopyInFlight = false;\n    updateMdmCopyButton();",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing MDM copy in-flight guard %q", want)
		}
	}
}

func TestDashboardShowsSafeConfigValidationMessage(t *testing.T) {
	b, err := content.ReadFile("index.html")
	if err != nil {
		t.Fatal(err)
	}
	html := string(b)
	for _, want := range []string{
		`const errorText = res.ok ? "" : await res.text();`,
		`res.ok ? "saved ✓" : (errorText || "save failed")`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard does not display config validation response: missing %q", want)
		}
	}
}
