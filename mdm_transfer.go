package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type mdmStage string

const (
	mdmStageConfiguration  mdmStage = "configuration"
	mdmStageVM             mdmStage = "vm"
	mdmStageIP             mdmStage = "ip"
	mdmStageAuthentication mdmStage = "authentication"
	mdmStageSFTP           mdmStage = "sftp"
	mdmStageVerification   mdmStage = "verification"
)

type mdmStageError struct {
	Stage mdmStage
	Err   error
}

func (e *mdmStageError) Error() string { return e.Err.Error() }
func (e *mdmStageError) Unwrap() error { return e.Err }

type mdmTransferTarget struct {
	Address  string
	Username string
	Password string
	Timeout  time.Duration
}

type mdmProfileCopier interface {
	CopyAndVerify(context.Context, mdmTransferTarget, []byte, string) error
}

type remoteProfileFS interface {
	WriteFile(path string, data []byte, perm fs.FileMode) error
	ReadFile(path string) ([]byte, error)
	Close() error
}

type remoteProfileDialer interface {
	Dial(context.Context, mdmTransferTarget) (remoteProfileFS, error)
}

type sftpProfileCopier struct {
	dialer remoteProfileDialer
}

func newSFTPProfileCopier() mdmProfileCopier {
	return &sftpProfileCopier{dialer: &sshSFTPProfileDialer{}}
}

func (c *sftpProfileCopier) CopyAndVerify(ctx context.Context, target mdmTransferTarget, profile []byte, payloadUUID string) (err error) {
	remote, err := c.dialer.Dial(ctx, target)
	if err != nil {
		var stageErr *mdmStageError
		if errors.As(err, &stageErr) {
			return err
		}
		return newMDMStageError(mdmStageAuthentication, "connect to guest", err)
	}
	defer func() {
		if closeErr := remote.Close(); closeErr != nil && err == nil {
			err = newMDMStageError(mdmStageSFTP, "close remote profile connection", closeErr)
		}
	}()

	if err := remote.WriteFile(mdmProfileRemotePath, profile, 0o600); err != nil {
		return newMDMStageError(mdmStageSFTP, "write remote profile", err)
	}
	remoteProfile, err := remote.ReadFile(mdmProfileRemotePath)
	if err != nil {
		return newMDMStageError(mdmStageVerification, "read remote profile", err)
	}
	if !bytes.Equal(remoteProfile, profile) {
		return &mdmStageError{Stage: mdmStageVerification, Err: errors.New("remote profile does not match generated profile")}
	}
	if !bytes.Contains(remoteProfile, []byte(payloadUUID)) {
		return &mdmStageError{Stage: mdmStageVerification, Err: errors.New("remote profile does not contain generated UUID")}
	}
	return nil
}

func newMDMStageError(stage mdmStage, operation string, err error) *mdmStageError {
	return &mdmStageError{Stage: stage, Err: fmt.Errorf("%s: %w", operation, err)}
}

type sshSFTPProfileDialer struct {
	dialContext   func(context.Context, string, string) (net.Conn, error)
	newClientConn func(net.Conn, string, *ssh.ClientConfig) (ssh.Conn, <-chan ssh.NewChannel, <-chan *ssh.Request, error)
	newSFTPClient func(*ssh.Client) (*sftp.Client, error)
}

func (d *sshSFTPProfileDialer) Dial(ctx context.Context, target mdmTransferTarget) (remoteProfileFS, error) {
	deadline := transferDeadline(ctx, target.Timeout)
	dialCtx := ctx
	cancelDial := func() {}
	if !deadline.IsZero() {
		dialCtx, cancelDial = context.WithDeadline(ctx, deadline)
	}
	defer cancelDial()

	dialContext := d.dialContext
	if dialContext == nil {
		var remaining time.Duration
		if !deadline.IsZero() {
			remaining = time.Until(deadline)
			if remaining <= 0 {
				remaining = time.Nanosecond
			}
		}
		networkDialer := &net.Dialer{Timeout: remaining}
		dialContext = networkDialer.DialContext
	}
	connection, err := dialContext(dialCtx, "tcp", target.Address)
	if err != nil {
		return nil, fmt.Errorf("open SSH connection: %w", err)
	}

	if !deadline.IsZero() {
		if err := connection.SetDeadline(deadline); err != nil {
			connection.Close()
			return nil, fmt.Errorf("set SSH connection deadline: %w", err)
		}
	}

	watchDone := make(chan struct{})
	var stopWatchOnce sync.Once
	stopWatch := func() { stopWatchOnce.Do(func() { close(watchDone) }) }
	go func() {
		select {
		case <-ctx.Done():
			connection.Close()
		case <-watchDone:
		}
	}()
	if err := ctx.Err(); err != nil {
		stopWatch()
		connection.Close()
		return nil, fmt.Errorf("open SSH connection: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            target.Username,
		Auth:            []ssh.AuthMethod{ssh.Password(target.Password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         target.Timeout,
	}
	newClientConn := d.newClientConn
	if newClientConn == nil {
		newClientConn = ssh.NewClientConn
	}
	sshConnection, newChannels, requests, err := newClientConn(connection, target.Address, config)
	if err != nil {
		stopWatch()
		connection.Close()
		return nil, fmt.Errorf("authenticate SSH connection: %w", err)
	}
	sshClient := ssh.NewClient(sshConnection, newChannels, requests)

	newSFTPClient := d.newSFTPClient
	if newSFTPClient == nil {
		newSFTPClient = func(client *ssh.Client) (*sftp.Client, error) {
			return sftp.NewClient(client)
		}
	}
	sftpClient, err := newSFTPClient(sshClient)
	if err != nil {
		stopWatch()
		sshClient.Close()
		connection.Close()
		return nil, newMDMStageError(mdmStageSFTP, "start SFTP client", err)
	}

	return &sftpRemoteProfileFS{
		openFile: func(path string, flags int) (io.WriteCloser, error) {
			return sftpClient.OpenFile(path, flags)
		},
		readFile: func(path string) ([]byte, error) {
			file, err := sftpClient.Open(path)
			if err != nil {
				return nil, err
			}
			data, readErr := io.ReadAll(file)
			return data, errors.Join(readErr, file.Close())
		},
		chmod: sftpClient.Chmod,
		closeResources: func() error {
			stopWatch()
			return errors.Join(sftpClient.Close(), sshClient.Close())
		},
	}, nil
}

func transferDeadline(ctx context.Context, timeout time.Duration) time.Time {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	if contextDeadline, ok := ctx.Deadline(); ok && (deadline.IsZero() || contextDeadline.Before(deadline)) {
		deadline = contextDeadline
	}
	return deadline
}

type sftpRemoteProfileFS struct {
	openFile       func(string, int) (io.WriteCloser, error)
	readFile       func(string) ([]byte, error)
	chmod          func(string, fs.FileMode) error
	closeResources func() error
}

func (f *sftpRemoteProfileFS) WriteFile(path string, data []byte, perm fs.FileMode) error {
	cleaned := filepath.Clean(path)
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return fmt.Errorf("invalid path traversal: %s", path)
	}
	file, err := f.openFile(cleaned, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
	if err != nil {
		return fmt.Errorf("open remote file: %w", err)
	}

	var writeErr error
	for len(data) > 0 {
		written, err := file.Write(data)
		if written < 0 || written > len(data) {
			writeErr = io.ErrShortWrite
			break
		}
		data = data[written:]
		if err != nil {
			writeErr = err
			break
		}
		if written == 0 {
			writeErr = io.ErrShortWrite
			break
		}
	}
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if err := f.chmod(cleaned, perm); err != nil {
		return fmt.Errorf("set remote file mode: %w", err)
	}
	return nil
}

func (f *sftpRemoteProfileFS) ReadFile(path string) ([]byte, error) {
	cleaned := filepath.Clean(path)
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return nil, fmt.Errorf("invalid path traversal: %s", path)
	}
	return f.readFile(cleaned)
}

func (f *sftpRemoteProfileFS) Close() error {
	if f.closeResources == nil {
		return nil
	}
	return f.closeResources()
}
