package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestNormalizeJamfBaseURL(t *testing.T) {
	tests := []struct {
		name, input, want string
		wantErr           bool
	}{
		{"empty before setup", "  ", "", false},
		{"trim HTTPS", " https://example.jamfcloud.com/// ", "https://example.jamfcloud.com", false},
		{"allow HTTP", "http://jamf.test/", "http://jamf.test", false},
		{"missing scheme", "example.jamfcloud.com", "", true},
		{"wrong scheme", "ftp://example.test", "", true},
		{"missing host", "https:///enroll", "", true},
		{"port without hostname", "https://:443", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeJamfBaseURL(tt.input)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("got %q, %v; want %q, error=%v", got, err, tt.want, tt.wantErr)
			}
		})
	}
}

func TestEffectiveSSHCredentials(t *testing.T) {
	cfg := Config{SSHUser: "admin", SSHPassword: "admin"}
	if user, pass := effectiveSSHCredentials(cfg, &VM{}); user != "admin" || pass != "admin" {
		t.Fatalf("global credentials = %q/%q", user, pass)
	}
	vm := &VM{SSHUser: "builder", SSHPassword: "builder-pass"}
	if user, pass := effectiveSSHCredentials(cfg, vm); user != "builder" || pass != "builder-pass" {
		t.Fatalf("per-VM credentials = %q/%q", user, pass)
	}
}

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	return &Manager{
		cfg:         defaultConfig(),
		vms:         make(map[string]*VM),
		busy:        make(map[string]bool),
		opStart:     make(map[string]time.Time),
		runningCmds: make(map[string]*exec.Cmd),
		subs:        make(map[chan []byte]struct{}),
		statePath:   filepath.Join(t.TempDir(), "state.json"),
		reload:      make(chan struct{}, 1),
	}
}

func TestNewConfigView(t *testing.T) {
	view := newConfigView(Config{SSHPassword: "saved-password", JamfInvitationCode: "saved-invite"})
	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "saved-password") || strings.Contains(string(data), "saved-invite") {
		t.Fatalf("client view leaked secrets: %s", data)
	}
	if !view.SSHPasswordSet || !view.JamfInvitationCodeSet {
		t.Fatal("missing saved flags")
	}
}

func TestHandleConfigBlankSecrets(t *testing.T) {
	m := newTestManager(t)
	m.cfg.JamfInvitationCode = "saved-invite"
	m.cfg.SSHPassword = "saved-password"

	body, err := json.Marshal(Config{JamfBaseURL: "https://new.example/", Paused: true})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/config", bytes.NewReader(body))
	res := httptest.NewRecorder()
	m.handleConfig(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if m.cfg.JamfInvitationCode != "saved-invite" || m.cfg.SSHPassword != "saved-password" {
		t.Fatalf("blank secrets overwrote saved values: %#v", m.cfg)
	}
	if m.cfg.JamfBaseURL != "https://new.example" {
		t.Fatalf("JamfBaseURL = %q, want normalized URL", m.cfg.JamfBaseURL)
	}
}

func TestHandleConfigPreservesOCIExclusionWhenFieldIsOmitted(t *testing.T) {
	m := newTestManager(t)
	m.cfg.ExcludeOCIFromScheduler = true
	req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(`{"listen":"127.0.0.1:9000","intervalMinutes":5,"windowMinutes":120,"maxConcurrent":1}`))
	res := httptest.NewRecorder()

	m.handleConfig(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
	}
	if !m.cfg.ExcludeOCIFromScheduler {
		t.Fatal("an older client omitting excludeOciFromScheduler disabled the safe default")
	}
}

func TestHandleConfigAppliesExplicitOCIExclusionValues(t *testing.T) {
	for _, want := range []bool{false, true} {
		t.Run(strings.ToUpper(strconv.FormatBool(want)), func(t *testing.T) {
			m := newTestManager(t)
			m.cfg.ExcludeOCIFromScheduler = !want
			body := fmt.Sprintf(`{"listen":"127.0.0.1:9000","intervalMinutes":5,"windowMinutes":120,"maxConcurrent":1,"excludeOciFromScheduler":%t}`, want)
			req := httptest.NewRequest(http.MethodPost, "/api/config", strings.NewReader(body))
			res := httptest.NewRecorder()

			m.handleConfig(res, req)

			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d: %s", res.Code, http.StatusOK, res.Body.String())
			}
			if m.cfg.ExcludeOCIFromScheduler != want {
				t.Fatalf("ExcludeOCIFromScheduler = %v, want %v", m.cfg.ExcludeOCIFromScheduler, want)
			}
		})
	}
}
