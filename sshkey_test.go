package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestEnsureSSHKeyPairGeneratesWhenMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tart-oven")
	pub, err := ensureSSHKeyPair(path)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.HasPrefix(string(pub), "ssh-ed25519 ") {
		t.Fatalf("public key = %q", pub)
	}
	if _, _, _, _, err := ssh.ParseAuthorizedKey(pub); err != nil {
		t.Fatalf("public key does not parse: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	if _, err := ssh.ParsePrivateKey(raw); err != nil {
		t.Fatalf("private key does not parse: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("private key mode = %o, want 600", mode)
	}
}

func TestEnsureSSHKeyPairNeverOverwritesAnExistingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tart-oven")
	first, err := ensureSSHKeyPair(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureSSHKeyPair(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("existing key was regenerated")
	}
}

func TestEligibleForKeyProvisioning(t *testing.T) {
	base := func() *VM {
		return &VM{Name: "runner1", State: "running", IP: "192.168.1.10"}
	}
	for _, tc := range []struct {
		name     string
		mutate   func(*VM)
		excluded map[string]bool
		busy     bool
		want     bool
	}{
		{name: "healthy candidate", mutate: func(*VM) {}, want: true},
		{name: "key already works", mutate: func(v *VM) { v.SSHOK = true }},
		{name: "not running", mutate: func(v *VM) { v.State = "stopped" }},
		{name: "no ip", mutate: func(v *VM) { v.IP = "" }},
		{name: "busy", mutate: func(*VM) {}, busy: true},
		{name: "excluded", mutate: func(*VM) {}, excluded: map[string]bool{"runner1": true}},
		{name: "template", mutate: func(v *VM) { v.Name = "base-TEMPLATE" }},
		{name: "oci image", mutate: func(v *VM) { v.Source = "OCI" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := base()
			tc.mutate(vm)
			if got := eligibleForKeyProvisioning(vm, tc.excluded, tc.busy); got != tc.want {
				t.Fatalf("eligible = %v, want %v", got, tc.want)
			}
		})
	}
}

type fakeGuestFS struct {
	files  map[string][]byte
	modes  map[string]os.FileMode
	closed bool
}

func newFakeGuestFS() *fakeGuestFS {
	return &fakeGuestFS{files: map[string][]byte{}, modes: map[string]os.FileMode{}}
}

func (f *fakeGuestFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	f.files[path] = append([]byte(nil), data...)
	f.modes[path] = perm
	return nil
}

func (f *fakeGuestFS) ReadFile(path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (f *fakeGuestFS) Close() error { f.closed = true; return nil }

func TestInstallAuthorizedKeyCreatesTheFileWhenAbsent(t *testing.T) {
	remote := newFakeGuestFS()
	wrote, err := installAuthorizedKey(remote, []byte("ssh-ed25519 AAAAKEY tart-oven"))
	if err != nil || !wrote {
		t.Fatalf("wrote = %v, err = %v", wrote, err)
	}
	if got := string(remote.files[".ssh/authorized_keys"]); got != "ssh-ed25519 AAAAKEY tart-oven\n" {
		t.Fatalf("authorized_keys = %q", got)
	}
	if mode := remote.modes[".ssh/authorized_keys"]; mode != 0o600 {
		t.Fatalf("mode = %o, want 600", mode)
	}
}

func TestInstallAuthorizedKeyPreservesExistingKeys(t *testing.T) {
	remote := newFakeGuestFS()
	remote.files[".ssh/authorized_keys"] = []byte("ssh-rsa SOMEONEELSE laptop")
	if _, err := installAuthorizedKey(remote, []byte("ssh-ed25519 AAAAKEY tart-oven")); err != nil {
		t.Fatal(err)
	}
	got := string(remote.files[".ssh/authorized_keys"])
	want := "ssh-rsa SOMEONEELSE laptop\nssh-ed25519 AAAAKEY tart-oven\n"
	if got != want {
		t.Fatalf("authorized_keys = %q, want %q", got, want)
	}
}

func TestInstallAuthorizedKeyIsANoOpWhenAlreadyPresent(t *testing.T) {
	remote := newFakeGuestFS()
	remote.files[".ssh/authorized_keys"] = []byte("ssh-ed25519 AAAAKEY tart-oven\n")
	wrote, err := installAuthorizedKey(remote, []byte("ssh-ed25519 AAAAKEY tart-oven"))
	if err != nil {
		t.Fatal(err)
	}
	if wrote {
		t.Fatal("rewrote authorized_keys even though the key was present")
	}
}

type fakeGuestDialer struct {
	fs      *fakeGuestFS
	err     error
	dialled int
}

func (d *fakeGuestDialer) Dial(context.Context, mdmTransferTarget) (remoteProfileFS, error) {
	d.dialled++
	if d.err != nil {
		return nil, d.err
	}
	return d.fs, nil
}

func TestSSHKeyAuthRejectedDistinguishesCredentialFailures(t *testing.T) {
	if !sshKeyAuthRejected(errors.New("authenticate SSH connection: ssh: unable to authenticate")) {
		t.Fatal("wrong password must be classified as an auth rejection")
	}
	if sshKeyAuthRejected(errors.New("open SSH connection: dial tcp 10.0.0.5:22: connect: connection refused")) {
		t.Fatal("connection refused must stay retryable")
	}
}

func TestProvisionVMKeyInstallsAndClosesTheSession(t *testing.T) {
	remote := newFakeGuestFS()
	dialer := &fakeGuestDialer{fs: remote}
	m := &Manager{vms: map[string]*VM{"runner1": {Name: "runner1"}}, busy: map[string]bool{},
		statePath: filepath.Join(t.TempDir(), "state.json"), keyDialer: dialer}

	err := m.provisionVMKey(context.Background(), "runner1",
		mdmTransferTarget{Address: "10.0.0.5:22", Username: "admin", Password: "admin"},
		[]byte("ssh-ed25519 AAAAKEY tart-oven"))
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if _, ok := remote.files[".ssh/authorized_keys"]; !ok {
		t.Fatal("key was not installed")
	}
	if !remote.closed {
		t.Fatal("session was not closed")
	}
	if m.vms["runner1"].SSHKeyInstalledAt.IsZero() {
		t.Fatal("install time was not recorded")
	}
}

func TestProvisionSSHKeysDoesNothingWhenDisabled(t *testing.T) {
	dialer := &fakeGuestDialer{fs: newFakeGuestFS()}
	m := &Manager{
		cfg:       Config{AutoInstallSSHKey: false, SSHUser: "admin", SSHPassword: "admin"},
		vms:       map[string]*VM{"runner1": {Name: "runner1", State: "running", IP: "10.0.0.5"}},
		busy:      map[string]bool{},
		keyDialer: dialer,
		statePath: filepath.Join(t.TempDir(), "state.json"),
	}
	m.provisionSSHKeys()
	if dialer.dialled != 0 {
		t.Fatalf("dialled %d times while disabled", dialer.dialled)
	}
}

func TestProvisionSSHKeysSkipsGuestsThatAlreadyHaveKeyAuth(t *testing.T) {
	dialer := &fakeGuestDialer{fs: newFakeGuestFS()}
	m := &Manager{
		cfg:       Config{AutoInstallSSHKey: true, SSHUser: "admin", SSHPassword: "admin", SSHKey: filepath.Join(t.TempDir(), "id")},
		vms:       map[string]*VM{"runner1": {Name: "runner1", State: "running", IP: "10.0.0.5", SSHOK: true}},
		busy:      map[string]bool{},
		keyDialer: dialer,
		statePath: filepath.Join(t.TempDir(), "state.json"),
	}
	m.provisionSSHKeys()
	if dialer.dialled != 0 {
		t.Fatalf("dialled %d times for a guest whose key already works", dialer.dialled)
	}
}
