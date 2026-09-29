// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/rpc"
	"os"

	plugin "github.com/hashicorp/go-plugin"
)

const (
	ContextHeader = "X-Forgejo-Extension-Context"
	SocketEnv     = "FORGEJO_EXTENSION_SOCKET"
	DataEnv       = "FORGEJO_EXTENSION_DATA"
)

type Actor struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	SiteAdmin bool   `json:"site_admin"`
}

type Repository struct {
	ID         int64  `json:"id"`
	Owner      string `json:"owner"`
	Name       string `json:"name"`
	Permission string `json:"permission"`
}

type RequestAuthority struct {
	ExtensionID string      `json:"extension_id"`
	PageID      string      `json:"page_id,omitempty"`
	Scope       string      `json:"scope"`
	Actor       Actor       `json:"actor"`
	Repository  *Repository `json:"repository,omitempty"`
}

// RequestContext decodes authority asserted by Forgejo at the trusted proxy boundary.
// Extensions must only serve requests received on their private Unix socket.
func RequestContext(r *http.Request) (RequestAuthority, error) {
	var authority RequestAuthority
	value := r.Header.Get(ContextHeader)
	if value == "" {
		return authority, errors.New("missing extension authority")
	}
	if err := json.Unmarshal([]byte(value), &authority); err != nil {
		return authority, fmt.Errorf("decode extension authority: %w", err)
	}
	if authority.ExtensionID == "" || authority.Scope == "" {
		return authority, errors.New("incomplete extension authority")
	}
	return authority, nil
}

// DataDir is the extension's persistent, private data directory.
func DataDir() string { return os.Getenv(DataEnv) }

var handshake = plugin.HandshakeConfig{
	ProtocolVersion:  Protocol,
	MagicCookieKey:   "FORGEJO_EXTENSION_MAGIC_COOKIE",
	MagicCookieValue: "forgejo-extension-v1",
}

type controlPlugin struct{}

func (controlPlugin) Server(*plugin.MuxBroker) (any, error) { return new(controlRPC), nil }
func (controlPlugin) Client(_ *plugin.MuxBroker, client *rpc.Client) (any, error) {
	return &controlClient{client: client}, nil
}

type controlRPC struct{}

func (*controlRPC) Ready(_ struct{}, ready *bool) error {
	*ready = true
	return nil
}

type controlClient struct{ client *rpc.Client }

func (c *controlClient) Ready() error {
	var ready bool
	if err := c.client.Call("Plugin.Ready", struct{}{}, &ready); err != nil {
		return err
	}
	if !ready {
		return errors.New("extension is not ready")
	}
	return nil
}

// Serve starts an extension's HTTP server and then joins Forgejo's process
// handshake. Call this from the extension executable's main function.
func Serve(handler http.Handler) error {
	socket := os.Getenv(SocketEnv)
	if socket == "" {
		return errors.New("extension socket is not configured")
	}
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return fmt.Errorf("listen on extension socket: %w", err)
	}
	if err := os.Chmod(socket, 0o600); err != nil {
		listener.Close()
		return err
	}
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	plugin.Serve(&plugin.ServeConfig{
		HandshakeConfig: handshake,
		Plugins:         plugin.PluginSet{"control": controlPlugin{}},
	})
	return server.Close()
}

// ClientContract exposes the protocol details to the host without duplicating
// the SDK's handshake configuration.
func ClientContract() (plugin.HandshakeConfig, plugin.PluginSet) {
	return handshake, plugin.PluginSet{"control": controlPlugin{}}
}
