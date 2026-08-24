package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

const (
	// authorizedKeysPath is home-relative: SFTP sessions start in the user's home
	// directory, the same assumption the MDM profile copy already relies on.
	authorizedKeysPath = ".ssh/authorized_keys"

	sshKeyInitialDelay = 30 * time.Second
	sshKeyMaxBackoff   = 2 * time.Minute
	sshKeyDeadline     = 10 * time.Minute
)

// keyProvisionInFlight tracks VMs with a provisioning attempt running. It is kept
// separate from m.busy so key work never blocks Run/Stop.
var keyProvisionInFlight sync.Map

// ensureSSHKeyPair returns the public key for the identity at path, generating an
// ed25519 pair when the private key does not exist yet. An existing key is never
// overwritten: the public half is re-derived from it if the .pub file is missing.
func ensureSSHKeyPair(path string) ([]byte, error) {
	path = expandHome(strings.TrimSpace(path))
	if path == "" {
		return nil, errors.New("no SSH identity file configured")
	}
	publicPath := path + ".pub"

	if raw, err := os.ReadFile(path); err == nil {
		if pub, err := os.ReadFile(publicPath); err == nil && len(bytes.TrimSpace(pub)) > 0 {
			return bytes.TrimSpace(pub), nil
		}
		signer, err := ssh.ParsePrivateKey(raw)
		if err != nil {
			return nil, fmt.Errorf("parse existing SSH private key: %w", err)
		}
		pub := bytes.TrimSpace(ssh.MarshalAuthorizedKey(signer.PublicKey()))
		if err := os.WriteFile(publicPath, append(pub, '\n'), 0o644); err != nil {
			return nil, fmt.Errorf("write SSH public key: %w", err)
		}
		return pub, nil
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate SSH key: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(privateKey, "tart-oven")
	if err != nil {
		return nil, fmt.Errorf("encode SSH private key: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create SSH key directory: %w", err)
		}
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, fmt.Errorf("write SSH private key: %w", err)
	}
	sshPublic, err := ssh.NewPublicKey(publicKey)
	if err != nil {
		return nil, fmt.Errorf("derive SSH public key: %w", err)
	}
	pub := bytes.TrimSpace(ssh.MarshalAuthorizedKey(sshPublic))
	if err := os.WriteFile(publicPath, append(pub, '\n'), 0o644); err != nil {
		return nil, fmt.Errorf("write SSH public key: %w", err)
	}
	return pub, nil
}

// eligibleForKeyProvisioning reports whether a VM should receive the public key.
// SSHOK is the gate: it records whether key authentication already works, so a guest
// that is reachable by key is never touched.
func eligibleForKeyProvisioning(vm *VM, excluded map[string]bool, busy bool) bool {
	if vm == nil || busy || vm.SSHOK {
		return false
	}
	if vm.State != "running" || strings.TrimSpace(vm.IP) == "" {
		return false
	}
	if excluded[vm.Name] {
		return false
	}
	if strings.Contains(vm.Name, templateMarker) {
		return false
	}
	return !strings.EqualFold(strings.TrimSpace(vm.Source), "OCI")
}

// installAuthorizedKey appends publicKey to the guest's authorized_keys unless it is
// already there, preserving any keys the guest already trusts. It reports whether a
// write happened. WriteFile creates .ssh itself; the default directory mode satisfies
// sshd StrictModes, which only rejects group- or world-writable paths.
func installAuthorizedKey(remote remoteProfileFS, publicKey []byte) (bool, error) {
	key := bytes.TrimSpace(publicKey)
	if len(key) == 0 {
		return false, errors.New("public key is empty")
	}
	existing, err := remote.ReadFile(authorizedKeysPath)
	if err != nil {
		existing = nil // an absent authorized_keys is the normal first-run case
	}
	for _, line := range bytes.Split(existing, []byte("\n")) {
		if bytes.Equal(bytes.TrimSpace(line), key) {
			return false, nil
		}
	}
	merged := bytes.TrimRight(existing, "\n")
	if len(merged) > 0 {
		merged = append(merged, '\n')
	}
	merged = append(merged, key...)
	merged = append(merged, '\n')
	if err := remote.WriteFile(authorizedKeysPath, merged, 0o600); err != nil {
		return false, fmt.Errorf("write authorized_keys: %w", err)
	}
	return true, nil
}

// sshKeyAuthRejected reports whether err means the stored credentials were refused.
// Those failures are terminal — retrying cannot help and would hammer the guest —
// while transport errors (sshd still booting) are worth retrying.
func sshKeyAuthRejected(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "unable to authenticate") ||
		strings.Contains(text, "permission denied") ||
		strings.Contains(text, "no supported methods remain")
}

// provisionVMKey performs one provisioning attempt against a single guest.
func (m *Manager) provisionVMKey(ctx context.Context, name string, target mdmTransferTarget, publicKey []byte) error {
	m.mu.Lock()
	dialer := m.keyDialer
	m.mu.Unlock()
	if dialer == nil {
		dialer = &sshSFTPProfileDialer{}
	}

	remote, err := dialer.Dial(ctx, target)
	if err != nil {
		return err
	}
	wrote, installErr := installAuthorizedKey(remote, publicKey)
	closeErr := remote.Close()
	if installErr != nil {
		return installErr
	}
	if closeErr != nil {
		return closeErr
	}

	m.mu.Lock()
	if vm := m.vms[name]; vm != nil {
		vm.SSHKeyInstalledAt = time.Now()
		vm.SSHKeyError = ""
	}
	m.save()
	m.mu.Unlock()
	if wrote {
		m.logln("ssh key installed on %s", name)
	}
	return nil
}

// provisionSSHKeys runs one scan pass: every eligible VM without working key auth is
// handed to a bounded background attempt. Called from the monitor loop.
func (m *Manager) provisionSSHKeys() {
	m.mu.Lock()
	if !m.cfg.AutoInstallSSHKey {
		m.mu.Unlock()
		return
	}
	cfg := m.cfg
	excluded := make(map[string]bool, len(cfg.Excluded))
	for _, name := range cfg.Excluded {
		excluded[strings.TrimSpace(name)] = true
	}
	type candidate struct {
		name     string
		ip       string
		username string
		password string
	}
	var candidates []candidate
	for name, vm := range m.vms {
		if !eligibleForKeyProvisioning(vm, excluded, m.busy[name]) {
			continue
		}
		username, password := effectiveSSHCredentials(cfg, vm)
		if strings.TrimSpace(username) == "" || password == "" {
			continue
		}
		candidates = append(candidates, candidate{name: name, ip: vm.IP, username: username, password: password})
	}
	m.mu.Unlock()

	for _, c := range candidates {
		if _, running := keyProvisionInFlight.LoadOrStore(c.name, true); running {
			continue
		}
		go func(c candidate) {
			defer keyProvisionInFlight.Delete(c.name)
			m.provisionWithRetry(c.name, c.ip, c.username, c.password)
		}(c)
	}
}

// provisionWithRetry waits for sshd, then retries with backoff until the key is
// installed, the credentials are refused, or the deadline passes.
func (m *Manager) provisionWithRetry(name, ip, username, password string) {
	m.mu.Lock()
	keyPath := m.cfg.SSHKey
	timeoutSec := m.cfg.SSHTimeoutSec
	m.mu.Unlock()

	publicKey, err := ensureSSHKeyPair(keyPath)
	if err != nil {
		m.recordKeyError(name, err)
		return
	}
	if timeoutSec < 1 {
		timeoutSec = 30
	}

	ctx, cancel := context.WithTimeout(context.Background(), sshKeyDeadline)
	defer cancel()

	target := mdmTransferTarget{
		Address:  net.JoinHostPort(ip, "22"),
		Username: username,
		Password: password,
		Timeout:  time.Duration(timeoutSec) * time.Second,
	}

	delay := sshKeyInitialDelay
	for {
		select {
		case <-ctx.Done():
			m.recordKeyError(name, errors.New("timed out waiting for the guest to accept the SSH key"))
			return
		case <-time.After(delay):
		}

		err := m.provisionVMKey(ctx, name, target, publicKey)
		if err == nil {
			return
		}
		if sshKeyAuthRejected(err) {
			m.recordKeyError(name, errors.New("guest rejected the configured SSH credentials"))
			return
		}
		if delay *= 2; delay > sshKeyMaxBackoff {
			delay = sshKeyMaxBackoff
		}
	}
}

// recordKeyError stores a provisioning failure for display without logging secrets.
func (m *Manager) recordKeyError(name string, err error) {
	m.mu.Lock()
	if vm := m.vms[name]; vm != nil {
		vm.SSHKeyError = err.Error()
	}
	m.save()
	m.mu.Unlock()
	m.logln("ssh key provisioning failed for %s: %v", name, err)
	m.broadcast()
}
