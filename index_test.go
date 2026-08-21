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
	} {
		if !strings.Contains(html, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
}
