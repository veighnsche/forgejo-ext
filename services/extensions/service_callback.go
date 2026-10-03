// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"forgejo.org/modules/log"
)

type serviceCallbackEndpoint struct {
	server *http.Server
	path   string
	info   os.FileInfo
}

// SetServiceCallbackEndpoint configures the optional service-only callback
// listener before Start. Its parent and group access must be provisioned by the
// operator; the manager neither creates the parent nor changes ownership.
// The supplied handler owns authorization of every callback operation.
func (m *Manager) SetServiceCallbackEndpoint(path string, handler http.Handler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("extension manager already started")
	}
	m.serviceCallbackPath = path
	m.serviceCallbackHandler = handler
	return nil
}

func callbackSocketLive(path string) bool {
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return !errors.Is(err, syscall.ECONNREFUSED)
	}
	_ = conn.Close()
	return true
}

func (m *Manager) startServiceCallback() error {
	path := m.serviceCallbackPath
	if path == "" {
		return nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || m.serviceCallbackHandler == nil {
		return errors.New("invalid service callback endpoint configuration")
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() {
		return errors.New("service callback parent directory unavailable")
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode().Type() != os.ModeSocket || callbackSocketLive(path) {
			return errors.New("service callback path is occupied")
		}
		// Stale socket from an unclean shutdown: only this endpoint binds
		// the path, so reclaim it instead of failing every later start.
		if err := os.Remove(path); err != nil {
			return errors.New("service callback path is not accessible")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.New("service callback path is not accessible")
	}
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return errors.New("service callback listener unavailable")
	}
	// UnixListener normally unlinks its path on Close, including a replacement
	// created by somebody else. Retain explicit inode ownership instead.
	listener.SetUnlinkOnClose(false)
	info, err := os.Lstat(path)
	if err != nil {
		_ = listener.Close()
		return errors.New("service callback socket identity unavailable")
	}
	endpoint := &serviceCallbackEndpoint{
		server: &http.Server{Handler: m.serviceCallbackHandler, ReadHeaderTimeout: 5 * time.Second},
		path:   path, info: info,
	}
	if err := os.Chmod(path, 0o660); err != nil {
		_ = listener.Close()
		endpoint.removeSocket()
		return errors.New("service callback socket permissions unavailable")
	}
	m.serviceCallback = endpoint
	go func() {
		if err := endpoint.server.Serve(wrapPeerCredentialListener(listener)); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("Extension service callback server failed: %v", err)
		}
	}()
	return nil
}

func (endpoint *serviceCallbackEndpoint) removeSocket() {
	info, err := os.Lstat(endpoint.path)
	if err == nil && os.SameFile(endpoint.info, info) {
		_ = os.Remove(endpoint.path)
	}
}

func (m *Manager) closeServiceCallback() {
	if m.serviceCallback == nil {
		return
	}
	_ = m.serviceCallback.server.Close()
	m.serviceCallback.removeSocket()
	m.serviceCallback = nil
}
