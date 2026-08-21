package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type recordingMDMProfileCopier struct {
	target      mdmTransferTarget
	profile     []byte
	payloadUUID string
	err         error
	called      bool
	checkLock   func() bool
}

func (f *recordingMDMProfileCopier) CopyAndVerify(_ context.Context, target mdmTransferTarget, profile []byte, payloadUUID string) error {
	f.called = true
	f.target = target
	f.profile = append([]byte(nil), profile...)
	f.payloadUUID = payloadUUID
	if f.checkLock != nil && !f.checkLock() {
		return errors.New("manager lock held during profile transfer")
	}
	return f.err
}

func newMDMHandlerManager() *Manager {
	return &Manager{
		cfg: Config{
			JamfBaseURL:        "https://jamf.example",
			JamfInvitationCode: "invite-code",
			SSHUser:            "admin",
			SSHPassword:        "admin",
			SSHTimeoutSec:      15,
		},
		vms: map[string]*VM{
			"base": {Name: "base", State: "running", IP: "192.0.2.10"},
		},
	}
}

func performMDMProfileRequest(t *testing.T, m *Manager, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/api/vm/mdm-profile", strings.NewReader(body))
	rr := httptest.NewRecorder()
	m.handleMDMProfile(rr, req)
	return rr
}

func decodeMDMProfileResponse(t *testing.T, rr *httptest.ResponseRecorder) mdmProfileResponse {
	t.Helper()
	if got := rr.Result().Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type=%q, want application/json; body=%s", got, rr.Body.String())
	}
	var response mdmProfileResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response %q: %v", rr.Body.String(), err)
	}
	return response
}

func assertMDMResponseSecretSafe(t *testing.T, body string) {
	t.Helper()
	for _, secret := range []string{"invite-code", `"password"`, "admin", "underlying-secret", "<plist"} {
		if strings.Contains(body, secret) {
			t.Fatalf("response leaked secret %q: %s", secret, body)
		}
	}
}

func TestHandleMDMProfileSuccess(t *testing.T) {
	m := newMDMHandlerManager()
	fake := &recordingMDMProfileCopier{}
	m.mdmCopier = fake
	m.mdmResolveIP = func(context.Context, string, string) (string, error) {
		t.Fatal("resolver called despite cached VM IP")
		return "", nil
	}

	rr := performMDMProfileRequest(t, m, http.MethodPost, `{"name":"base"}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if fake.target.Address != "192.0.2.10:22" || fake.target.Username != "admin" || fake.target.Password != "admin" {
		t.Fatalf("target=%#v", fake.target)
	}
	if fake.target.Timeout != 15*time.Second {
		t.Fatalf("timeout=%s, want 15s", fake.target.Timeout)
	}
	if !fake.called || fake.payloadUUID == "" || !strings.Contains(string(fake.profile), "invite-code") {
		t.Fatalf("copy arguments missing generated profile: called=%v uuid=%q profile=%q", fake.called, fake.payloadUUID, fake.profile)
	}
	response := decodeMDMProfileResponse(t, rr)
	if !response.OK || response.Name != "base" || response.Path != mdmProfileDisplayPath || response.PayloadUUID != fake.payloadUUID {
		t.Fatalf("response=%#v", response)
	}
	assertMDMResponseSecretSafe(t, rr.Body.String())
}

func TestHandleMDMProfileRouteIsRegistered(t *testing.T) {
	m := newMDMHandlerManager()
	m.mdmCopier = &recordingMDMProfileCopier{}
	req := httptest.NewRequest(http.MethodPost, "/api/vm/mdm-profile", strings.NewReader(`{"name":"base"}`))
	rr := httptest.NewRecorder()

	m.routes().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleMDMProfileRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		mutate     func(*Manager)
		wantStatus int
		wantStage  mdmStage
	}{
		{name: "GET", method: http.MethodGet, body: `{"name":"base"}`, wantStatus: http.StatusMethodNotAllowed},
		{name: "malformed JSON", method: http.MethodPost, body: `{`, wantStatus: http.StatusBadRequest},
		{name: "missing name", method: http.MethodPost, body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "blank name", method: http.MethodPost, body: `{"name":"  "}`, wantStatus: http.StatusBadRequest},
		{
			name:       "missing Jamf URL",
			method:     http.MethodPost,
			body:       `{"name":"base"}`,
			mutate:     func(m *Manager) { m.cfg.JamfBaseURL = "" },
			wantStatus: http.StatusBadRequest,
			wantStage:  mdmStageConfiguration,
		},
		{
			name:       "missing invitation code",
			method:     http.MethodPost,
			body:       `{"name":"base"}`,
			mutate:     func(m *Manager) { m.cfg.JamfInvitationCode = "" },
			wantStatus: http.StatusBadRequest,
			wantStage:  mdmStageConfiguration,
		},
		{
			name:       "missing SSH user",
			method:     http.MethodPost,
			body:       `{"name":"base"}`,
			mutate:     func(m *Manager) { m.cfg.SSHUser = "" },
			wantStatus: http.StatusBadRequest,
			wantStage:  mdmStageConfiguration,
		},
		{
			name:       "missing SSH password",
			method:     http.MethodPost,
			body:       `{"name":"base"}`,
			mutate:     func(m *Manager) { m.cfg.SSHPassword = "" },
			wantStatus: http.StatusBadRequest,
			wantStage:  mdmStageConfiguration,
		},
		{
			name:       "missing VM",
			method:     http.MethodPost,
			body:       `{"name":"missing"}`,
			wantStatus: http.StatusBadRequest,
			wantStage:  mdmStageVM,
		},
		{
			name:       "stopped VM",
			method:     http.MethodPost,
			body:       `{"name":"base"}`,
			mutate:     func(m *Manager) { m.vms["base"].State = "stopped" },
			wantStatus: http.StatusBadRequest,
			wantStage:  mdmStageVM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMDMHandlerManager()
			fake := &recordingMDMProfileCopier{}
			m.mdmCopier = fake
			if tt.mutate != nil {
				tt.mutate(m)
			}
			rr := performMDMProfileRequest(t, m, tt.method, tt.body)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status=%d body=%s, want %d", rr.Code, rr.Body.String(), tt.wantStatus)
			}
			response := decodeMDMProfileResponse(t, rr)
			if response.OK || response.Error == "" || response.Stage != tt.wantStage {
				t.Fatalf("response=%#v, want failed stage %q", response, tt.wantStage)
			}
			if fake.called {
				t.Fatal("copier called for invalid request")
			}
			assertMDMResponseSecretSafe(t, rr.Body.String())
		})
	}
}

func TestHandleMDMProfileResolvesMissingIPWithoutHoldingManagerLock(t *testing.T) {
	m := newMDMHandlerManager()
	m.vms["base"].IP = ""
	fake := &recordingMDMProfileCopier{}
	m.mdmCopier = fake
	var gotName, gotHome string
	m.mdmResolveIP = func(_ context.Context, name, home string) (string, error) {
		gotName, gotHome = name, home
		if !m.mu.TryLock() {
			t.Fatal("manager lock held during IP resolution")
		}
		m.mu.Unlock()
		return "198.51.100.12", nil
	}
	m.cfg.VMStoragePath = "/tmp/tart-home"

	rr := performMDMProfileRequest(t, m, http.MethodPost, `{"name":" base "}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if gotName != "base" || gotHome != "/tmp/tart-home" {
		t.Fatalf("resolver arguments=(%q, %q)", gotName, gotHome)
	}
	if fake.target.Address != "198.51.100.12:22" {
		t.Fatalf("address=%q", fake.target.Address)
	}
}

func TestHandleMDMProfileReportsInjectedIPFailure(t *testing.T) {
	m := newMDMHandlerManager()
	m.vms["base"].IP = ""
	m.mdmCopier = &recordingMDMProfileCopier{}
	m.mdmResolveIP = func(context.Context, string, string) (string, error) {
		return "", errors.New("underlying-secret")
	}

	rr := performMDMProfileRequest(t, m, http.MethodPost, `{"name":"base"}`)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	response := decodeMDMProfileResponse(t, rr)
	if response.Stage != mdmStageIP || response.Error == "" {
		t.Fatalf("response=%#v", response)
	}
	assertMDMResponseSecretSafe(t, rr.Body.String())
}

func TestHandleMDMProfileUsesPerVMCredentialsWithoutHoldingManagerLock(t *testing.T) {
	m := newMDMHandlerManager()
	m.vms["base"].SSHUser = "builder"
	m.vms["base"].SSHPassword = "builder-pass"
	fake := &recordingMDMProfileCopier{
		checkLock: func() bool {
			if !m.mu.TryLock() {
				return false
			}
			m.mu.Unlock()
			return true
		},
	}
	m.mdmCopier = fake

	rr := performMDMProfileRequest(t, m, http.MethodPost, `{"name":"base"}`)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if fake.target.Username != "builder" || fake.target.Password != "builder-pass" {
		t.Fatalf("target=%#v", fake.target)
	}
	assertMDMResponseSecretSafe(t, rr.Body.String())
}

func TestHandleMDMProfileMapsTypedStageErrorsSafely(t *testing.T) {
	tests := []struct {
		stage      mdmStage
		wantStatus int
		wantError  string
	}{
		{stage: mdmStageConfiguration, wantStatus: http.StatusBadRequest, wantError: "profile configuration is incomplete"},
		{stage: mdmStageVM, wantStatus: http.StatusBadRequest, wantError: "VM is not available"},
		{stage: mdmStageIP, wantStatus: http.StatusBadGateway, wantError: "could not resolve VM IP"},
		{stage: mdmStageAuthentication, wantStatus: http.StatusBadGateway, wantError: "SSH authentication failed"},
		{stage: mdmStageSFTP, wantStatus: http.StatusBadGateway, wantError: "SFTP upload failed"},
		{stage: mdmStageVerification, wantStatus: http.StatusBadGateway, wantError: "uploaded profile verification failed"},
	}

	for _, tt := range tests {
		t.Run(string(tt.stage), func(t *testing.T) {
			m := newMDMHandlerManager()
			m.mdmCopier = &recordingMDMProfileCopier{
				err: &mdmStageError{Stage: tt.stage, Err: errors.New("underlying-secret")},
			}

			rr := performMDMProfileRequest(t, m, http.MethodPost, `{"name":"base"}`)

			if rr.Code != tt.wantStatus {
				t.Fatalf("status=%d body=%s, want %d", rr.Code, rr.Body.String(), tt.wantStatus)
			}
			response := decodeMDMProfileResponse(t, rr)
			if response.Stage != tt.stage || response.Error != tt.wantError || response.OK {
				t.Fatalf("response=%#v", response)
			}
			assertMDMResponseSecretSafe(t, rr.Body.String())
		})
	}
}

func TestHandleMDMProfileMissingDependenciesFailSafely(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manager)
	}{
		{name: "copier", mutate: func(m *Manager) { m.mdmCopier = nil }},
		{name: "resolver", mutate: func(m *Manager) {
			m.vms["base"].IP = ""
			m.mdmResolveIP = nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMDMHandlerManager()
			m.mdmCopier = &recordingMDMProfileCopier{}
			m.mdmResolveIP = func(context.Context, string, string) (string, error) { return "192.0.2.10", nil }
			tt.mutate(m)
			rr := performMDMProfileRequest(t, m, http.MethodPost, `{"name":"base"}`)
			if rr.Code < 400 {
				t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
			}
			response := decodeMDMProfileResponse(t, rr)
			if response.OK || response.Error == "" {
				t.Fatalf("response=%#v", response)
			}
			assertMDMResponseSecretSafe(t, rr.Body.String())
		})
	}
}

func TestResolveMDMIPWithTartHonorsRequestCancellation(t *testing.T) {
	m := &Manager{cfg: Config{TartAppPath: "/usr/bin/false"}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.resolveMDMIPWithTart(ctx, "base", "/tmp/tart-home")

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error=%v, want context cancellation", err)
	}
}
