// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"forgejo.org/modules/extensions"

	"github.com/gofrs/flock"
	"github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
)

// Descriptor contains a defensive manifest copy and the package directory.
// Root is for the core asset handler; it must never be sent to browsers.
type Descriptor struct {
	Manifest extensions.Manifest
	Root     string
}

type running struct {
	descriptor Descriptor
	client     *plugin.Client
	transport  *http.Transport
	runtimeDir string
}

type Manager struct {
	root    string
	mu      sync.Mutex
	running map[string]*running
	started bool
	env     []string
	lock    *flock.Flock
}

var defaultManager atomic.Pointer[Manager]

func SetDefault(manager *Manager) { defaultManager.Store(manager) }
func GetManager() *Manager        { return defaultManager.Load() }

func NewManager(root string) *Manager {
	return &Manager{root: root, running: make(map[string]*running)}
}

// Start loads installed, enabled packages once. A failed package stops all
// newly started packages so the registry never exposes a partial startup.
func (m *Manager) Start(ctx context.Context) (err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("extension manager already started")
	}
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return err
	}
	m.lock, err = extensions.AcquirePackageLock(m.root)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			m.stopAll()
			_ = m.lock.Unlock()
			m.lock = nil
		}
	}()
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		packageDir := filepath.Join(m.root, entry.Name())
		if _, err := os.Stat(filepath.Join(packageDir, ".disabled")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		item, err := m.startOne(packageDir, entry.Name())
		if err != nil {
			return fmt.Errorf("start extension %q: %w", entry.Name(), err)
		}
		m.running[item.descriptor.Manifest.ID] = item
	}
	m.started = true
	return nil
}

func (m *Manager) startOne(packageDir, dirName string) (_ *running, err error) {
	manifest, err := extensions.LoadManifest(packageDir)
	if err != nil {
		return nil, err
	}
	if manifest.ID != dirName {
		return nil, errors.New("package directory must match manifest id")
	}
	executable := filepath.Join(packageDir, filepath.FromSlash(manifest.Executable))
	info, err := os.Lstat(executable)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("extension executable must be a regular executable file")
	}
	dataDir := filepath.Join(m.root, ".data", manifest.ID)
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	runtimeRoot := filepath.Join(m.root, ".runtime")
	if err := os.MkdirAll(runtimeRoot, 0o700); err != nil {
		return nil, err
	}
	runtimeDir, err := os.MkdirTemp(runtimeRoot, "r")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(runtimeDir)
		}
	}()
	socket := filepath.Join(runtimeDir, "s")
	cmd := exec.Command(executable)
	cmd.Dir = packageDir
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dataDir,
		"TMPDIR=" + runtimeDir,
		extensions.SocketEnv + "=" + socket,
		extensions.DataEnv + "=" + dataDir,
	}, m.env...)
	handshake, plugins := extensions.ClientContract()
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: handshake,
		Plugins:         plugins,
		Cmd:             cmd,
		StartTimeout:    10 * time.Second,
		SkipHostEnv:     true,
		Logger:          hclog.NewNullLogger(),
	})
	defer func() {
		if err != nil {
			client.Kill()
		}
	}()
	protocol, err := client.Client()
	if err != nil {
		return nil, err
	}
	control, err := protocol.Dispense("control")
	if err != nil {
		return nil, err
	}
	if err = control.(interface{ Ready() error }).Ready(); err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("extension HTTP listener: %w", err)
	}
	_ = conn.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &running{descriptor: Descriptor{Manifest: manifest, Root: packageDir}, client: client, transport: transport, runtimeDir: runtimeDir}, nil
}

func cloneDescriptor(d Descriptor) Descriptor {
	d.Manifest.Pages = append([]extensions.Page(nil), d.Manifest.Pages...)
	d.Manifest.Panels = append([]extensions.Panel(nil), d.Manifest.Panels...)
	return d
}

func (m *Manager) List() []Descriptor {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]Descriptor, 0, len(m.running))
	for id, item := range m.running {
		if item.client.Exited() {
			m.stopOne(id, item)
			continue
		}
		list = append(list, cloneDescriptor(item.descriptor))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Manifest.ID < list[j].Manifest.ID })
	return list
}

func (m *Manager) Lookup(id string) (Descriptor, http.RoundTripper, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.running[id]
	if item == nil {
		return Descriptor{}, nil, false
	}
	if item.client.Exited() {
		m.stopOne(id, item)
		return Descriptor{}, nil, false
	}
	return cloneDescriptor(item.descriptor), item.transport, true
}

func (m *Manager) stopOne(id string, item *running) {
	delete(m.running, id)
	item.transport.CloseIdleConnections()
	item.client.Kill()
	_ = os.RemoveAll(item.runtimeDir)
}

func (m *Manager) stopAll() {
	for id, item := range m.running {
		m.stopOne(id, item)
	}
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopAll()
	m.started = false
	if m.lock != nil {
		err := m.lock.Unlock()
		m.lock = nil
		return err
	}
	return nil
}
