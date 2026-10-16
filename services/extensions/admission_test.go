// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"net/http"
	"testing"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"

	plugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/require"
)

// admissionFixture is one fabricated live backend: no process runs,
// but the manager tracks it exactly like a registered backend. The
// zero plugin client reports not-exited, so admission decisions see
// a live backend without any host effect.
func admissionFixture(t *testing.T, m *Manager, manifest sdk.Manifest, installation, instanceID string) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running[manifest.ID] = &running{
		descriptor:   Descriptor{Manifest: manifest, InstanceID: instanceID},
		client:       &plugin.Client{},
		transport:    &http.Transport{},
		installation: installation,
	}
}

func admissionIntent(t *testing.T, root, id string, desired bool, pending *string) packages.Intent {
	t.Helper()
	installation, err := packages.EnsureInstallation(root, id)
	require.NoError(t, err)
	intent, err := packages.StoreIntent(root, id, installation, desired, pending, currentGeneration(t, root, id))
	require.NoError(t, err)
	return intent
}

func currentGeneration(t *testing.T, root, id string) string {
	t.Helper()
	intent, err := packages.LoadIntent(root, id)
	require.NoError(t, err)
	return intent.Generation
}

func setFence(t *testing.T, m *Manager, id string, fenced bool) {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if fenced {
		m.draining[id] = true
	} else {
		delete(m.draining, id)
	}
}

func journalEffect(t *testing.T, m *Manager, id string, effect controlledEffect) {
	t.Helper()
	m.obsMu.Lock()
	defer m.obsMu.Unlock()
	if m.lastObs == nil {
		m.lastObs = make(map[string]controlledEffect)
	}
	m.lastObs[id] = effect
}

func requireRouted(t *testing.T, m *Manager, id string) {
	t.Helper()
	_, _, ok := m.Lookup(id)
	require.True(t, ok, "package %s must route", id)
	found := false
	for _, listed := range m.List() {
		if listed.Manifest.ID == id {
			found = true
		}
	}
	require.True(t, found, "package %s must navigate", id)
}

func requireFenced(t *testing.T, m *Manager, id string) {
	t.Helper()
	_, _, ok := m.Lookup(id)
	require.False(t, ok, "package %s must not route", id)
	for _, listed := range m.List() {
		require.NotEqual(t, id, listed.Manifest.ID, "package %s must stay out of navigation", id)
	}
}

const admissionTestVersion = "1.0.0"

// An uncontrolled optional backend routes only with current desired
// true, no pending/busy/drain/maintenance fence, and a live backend
// bound to its intent installation.
func TestOrdinaryAdmissionUncontrolled(t *testing.T) {
	newSetup := func(t *testing.T) (*Manager, string, packages.Intent) {
		t.Helper()
		root := t.TempDir()
		m := NewManager(root)
		m.started = true
		intent := admissionIntent(t, root, "sample", true, nil)
		admissionFixture(t, m, sdk.Manifest{ID: "sample", Version: admissionTestVersion}, intent.InstallationID, "instance-1")
		return m, root, intent
	}
	t.Run("open when desired with no fence", func(t *testing.T) {
		m, _, intent := newSetup(t)
		require.True(t, m.ordinaryAdmissionOpen("sample"))
		require.True(t, m.VerifyOrdinaryToken("sample", "instance-1"))
		requireRouted(t, m, "sample")
		_ = intent
	})
	t.Run("closed with pending intent", func(t *testing.T) {
		m, root, _ := newSetup(t)
		activate := packages.IntentActivate
		admissionIntent(t, root, "sample", true, &activate)
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		require.False(t, m.VerifyOrdinaryToken("sample", "instance-1"))
		requireFenced(t, m, "sample")
	})
	t.Run("closed when undesired", func(t *testing.T) {
		m, root, _ := newSetup(t)
		admissionIntent(t, root, "sample", false, nil)
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		requireFenced(t, m, "sample")
	})
	t.Run("closed while draining", func(t *testing.T) {
		m, _, _ := newSetup(t)
		setFence(t, m, "sample", true)
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		require.False(t, m.VerifyOrdinaryToken("sample", "instance-1"))
		requireFenced(t, m, "sample")
		setFence(t, m, "sample", false)
		require.True(t, m.ordinaryAdmissionOpen("sample"))
		requireRouted(t, m, "sample")
	})
	t.Run("closed while busy", func(t *testing.T) {
		m, _, _ := newSetup(t)
		m.mu.Lock()
		m.busy["sample"] = sdk.LifecycleActivate
		m.mu.Unlock()
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		requireFenced(t, m, "sample")
	})
	t.Run("closed under maintenance", func(t *testing.T) {
		m, _, _ := newSetup(t)
		m.mu.Lock()
		m.maintenanceDir = t.TempDir()
		m.mu.Unlock()
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		requireFenced(t, m, "sample")
	})
	t.Run("closed with installation mismatch", func(t *testing.T) {
		m, _, _ := newSetup(t)
		m.mu.Lock()
		m.running["sample"] = &running{
			descriptor:   Descriptor{Manifest: sdk.Manifest{ID: "sample", Version: admissionTestVersion}, InstanceID: "instance-1"},
			client:       &plugin.Client{},
			transport:    &http.Transport{},
			installation: "00000000-0000-4000-8000-000000000000",
		}
		m.mu.Unlock()
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		requireFenced(t, m, "sample")
	})
	t.Run("closed with catalog version mismatch", func(t *testing.T) {
		m, _, _ := newSetup(t)
		m.mu.Lock()
		m.catalog = []sdk.CatalogPackage{{ID: "sample", Version: "2.0.0"}}
		m.mu.Unlock()
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		requireFenced(t, m, "sample")
	})
	t.Run("token requires the exact instance", func(t *testing.T) {
		m, _, _ := newSetup(t)
		require.False(t, m.VerifyOrdinaryToken("sample", "instance-2"))
		require.False(t, m.VerifyOrdinaryToken("sample", ""))
		require.False(t, m.VerifyOrdinaryToken("", "instance-1"))
	})
}

// A controlled optional backend additionally requires the current
// controller instance with confirmed running effects and admission
// readiness, plus matching catalog identity.
func TestOrdinaryAdmissionControlled(t *testing.T) {
	newSetup := func(t *testing.T) (*Manager, string, packages.Intent) {
		t.Helper()
		root := t.TempDir()
		m := NewManager(root)
		m.started = true
		m.mu.Lock()
		m.catalog = []sdk.CatalogPackage{{ID: "sample", Version: admissionTestVersion, LifecycleController: "controller"}}
		m.catalogSHA = "sha"
		m.controllers["controller"] = controllerState{instanceID: "controller-1", ready: true}
		m.mu.Unlock()
		intent := admissionIntent(t, root, "sample", true, nil)
		admissionFixture(t, m, sdk.Manifest{ID: "sample", Version: admissionTestVersion, LifecycleController: "controller"}, intent.InstallationID, "instance-1")
		admissionFixture(t, m, sdk.Manifest{ID: "controller", Version: admissionTestVersion}, intent.InstallationID, "controller-1")
		journalEffect(t, m, "sample", controlledEffect{state: sdk.LifecycleStateRunning, admission: true, generation: intent.Generation, installation: intent.InstallationID, instanceID: "controller-1"})
		return m, root, intent
	}
	t.Run("open with current running effects", func(t *testing.T) {
		m, _, _ := newSetup(t)
		require.True(t, m.ordinaryAdmissionOpen("sample"))
		require.True(t, m.VerifyOrdinaryToken("sample", "instance-1"))
		requireRouted(t, m, "sample")
	})
	t.Run("closed without a current effect", func(t *testing.T) {
		m, _, intent := newSetup(t)
		for name, effect := range map[string]controlledEffect{
			"stale generation": {state: sdk.LifecycleStateRunning, admission: true, generation: "99", installation: intent.InstallationID, instanceID: "controller-1"},
			"stopped effects":  {state: sdk.LifecycleStateStopped, admission: false, generation: intent.Generation, installation: intent.InstallationID, instanceID: "controller-1"},
			"no grant":         {state: sdk.LifecycleStateRunning, admission: false, generation: intent.Generation, installation: intent.InstallationID, instanceID: "controller-1"},
			"old instance":     {state: sdk.LifecycleStateRunning, admission: true, generation: intent.Generation, installation: intent.InstallationID, instanceID: "controller-0"},
		} {
			journalEffect(t, m, "sample", effect)
			require.False(t, m.ordinaryAdmissionOpen("sample"), name)
			require.False(t, m.VerifyOrdinaryToken("sample", "instance-1"), name)
			requireFenced(t, m, "sample")
		}
	})
	t.Run("closed when the controller is not ready", func(t *testing.T) {
		m, _, _ := newSetup(t)
		m.mu.Lock()
		m.controllers["controller"] = controllerState{instanceID: "controller-1", ready: false}
		m.mu.Unlock()
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		requireFenced(t, m, "sample")
	})
	t.Run("closed with catalog version mismatch", func(t *testing.T) {
		m, _, _ := newSetup(t)
		m.mu.Lock()
		m.catalog = []sdk.CatalogPackage{{ID: "sample", Version: "9.9.9", LifecycleController: "controller"}}
		m.mu.Unlock()
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		requireFenced(t, m, "sample")
	})
	t.Run("closed with pending intent despite good effects", func(t *testing.T) {
		m, root, _ := newSetup(t)
		activate := packages.IntentActivate
		admissionIntent(t, root, "sample", true, &activate)
		require.False(t, m.ordinaryAdmissionOpen("sample"))
		requireFenced(t, m, "sample")
	})
}

// Required packages keep their existing availability: a live
// backend with no teardown fence routes, independent of lifecycle
// intent tracking.
func TestOrdinaryAdmissionRequired(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root, "soda")
	m.started = true
	admissionFixture(t, m, sdk.Manifest{ID: "soda", Version: admissionTestVersion}, "install-1", "instance-1")
	require.True(t, m.ordinaryAdmissionOpen("soda"))
	requireRouted(t, m, "soda")
	setFence(t, m, "soda", true)
	require.False(t, m.ordinaryAdmissionOpen("soda"))
	requireFenced(t, m, "soda")
}

// Ordinary background tokens verify only while their exact
// package/instance is admitted. The fence rejects without revoking:
// releasing it restores the same token.
func TestBackgroundTokenFollowsFence(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	m.started = true
	intent := admissionIntent(t, root, "sample", true, nil)
	admissionFixture(t, m, sdk.Manifest{ID: "sample", Version: admissionTestVersion}, intent.InstallationID, "instance-1")
	token, err := m.IssueRuntimeAdmission("sample", intent.InstallationID, "instance-1", []string{"native.actor.read"})
	require.NoError(t, err)
	_, ok := m.VerifyBackgroundAdmission(token)
	require.True(t, ok)
	setFence(t, m, "sample", true)
	_, ok = m.VerifyBackgroundAdmission(token)
	require.False(t, ok, "fenced tokens must not verify")
	setFence(t, m, "sample", false)
	_, ok = m.VerifyBackgroundAdmission(token)
	require.True(t, ok, "released fence must restore retained tokens")
	_, err = m.IssueRuntimeAdmission("", intent.InstallationID, "instance-1", []string{"native.actor.read"})
	require.Error(t, err, "unbound admissions are refused")
}

// The service bootstrap issues no admission for a fenced package,
// and previously bootstrapped tokens stop verifying until release.
func TestServiceBootstrapFollowsFence(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	m.started = true
	intent := admissionIntent(t, root, "bgsample", true, nil)
	admissionFixture(t, m, sdk.Manifest{ID: "bgsample", Version: admissionTestVersion, Capabilities: []string{"native.service.bridge"}}, intent.InstallationID, "instance-1")
	self := uint32(0)
	m.mu.Lock()
	m.serviceBridgePeers = map[uint32]string{self: "bgsample"}
	m.mu.Unlock()
	token, installation, err := m.BootstrapServiceAdmission(self, "")
	require.NoError(t, err)
	require.Equal(t, intent.InstallationID, installation)
	_, ok := m.VerifyBackgroundAdmission(token)
	require.True(t, ok)
	setFence(t, m, "bgsample", true)
	_, _, err = m.BootstrapServiceAdmission(self, "")
	require.ErrorContains(t, err, "not admitted")
	_, ok = m.VerifyBackgroundAdmission(token)
	require.False(t, ok, "fenced service tokens must not verify")
	setFence(t, m, "bgsample", false)
	_, ok = m.VerifyBackgroundAdmission(token)
	require.True(t, ok, "released fence must restore retained tokens")
}

// ControlState projects the unified predicate: open only after
// durable activation, closed under the fence or pending intent.
func TestControlStateProjectionFollowsFence(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)
	m.started = true
	m.mu.Lock()
	m.catalog = []sdk.CatalogPackage{{ID: "sample", Version: admissionTestVersion, LifecycleController: "controller"}}
	m.catalogSHA = "sha"
	m.controllers["controller"] = controllerState{instanceID: "controller-1", ready: true}
	m.mu.Unlock()
	intent := admissionIntent(t, root, "sample", true, nil)
	admissionFixture(t, m, sdk.Manifest{ID: "sample", Version: admissionTestVersion, LifecycleController: "controller"}, intent.InstallationID, "instance-1")
	admissionFixture(t, m, sdk.Manifest{ID: "controller", Version: admissionTestVersion}, intent.InstallationID, "controller-1")
	journalEffect(t, m, "sample", controlledEffect{state: sdk.LifecycleStateRunning, admission: true, generation: intent.Generation, installation: intent.InstallationID, instanceID: "controller-1"})
	response, reject := m.ControlState(context.Background(), "controller", "sample")
	require.Nil(t, reject)
	require.True(t, response.AdmissionOpen, "durable activation opens the projection")
	setFence(t, m, "sample", true)
	response, reject = m.ControlState(context.Background(), "controller", "sample")
	require.Nil(t, reject)
	require.False(t, response.AdmissionOpen, "the fence closes the projection")
	setFence(t, m, "sample", false)
	activate := packages.IntentActivate
	admissionIntent(t, root, "sample", true, &activate)
	response, reject = m.ControlState(context.Background(), "controller", "sample")
	require.Nil(t, reject)
	require.False(t, response.AdmissionOpen, "pending intent closes the projection")
}
