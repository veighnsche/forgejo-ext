// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"
	"forgejo.org/modules/log"

	"github.com/gofrs/flock"
	"github.com/hashicorp/go-hclog"
	plugin "github.com/hashicorp/go-plugin"
)

// Descriptor contains a defensive manifest copy and the package directory.
// Root is for the core asset handler; it must never be sent to browsers.
type Descriptor struct {
	Manifest   sdk.Manifest
	Root       string
	InstanceID string
}

type running struct {
	descriptor     Descriptor
	client         *plugin.Client
	transport      *http.Transport
	runtimeDir     string
	callbackServer *http.Server
	installation   string
}

type Manager struct {
	root                   string
	requiredIDs            []string
	mu                     sync.Mutex
	running                map[string]*running
	started                bool
	env                    []string
	lock                   *flock.Flock
	callbackHandlerFactory func(instanceID string) http.Handler
	instanceStopped        func(instanceID string)
	serviceCallbackPath    string
	serviceCallbackHandler http.Handler
	serviceCallback        *serviceCallbackEndpoint
	background             *backgroundRegistry
	serviceBridgePeers     map[uint32]string
}

var defaultManager atomic.Pointer[Manager]

func SetDefault(manager *Manager) {
	defaultManager.Store(manager)
	if manager == nil {
		packages.SetPolicyRuntime(nil)
	} else {
		packages.SetPolicyRuntime(manager)
	}
}
func GetManager() *Manager { return defaultManager.Load() }

func NewManager(root string, requiredIDs ...string) *Manager {
	return &Manager{root: root, requiredIDs: append([]string(nil), requiredIDs...), running: make(map[string]*running), background: newBackgroundRegistry()}
}

// SetCallbackHandlerFactory installs the host capability handler used by
// extensions that declare native callback capabilities. It must be configured
// before Start.
func (m *Manager) SetCallbackHandlerFactory(factory func(instanceID string) http.Handler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("extension manager already started")
	}
	m.callbackHandlerFactory = factory
	return nil
}

// SetInstanceStopped installs a callback to revoke admissions owned by an
// instance. The callback must not call back into Manager: it runs under m.mu.
func (m *Manager) SetInstanceStopped(callback func(instanceID string)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.started {
		return errors.New("extension manager already started")
	}
	m.instanceStopped = callback
	return nil
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
		return errors.New("extension package directory unavailable")
	}
	m.lock, err = packages.AcquirePackageLock(m.root)
	if err != nil {
		return errors.New("extension package lock unavailable")
	}
	defer func() {
		if err != nil {
			m.stopAll()
			m.closeServiceCallback()
			_ = m.lock.Unlock()
			m.lock = nil
		}
	}()
	if err := m.startServiceCallback(); err != nil {
		return err
	}
	entries, err := os.ReadDir(m.root)
	if err != nil {
		return errors.New("cannot list extension packages")
	}
	required := make(map[string]bool, len(m.requiredIDs))
	for _, id := range m.requiredIDs {
		if !sdk.ValidID(id) || required[id] {
			return errors.New("invalid or duplicate required extension id")
		}
		required[id] = false
	}
	for _, entry := range entries {
		if _, ok := required[entry.Name()]; !ok {
			continue
		}
		if !entry.IsDir() {
			return fmt.Errorf("required extension %q is not a package directory", entry.Name())
		}
		if _, err := os.Lstat(filepath.Join(m.root, entry.Name(), ".disabled")); err == nil {
			return fmt.Errorf("required extension %q is disabled", entry.Name())
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("required extension %q activation state unavailable", entry.Name())
		}
		required[entry.Name()] = true
	}
	for _, id := range m.requiredIDs {
		if !required[id] {
			return fmt.Errorf("required extension %q is missing", id)
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		if !sdk.ValidID(entry.Name()) {
			return errors.New("invalid extension package directory name")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		packageDir := filepath.Join(m.root, entry.Name())
		if _, err := os.Stat(filepath.Join(packageDir, ".disabled")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return errors.New("cannot read package activation state")
		}
		item, err := m.startOne(packageDir, entry.Name())
		if err != nil {
			log.Error("Extension %s startup failed: %v", entry.Name(), err)
			return fmt.Errorf("start extension %q: %w", entry.Name(), err)
		}
		m.running[item.descriptor.Manifest.ID] = item
		log.Info("Extension %s started", entry.Name())
		go m.observeExit(entry.Name(), item)
	}
	m.started = true
	return nil
}

func (m *Manager) startOne(packageDir, dirName string) (_ *running, err error) {
	phase := "package validation"
	defer func() {
		if err != nil {
			err = errors.New(phase + " failed")
		}
	}()
	manifest, err := sdk.LoadManifest(packageDir)
	if err != nil {
		return nil, err
	}
	if manifest.ID != dirName {
		return nil, errors.New("package directory must match manifest id")
	}
	// The installer assigns installation identity; start only re-reads the
	// recorded UUID so restart and replacement preserve it.
	installation, err := packages.EnsureInstallation(m.root, manifest.ID)
	if err != nil {
		return nil, err
	}
	if slices.Contains(manifest.Capabilities, sdk.CapabilityServiceBridge) && m.serviceCallback == nil {
		phase = "service callback configuration"
		return nil, errors.New("service callback endpoint is unavailable")
	}
	executable := filepath.Join(packageDir, filepath.FromSlash(manifest.Executable))
	phase = "executable validation"
	info, err := os.Lstat(executable)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, errors.New("extension executable must be a regular executable file")
	}
	dataDir := filepath.Join(m.root, ".data", manifest.ID)
	phase = "runtime directory setup"
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
	instanceBytes := make([]byte, 32)
	if _, err := rand.Read(instanceBytes); err != nil {
		return nil, err
	}
	instanceID := base64.RawURLEncoding.EncodeToString(instanceBytes)
	var callbackServer *http.Server
	if needsNativeCallback(manifest.Capabilities) {
		if m.callbackHandlerFactory == nil {
			return nil, errors.New("native callback handler is unavailable")
		}
		handler := m.callbackHandlerFactory(instanceID)
		if handler == nil {
			return nil, errors.New("native callback handler is unavailable")
		}
		callbackSocket := filepath.Join(runtimeDir, "c")
		rawListener, listenErr := net.Listen("unix", callbackSocket)
		if listenErr != nil {
			return nil, listenErr
		}
		listener := wrapPeerCredentialListener(rawListener)
		if chmodErr := os.Chmod(callbackSocket, 0o600); chmodErr != nil {
			_ = listener.Close()
			return nil, chmodErr
		}
		callbackServer = &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if serveErr := callbackServer.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				log.Error("Extension native callback server failed: %v", serveErr)
			}
		}()
		defer func() {
			if err != nil {
				_ = callbackServer.Close()
			}
		}()
	}
	socket := filepath.Join(runtimeDir, "s")
	cmd := exec.Command(executable)
	cmd.Dir = packageDir
	cmd.Env = append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dataDir,
		"TMPDIR=" + runtimeDir,
		sdk.SocketEnv + "=" + socket,
		sdk.DataEnv + "=" + dataDir,
	}, m.env...)
	if needsNativeCallback(manifest.Capabilities) {
		cmd.Env = append(cmd.Env, sdk.CallbackEnv+"="+filepath.Join(runtimeDir, "c"))
	}
	handshake, plugins := sdk.ClientContract()
	phase = "process handshake"
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
	phase = "control readiness"
	if err = control.(interface{ Ready() error }).Ready(); err != nil {
		return nil, err
	}
	phase = "runtime registration"
	registration, err := control.(interface {
		Registration() (sdk.Registration, error)
	}).Registration()
	if err != nil {
		return nil, err
	}
	if registration.AuthorizesContribution != slices.Contains(manifest.Capabilities, sdk.CapabilityContributionAuthorize) || len(registration.Policies) != len(manifest.Policies) {
		return nil, errors.New("extension runtime registration does not match manifest")
	}
	for _, policy := range manifest.Policies {
		if !slices.Contains(registration.Policies, policy) {
			return nil, errors.New("extension runtime registration does not match manifest")
		}
	}
	if slices.Contains(manifest.Capabilities, sdk.CapabilityBackgroundOperations) {
		phase = "background admission"
		admission, issueErr := m.IssueRuntimeAdmission(installation, instanceID, manifest.Capabilities)
		if issueErr != nil {
			return nil, issueErr
		}
		deliverer, ok := control.(interface {
			DeliverBackgroundAdmission(sdk.BackgroundAdmissionDelivery) error
		})
		if !ok {
			m.RevokeBackgroundForInstance(instanceID)
			return nil, errors.New("extension control cannot receive background admission")
		}
		if err := deliverer.DeliverBackgroundAdmission(sdk.BackgroundAdmissionDelivery{Admission: admission, InstallationID: installation}); err != nil {
			m.RevokeBackgroundForInstance(instanceID)
			return nil, err
		}
	}
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	phase = "HTTP listener"
	if err != nil {
		return nil, fmt.Errorf("extension HTTP listener: %w", err)
	}
	_ = conn.Close()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	return &running{descriptor: Descriptor{Manifest: manifest, Root: packageDir, InstanceID: instanceID}, client: client, transport: transport, runtimeDir: runtimeDir, callbackServer: callbackServer, installation: installation}, nil
}

func needsNativeCallback(capabilities []string) bool {
	for _, capability := range capabilities {
		switch capability {
		case sdk.CapabilityActorRead, sdk.CapabilityRepositoryRead,
			sdk.CapabilityOwnedRepositoriesSearch, sdk.CapabilityOrganizationOwnership,
			sdk.CapabilityPublicKeysRead, sdk.CapabilityBackgroundOperations:
			return true
		}
	}
	return false
}

func cloneDescriptor(d Descriptor) Descriptor {
	d.Manifest.Pages = append([]sdk.Page(nil), d.Manifest.Pages...)
	d.Manifest.Panels = append([]sdk.Panel(nil), d.Manifest.Panels...)
	d.Manifest.Capabilities = append([]string(nil), d.Manifest.Capabilities...)
	d.Manifest.Policies = append([]string(nil), d.Manifest.Policies...)
	return d
}

func (m *Manager) List() []Descriptor {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]Descriptor, 0, len(m.running))
	for id, item := range m.running {
		if item.client.Exited() {
			m.stopOne(id, item, true)
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
		m.stopOne(id, item, true)
		return Descriptor{}, nil, false
	}
	return cloneDescriptor(item.descriptor), item.transport, true
}

var ErrRequiredPolicyUnavailable = packages.ErrRequiredPolicyUnavailable

// EvaluateRequiredPolicy asks every required package that declares policyID.
// A missing package or handler, process exit, timeout, or malformed response
// fails closed. Required packages are evaluated in configured order.
func (m *Manager) EvaluateRequiredPolicy(ctx context.Context, policyID string, request sdk.PolicyRequest) (sdk.PolicyDecision, error) {
	m.mu.Lock()
	if !m.started || len(m.requiredIDs) == 0 {
		m.mu.Unlock()
		return sdk.PolicyDecision{}, ErrRequiredPolicyUnavailable
	}
	transports := make([]http.RoundTripper, 0, len(m.requiredIDs))
	for _, id := range m.requiredIDs {
		item := m.running[id]
		if item == nil {
			m.mu.Unlock()
			return sdk.PolicyDecision{}, ErrRequiredPolicyUnavailable
		}
		if item.client.Exited() {
			m.stopOne(id, item, true)
			m.mu.Unlock()
			return sdk.PolicyDecision{}, ErrRequiredPolicyUnavailable
		}
		if slices.Contains(item.descriptor.Manifest.Policies, policyID) {
			transports = append(transports, item.transport)
		}
	}
	m.mu.Unlock()
	if len(transports) == 0 {
		return sdk.PolicyDecision{}, ErrRequiredPolicyUnavailable
	}
	data, err := json.Marshal(request)
	if err != nil {
		return sdk.PolicyDecision{}, ErrRequiredPolicyUnavailable
	}
	for _, transport := range transports {
		callCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		httpRequest, err := http.NewRequestWithContext(callCtx, http.MethodPost, "http://extension/v1/policies/"+url.PathEscape(policyID), bytes.NewReader(data))
		if err != nil {
			cancel()
			return sdk.PolicyDecision{}, ErrRequiredPolicyUnavailable
		}
		httpRequest.Header.Set("Content-Type", "application/json")
		response, err := transport.RoundTrip(httpRequest)
		if err != nil {
			cancel()
			return sdk.PolicyDecision{}, ErrRequiredPolicyUnavailable
		}
		var decision sdk.PolicyDecision
		if response.StatusCode != http.StatusOK {
			err = ErrRequiredPolicyUnavailable
		} else {
			var body []byte
			body, err = io.ReadAll(io.LimitReader(response.Body, 64<<10+1))
			if err == nil && len(body) <= 64<<10 {
				decoder := json.NewDecoder(bytes.NewReader(body))
				decoder.DisallowUnknownFields()
				err = decoder.Decode(&decision)
				if err == nil && decoder.Decode(new(any)) != io.EOF {
					err = ErrRequiredPolicyUnavailable
				}
			} else {
				err = ErrRequiredPolicyUnavailable
			}
		}
		_ = response.Body.Close()
		cancel()
		if err != nil {
			return sdk.PolicyDecision{}, ErrRequiredPolicyUnavailable
		}
		if !decision.Allowed {
			return decision, nil
		}
	}
	return sdk.PolicyDecision{Allowed: true}, nil
}

func (m *Manager) stopOne(id string, item *running, crashed bool) {
	delete(m.running, id)
	// Runtime stop revokes its runtime and service admissions; browser
	// admissions are revoked through the injected callback as before.
	m.RevokeBackgroundForInstance(item.descriptor.InstanceID)
	if m.instanceStopped != nil {
		m.instanceStopped(item.descriptor.InstanceID)
	}
	item.transport.CloseIdleConnections()
	if item.callbackServer != nil {
		_ = item.callbackServer.Close()
	}
	item.client.Kill()
	_ = os.RemoveAll(item.runtimeDir)
	if crashed {
		log.Warn("Extension %s exited unexpectedly; runtime unavailable", id)
	} else {
		log.Info("Extension %s stopped", id)
	}
}

func (m *Manager) observeExit(id string, item *running) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		if m.running[id] != item {
			m.mu.Unlock()
			return
		}
		if item.client.Exited() {
			m.stopOne(id, item, true)
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
	}
}

func (m *Manager) stopAll() {
	for id, item := range m.running {
		m.stopOne(id, item, false)
	}
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopAll()
	m.closeServiceCallback()
	m.started = false
	if m.lock != nil {
		err := m.lock.Unlock()
		m.lock = nil
		return err
	}
	return nil
}
