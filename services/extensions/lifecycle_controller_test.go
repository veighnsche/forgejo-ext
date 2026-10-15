// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	sdk "forgejo.org/extension-sdk"
	packages "forgejo.org/modules/extensions"

	"github.com/stretchr/testify/require"
)

// holdTestLock takes LOCK_EX on a directory inode, prints READY, and
// holds until stdin closes: the maintenance-exclusion second process.
func holdTestLock(dir string) int {
	file, err := os.OpenFile(dir, os.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Println("READY")
	_, _ = io.ReadAll(os.Stdin)
	return 0
}

type fixtureOwner struct {
	InstallationID   string `json:"installation_id"`
	EffectGeneration string `json:"effect_generation"`
}

type fixtureGrant struct {
	ControllerInstanceID string `json:"controller_instance_id"`
	PackageVersion       string `json:"package_version"`
	PackageSHA256        string `json:"package_sha256"`
	InstallationID       string `json:"installation_id"`
	Generation           string `json:"generation"`
}

func fixtureStateDir() string {
	dir := filepath.Join(os.Getenv("HOME"), "state")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

func readFixtureOwner(stateDir string) (fixtureOwner, bool) {
	data, err := os.ReadFile(filepath.Join(stateDir, "owner.json"))
	if err != nil {
		return fixtureOwner{}, false
	}
	var owner fixtureOwner
	if err := json.Unmarshal(data, &owner); err != nil || owner.InstallationID == "" {
		return fixtureOwner{}, false
	}
	if _, err := os.Lstat(filepath.Join(stateDir, "effect.up")); err != nil {
		return fixtureOwner{}, false
	}
	return owner, true
}

func runControllerHelper(knobs testFixtureKnobs) int {
	stateDir := fixtureStateDir()
	application := sdk.Application{
		HTTP: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "controller")
		}),
		Lifecycle: func(ctx context.Context, authority sdk.LifecycleAuthority, call sdk.LifecycleCall) (sdk.LifecycleObservation, error) {
			verification, err := authority.Verify(ctx, call)
			if err != nil {
				return sdk.LifecycleObservation{}, err
			}
			return controllerFixtureCall(stateDir, knobs, call, verification)
		},
	}
	if err := sdk.Serve(application); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func controllerFixtureCall(stateDir string, knobs testFixtureKnobs, call sdk.LifecycleCall, verification sdk.LifecycleVerification) (sdk.LifecycleObservation, error) {
	fail := func(err error) (sdk.LifecycleObservation, error) { return sdk.LifecycleObservation{}, err }
	switch call.Operation {
	case sdk.LifecycleObserve:
		return fixtureObserveCall(stateDir, call), nil
	case sdk.LifecycleActivate:
		if !verification.DesiredEnabled {
			return fail(errors.New("activation is not desired"))
		}
		if knobs.SlowActivateMs > 0 {
			time.Sleep(time.Duration(knobs.SlowActivateMs) * time.Millisecond)
		}
		bumpActivateCount(stateDir)
		if knobs.FailActivate {
			return fail(errors.New("activation effect failed"))
		}
		return fixtureAdoptOrCreate(stateDir, call)
	case sdk.LifecycleDeactivate:
		return fixtureDeactivateCall(stateDir, call)
	default:
		return fail(errors.New("unknown lifecycle operation"))
	}
}

func bumpObserveCount(stateDir string) {
	path := filepath.Join(stateDir, "observe.count")
	count := 0
	if data, err := os.ReadFile(path); err == nil {
		_, _ = fmt.Sscanf(string(data), "%d", &count)
	}
	_ = os.WriteFile(path, []byte(fmt.Sprintf("%d", count+1)), 0o600)
}

func readObserveCount(stateDir string) int {
	data, err := os.ReadFile(filepath.Join(stateDir, "observe.count"))
	if err != nil {
		return 0
	}
	count := 0
	_, _ = fmt.Sscanf(string(data), "%d", &count)
	return count
}

func fixtureObserveCall(stateDir string, call sdk.LifecycleCall) sdk.LifecycleObservation {
	bumpObserveCount(stateDir)
	owner, owned := readFixtureOwner(stateDir)
	if !owned {
		return sdk.LifecycleObservation{LifecycleCall: call, State: sdk.LifecycleStateStopped, Detail: "No owned runtime exists."}
	}
	grantData, err := os.ReadFile(filepath.Join(stateDir, "grant.json"))
	ready := false
	if err == nil {
		var grant fixtureGrant
		if json.Unmarshal(grantData, &grant) == nil &&
			grant.ControllerInstanceID == call.ControllerInstanceID &&
			grant.PackageVersion == call.PackageVersion && grant.PackageSHA256 == call.PackageSHA256 &&
			grant.InstallationID == call.InstallationID && grant.Generation == call.Generation {
			ready = true
		}
	}
	generation := owner.EffectGeneration
	detail := "Owned runtime is running without a current grant."
	if ready {
		detail = "Owned runtime is running with a current operational grant."
	}
	return sdk.LifecycleObservation{LifecycleCall: call, State: sdk.LifecycleStateRunning, EffectGeneration: &generation, AdmissionReady: ready, Detail: detail}
}

func fixtureAdoptOrCreate(stateDir string, call sdk.LifecycleCall) (sdk.LifecycleObservation, error) {
	fail := func(err error) (sdk.LifecycleObservation, error) { return sdk.LifecycleObservation{}, err }
	writeGrant := func() error {
		grant := fixtureGrant{ControllerInstanceID: call.ControllerInstanceID, PackageVersion: call.PackageVersion, PackageSHA256: call.PackageSHA256, InstallationID: call.InstallationID, Generation: call.Generation}
		data, err := json.Marshal(grant)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(stateDir, "grant.json"), data, 0o600)
	}
	if owner, owned := readFixtureOwner(stateDir); owned {
		if owner.InstallationID != call.InstallationID {
			return fail(errors.New("effect owner mismatches"))
		}
		if err := writeGrant(); err != nil {
			return fail(err)
		}
		generation := owner.EffectGeneration
		return sdk.LifecycleObservation{LifecycleCall: call, State: sdk.LifecycleStateRunning, EffectGeneration: &generation, AdmissionReady: true, Detail: "Existing owned runtime adopted without restart or reenrollment."}, nil
	}
	owner := fixtureOwner{InstallationID: call.InstallationID, EffectGeneration: call.Generation}
	data, err := json.Marshal(owner)
	if err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "owner.json"), data, 0o600); err != nil {
		return fail(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "effect.up"), []byte(call.Generation), 0o600); err != nil {
		return fail(err)
	}
	if err := writeGrant(); err != nil {
		return fail(err)
	}
	generation := call.Generation
	return sdk.LifecycleObservation{LifecycleCall: call, State: sdk.LifecycleStateRunning, EffectGeneration: &generation, AdmissionReady: true, Detail: "Owned runtime started with a current operational grant."}, nil
}

func fixtureDeactivateCall(stateDir string, call sdk.LifecycleCall) (sdk.LifecycleObservation, error) {
	_ = os.Remove(filepath.Join(stateDir, "grant.json"))
	owner, owned := readFixtureOwner(stateDir)
	if !owned {
		return sdk.LifecycleObservation{LifecycleCall: call, State: sdk.LifecycleStateStopped, Detail: "No owned runtime exists; operational admission is closed."}, nil
	}
	if owner.InstallationID != call.InstallationID {
		return sdk.LifecycleObservation{}, errors.New("foreign runtime is untouched")
	}
	_ = os.Remove(filepath.Join(stateDir, "effect.up"))
	_ = os.Remove(filepath.Join(stateDir, "owner.json"))
	return sdk.LifecycleObservation{LifecycleCall: call, State: sdk.LifecycleStateStopped, Detail: "Owned runtime stopped; operational admission is closed."}, nil
}

func bumpActivateCount(stateDir string) {
	path := filepath.Join(stateDir, "activate.count")
	count := 0
	if data, err := os.ReadFile(path); err == nil {
		_, _ = fmt.Sscanf(string(data), "%d", &count)
	}
	_ = os.WriteFile(path, []byte(fmt.Sprintf("%d", count+1)), 0o600)
}

func readActivateCount(stateDir string) int {
	data, err := os.ReadFile(filepath.Join(stateDir, "activate.count"))
	if err != nil {
		return 0
	}
	count := 0
	_, _ = fmt.Sscanf(string(data), "%d", &count)
	return count
}

// testAuthorizeMux serves the authorize callback for one controller
// instance in services tests. The production multiplexer lives in the
// routers package (importing it here would cycle); the mapping there is
// covered by its own test.
func testAuthorizeMux(m *Manager, instanceID string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != sdk.LifecycleAuthorizePath {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost || r.URL.RawQuery != "" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		token := r.Header.Get(sdk.LifecycleAdmissionHeader)
		if token == "" {
			http.Error(w, "admission required", http.StatusForbidden)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, sdk.MaximumLifecycleBodyBytes+1))
		if err != nil || len(body) > sdk.MaximumLifecycleBodyBytes {
			http.Error(w, "body exceeds limit", http.StatusBadRequest)
			return
		}
		call, err := sdk.DecodeLifecycleCall(body)
		if err != nil {
			http.Error(w, "malformed call", http.StatusBadRequest)
			return
		}
		if call.ControllerInstanceID != instanceID {
			http.Error(w, "admission rejected", http.StatusForbidden)
			return
		}
		verification, authErr := m.VerifyLifecycleCall(token, call)
		if authErr != nil {
			http.Error(w, authErr.Message, authErr.HTTP)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(verification)
	})
}

func installLifecyclePackage(t *testing.T, root, id, manifest string, knobs testFixtureKnobs) {
	t.Helper()
	packageDir := filepath.Join(root, id)
	require.NoError(t, os.MkdirAll(packageDir, 0o700))
	executable, err := os.Executable()
	require.NoError(t, err)
	data, err := os.ReadFile(executable)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension"), data, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(packageDir, "extension.json"), []byte(manifest), 0o600))
	dataDir := filepath.Join(root, ".data", id)
	require.NoError(t, os.MkdirAll(dataDir, 0o700))
	knobData, err := json.Marshal(knobs)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "fixture.json"), knobData, 0o600))
}

const controllerManifest = `{"protocol":1,"id":"controller","name":"Test","version":"1.0.0","executable":"extension","capabilities":["native.extension.lifecycle"]}`
const sampleManifest = `{"protocol":1,"id":"sample","name":"Test","version":"1.0.0","executable":"extension","capabilities":["native.actor.read"],"lifecycle_controller":"controller"}`

type controllerHarness struct {
	root            string
	m               *Manager
	sha             string
	controllerEntry sdk.CatalogPackage
	sampleEntry     sdk.CatalogPackage
}

func startControllerHarness(t *testing.T, maintenanceDir string, sampleKnobs, controllerKnobs testFixtureKnobs, preinstallSample bool) *controllerHarness {
	t.Helper()
	root := shortTestRoot(t)
	installLifecyclePackage(t, root, "controller", controllerManifest, controllerKnobs)
	installLifecyclePackage(t, root, "sample", sampleManifest, sampleKnobs)
	controllerEntry := testCatalogEntry("controller", "1.0.0", true, "")
	controllerEntry.Capabilities = []string{"native.extension.lifecycle"}
	sampleEntry := testCatalogEntry("sample", "1.0.0", false, "controller")
	sampleEntry.Capabilities = []string{"native.actor.read"}
	// Required bootstrap reuses only exact installed receipts: the
	// fixture completes the required install the way production
	// installs do. The optional receipt ships only when the scenario
	// needs preinstalled bytes.
	require.NoError(t, packages.WriteReceipt(filepath.Join(root, "controller"), controllerEntry))
	if preinstallSample {
		require.NoError(t, packages.WriteReceipt(filepath.Join(root, "sample"), sampleEntry))
	}
	m := NewManager(root, "controller")
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	configureTestCatalog(t, m, root, controllerEntry, sampleEntry)
	if maintenanceDir != "" {
		require.NoError(t, m.SetMaintenanceDir(maintenanceDir))
	}
	require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(m, instanceID)
	}))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })
	return &controllerHarness{root: root, m: m, sha: m.catalogSHA, controllerEntry: controllerEntry, sampleEntry: sampleEntry}
}

func controllerStateDir(h *controllerHarness) string {
	return filepath.Join(h.root, ".data", "controller", "state")
}

func enabledSample(t *testing.T, h *controllerHarness) packages.Intent {
	t.Helper()
	requestEnabledAccept(t, h.m, "sample", "0", h.sha, true)
	waitActiveOpDone(t, h.m)
	observed := lifecycleResult(t, h.m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, observed.State)
	require.False(t, observed.Unconfirmed)
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
	require.True(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
	return intent
}

// The required controller registers only when its manifest capability
// and handler registration agree; a declared capability without a
// handler fails the handshake instead of becoming a silent no-op.
func TestControllerRegistrationMismatch(t *testing.T) {
	root := shortTestRoot(t)
	installLifecyclePackage(t, root, "controller", controllerManifest, testFixtureKnobs{})
	controllerEntry := testCatalogEntry("controller", "1.0.0", true, "")
	controllerEntry.Capabilities = []string{"native.extension.lifecycle"}
	// The receipt matches so bootstrap reaches the handshake it must
	// reject; the failure stays a registration refusal, not a fetch.
	require.NoError(t, packages.WriteReceipt(filepath.Join(root, "controller"), controllerEntry))
	m := NewManager(root, "controller")
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	configureTestCatalog(t, m, root, controllerEntry)
	require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(m, instanceID)
	}))
	err := m.Start(context.Background())
	// The handshake rejects the mismatched registration instead of
	// running a silent no-op controller; the phase wrapper carries the
	// refusal.
	require.ErrorContains(t, err, "runtime registration failed")
}

// Full enable/disable over real private sockets with live fixture
// processes: the manager persists intent before effects, dispatches
// through the required controller with distinct admissions validated on
// the authorize callback, and clears pending durably only after both
// backend and effects confirm. Generations advance once per accepted
// mutation and never on completion.
func TestControlledEnableDisableLiveWire(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller"}, true)
	enabled := enabledSample(t, h)
	_ = enabled

	_, _, ok := h.m.Lookup("sample")
	require.True(t, ok, "enabled package must open ordinary admission")
	observed := lifecycleResult(t, h.m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, observed.State)
	require.False(t, observed.Unconfirmed)
	require.Equal(t, 1, readActivateCount(controllerStateDir(h)))

	requestEnabledAccept(t, h.m, "sample", "1", h.sha, false)
	waitActiveOpDone(t, h.m)
	disabled := lifecycleResult(t, h.m.Observe("sample"))
	require.Equal(t, packages.LifecycleDisabled, disabled.State)
	require.False(t, disabled.Unconfirmed)
	_, _, ok = h.m.Lookup("sample")
	require.False(t, ok, "disable must close ordinary admission")
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "2", intent.Generation)
	require.False(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
	_, err = os.Lstat(filepath.Join(controllerStateDir(h), "effect.up"))
	require.True(t, os.IsNotExist(err), "disable must stop owned effects")
	require.NoError(t, packages.ValidateInstalled(h.root, h.sampleEntry), "disable retains package bytes")

	stale := h.m.RequestEnabled(context.Background(), "sample", "1", h.sha, false)
	require.Equal(t, packages.LifecycleFailed, stale.State)
	require.Equal(t, packages.LifecycleErrStaleState, stale.Error.Code)
}

// Two concurrent activations serialize on the single mutation slot:
// exactly one reaches the controller, the other fails without an
// effect, and the survivor completes.
func TestConcurrentEnableSerializes(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller", SlowActivateMs: 2000}, true)
	barrier := make(chan struct{})
	results := make(chan packages.LifecycleResult, 2)
	for range 2 {
		go func() {
			<-barrier
			results <- h.m.RequestEnabled(context.Background(), "sample", "0", h.sha, true)
		}()
	}
	close(barrier)
	first := <-results
	second := <-results
	var winner, loser packages.LifecycleResult
	accepted := func(result packages.LifecycleResult) bool {
		return result.Error == nil && result.State == packages.LifecycleActivating
	}
	if accepted(first) {
		winner, loser = first, second
	} else {
		winner, loser = second, first
	}
	lifecycleResult(t, winner)
	require.True(t, accepted(winner), "exactly one enable is accepted, got %+v", winner)
	require.Equal(t, packages.LifecycleFailed, loser.State)
	require.Contains(t, []string{packages.LifecycleErrInProgress, packages.LifecycleErrStaleState}, loser.Error.Code)
	waitActiveOpDone(t, h.m)
	observed := lifecycleResult(t, h.m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, observed.State)
	require.False(t, observed.Unconfirmed)
	require.Equal(t, 1, readActivateCount(controllerStateDir(h)))
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
	require.True(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
}

// A backend that fails after external activation keeps admission
// closed and runs one bounded deactivate under the failed-enable
// exception. Desired and generation are unchanged and pending activate
// is retained, so an explicit retry authorizes another activation. The
// exception grants only that deactivate inside its command: it can
// never authorize activate and dies with the command.
func TestFailedEnableCleanupConfinement(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{Role: "plain-exit"}, testFixtureKnobs{Role: "controller"}, true)
	requestEnabledAccept(t, h.m, "sample", "0", h.sha, true)
	waitActiveOpDone(t, h.m)
	observed := lifecycleResult(t, h.m.Observe("sample"))
	require.Equal(t, packages.LifecycleFailed, observed.State)
	require.True(t, observed.Unconfirmed)
	require.NotNil(t, observed.Error)
	require.Equal(t, packages.LifecycleErrActivateFailed, observed.Error.Code)
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "1", intent.Generation)
	require.True(t, intent.DesiredEnabled)
	require.NotNil(t, intent.PendingOperation)
	require.Equal(t, packages.IntentActivate, *intent.PendingOperation)
	_, _, ok := h.m.Lookup("sample")
	require.False(t, ok, "failed enable must keep admission closed")
	_, err = os.Lstat(filepath.Join(controllerStateDir(h), "effect.up"))
	require.True(t, os.IsNotExist(err), "failed-enable cleanup must stop effects")

	installation := intent.InstallationID
	m := h.m
	m.mu.Lock()
	instance := m.controllers["controller"].instanceID
	m.mu.Unlock()
	activateCall := buildCall("controller", instance, h.sampleEntry, installation, "1", sdk.LifecycleActivate, 60)
	exceptionActivate, err := m.IssueLifecycleAdmission(&BackgroundAdmission{Lifecycle: true, InstallationID: installation, InstanceID: instance, Call: activateCall, DesiredEnabled: true, CleanupException: true})
	require.NoError(t, err)
	defer m.RevokeLifecycleAdmission(exceptionActivate)
	_, authErr := m.VerifyLifecycleCall(exceptionActivate, activateCall)
	require.Error(t, authErr, "cleanup admission must never authorize activate")
	require.Equal(t, 403, authErr.HTTP)

	deactivateCall := buildCall("controller", instance, h.sampleEntry, installation, "1", sdk.LifecycleDeactivate, 60)
	exceptionDeactivate, err := m.IssueLifecycleAdmission(&BackgroundAdmission{Lifecycle: true, InstallationID: installation, InstanceID: instance, Call: deactivateCall, DesiredEnabled: true, CleanupException: true})
	require.NoError(t, err)
	defer m.RevokeLifecycleAdmission(exceptionDeactivate)
	_, authErr = m.VerifyLifecycleCall(exceptionDeactivate, deactivateCall)
	require.Error(t, authErr, "cleanup admission must not work outside its command")
	m.mu.Lock()
	m.mutation = "sample"
	m.mutationPhase = "cleanup"
	m.mu.Unlock()
	verification, authErr := m.VerifyLifecycleCall(exceptionDeactivate, deactivateCall)
	require.Nil(t, authErr)
	require.True(t, verification.DesiredEnabled)
	m.mu.Lock()
	m.mutation = ""
	m.mutationPhase = ""
	m.mu.Unlock()
	m.RevokeLifecycleAdmission(exceptionDeactivate)
	_, authErr = m.VerifyLifecycleCall(exceptionDeactivate, deactivateCall)
	require.Error(t, authErr, "cleanup admission must die with its command")

	ordinary, err := m.IssueRuntimeAdmission(installation, instance, []string{"native.actor.read"})
	require.NoError(t, err)
	_, authErr = m.VerifyLifecycleCall(ordinary, deactivateCall)
	require.Error(t, authErr, "ordinary admissions never verify as lifecycle")
	_, ok = m.VerifyBackgroundAdmission(exceptionActivate)
	require.False(t, ok, "lifecycle admissions never verify as background")
}

// Disable still cleans up after the optional backend is lost, and even
// when its package bytes and receipt are gone: observe and deactivate
// need the persisted installation UUID and the controller relation,
// never the optional bytes.
func TestDisableAfterControlledBackendLoss(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller"}, true)
	enabledSample(t, h)
	h.m.mu.Lock()
	item := h.m.running["sample"]
	require.NotNil(t, item)
	item.client.Kill()
	h.m.mu.Unlock()
	_, _, ok := h.m.Lookup("sample")
	require.False(t, ok, "crashed backend must lose admission")
	require.NoError(t, os.RemoveAll(filepath.Join(h.root, "sample")))
	requestEnabledAccept(t, h.m, "sample", "1", h.sha, false)
	waitActiveOpDone(t, h.m)
	disabled := lifecycleResult(t, h.m.Observe("sample"))
	require.Equal(t, packages.LifecycleDisabled, disabled.State)
	require.False(t, disabled.Unconfirmed)
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "2", intent.Generation)
	require.False(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
	_, err = os.Lstat(filepath.Join(controllerStateDir(h), "effect.up"))
	require.True(t, os.IsNotExist(err))
}

// Restart re-observation: a running effect without a current grant is
// failed, never enabled. An explicit retry with the same desired flag
// increments exactly once and adopts the verified owned effect without
// restart, refreshing the grant.
func TestRestartReobservationAdopts(t *testing.T) {
	h := startControllerHarness(t, "", testFixtureKnobs{}, testFixtureKnobs{Role: "controller"}, true)
	enabledSample(t, h)
	before, err := os.ReadFile(filepath.Join(controllerStateDir(h), "effect.up"))
	require.NoError(t, err)
	require.Equal(t, "1", string(before))
	require.NoError(t, os.Remove(filepath.Join(controllerStateDir(h), "grant.json")))
	require.NoError(t, h.m.Close())

	m := NewManager(h.root, "controller")
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	require.NoError(t, m.SetCatalogPath(filepath.Join(h.root, ".test-catalog.json")))
	require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(m, instanceID)
	}))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })
	h.m = m
	observed := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleFailed, observed.State)
	require.True(t, observed.Unconfirmed)
	_, _, ok := m.Lookup("sample")
	require.True(t, ok, "ordinary backend restarted with the manager")

	requestEnabledAccept(t, m, "sample", "1", h.sha, true)
	waitActiveOpDone(t, m)
	retried := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, retried.State)
	require.False(t, retried.Unconfirmed)
	intent, err := packages.LoadIntent(h.root, "sample")
	require.NoError(t, err)
	require.Equal(t, "2", intent.Generation)
	require.True(t, intent.DesiredEnabled)
	require.Nil(t, intent.PendingOperation)
	after, err := os.ReadFile(filepath.Join(controllerStateDir(h), "effect.up"))
	require.NoError(t, err)
	require.Equal(t, string(before), string(after), "adopt must not restart owned effects")
	ownerData, err := os.ReadFile(filepath.Join(controllerStateDir(h), "owner.json"))
	require.NoError(t, err)
	var owner fixtureOwner
	require.NoError(t, json.Unmarshal(ownerData, &owner))
	require.Equal(t, "1", owner.EffectGeneration, "prior effect generation stays valid evidence")
	observed = lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, observed.State)
	require.False(t, observed.Unconfirmed)
}

// The maintenance directory inode excludes mutations while a second
// process holds it exclusively or the marker persists, without
// deadlocking reads that hold no lock. The fixture directory is not
// root-owned, so production fail-closed refusal carries these cases
// here; the root-owned success path is covered where the contract can
// be built. Recovery after marker removal runs on a fresh manager
// without the fixture guard.
func TestMaintenanceGuardBlocksMutation(t *testing.T) {
	maintenanceDir := t.TempDir()
	require.NoError(t, os.Chmod(maintenanceDir, 0o755))
	h := startControllerHarness(t, maintenanceDir, testFixtureKnobs{}, testFixtureKnobs{Role: "controller"}, true)

	executable, err := os.Executable()
	require.NoError(t, err)
	holder := exec.Command(executable)
	holder.Env = append(os.Environ(), "FORGEJO_EXTENSION_TEST_HELPER=1", "FORGEJO_EXTENSION_TEST_HOLDDIR="+maintenanceDir)
	stdin, err := holder.StdinPipe()
	require.NoError(t, err)
	stdout, err := holder.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, holder.Start())
	ready := make([]byte, 16)
	_, err = io.ReadFull(stdout, ready[:6])
	require.NoError(t, err, "holder must print READY")

	blocked := h.m.RequestEnabled(context.Background(), "sample", "0", h.sha, true)
	require.Equal(t, packages.LifecycleFailed, blocked.State)
	require.Equal(t, packages.LifecycleErrMaintenance, blocked.Error.Code)
	snapshot := h.m.Snapshot()
	require.NotNil(t, snapshot, "reads must not deadlock under exclusive exclusion")
	require.NoError(t, stdin.Close())
	require.NoError(t, holder.Wait())

	require.NoError(t, os.WriteFile(filepath.Join(maintenanceDir, "operation.json"), []byte(`{}`), 0o600))
	marked := h.m.RequestEnabled(context.Background(), "sample", "0", h.sha, true)
	require.Equal(t, packages.LifecycleFailed, marked.State)
	require.Equal(t, packages.LifecycleErrMaintenance, marked.Error.Code)
	require.NoError(t, os.Remove(filepath.Join(maintenanceDir, "operation.json")))

	require.NoError(t, h.m.Close())
	recoveredManager := NewManager(h.root, "controller")
	recoveredManager.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	configureTestCatalog(t, recoveredManager, h.root, h.controllerEntry, h.sampleEntry)
	require.NoError(t, recoveredManager.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(recoveredManager, instanceID)
	}))
	require.NoError(t, recoveredManager.Start(context.Background()))
	t.Cleanup(func() { _ = recoveredManager.Close() })
	requestEnabledAccept(t, recoveredManager, "sample", "0", recoveredManager.catalogSHA, true)
	waitActiveOpDone(t, recoveredManager)
	recovered := lifecycleResult(t, recoveredManager.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, recovered.State)
	require.False(t, recovered.Unconfirmed)
}

// Enable downloads and validates the catalog-selected archive when no
// installed bytes satisfy the pin. Trust anchors come from the
// environment; no test client bypasses the bounded production fetcher.
func TestEnableDownloadsCatalogArtifact(t *testing.T) {
	root := shortTestRoot(t)
	installLifecyclePackage(t, root, "controller", controllerManifest, testFixtureKnobs{Role: "controller"})
	installLifecyclePackage(t, root, "sample", sampleManifest, testFixtureKnobs{})
	body := buildSampleArchive(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	sum := sha256.Sum256(body)
	sampleEntry := testCatalogEntry("sample", "1.0.0", false, "controller")
	sampleEntry.Capabilities = []string{"native.actor.read"}
	sampleEntry.URL = server.URL + "/sample.tar.gz"
	sampleEntry.Size = int64(len(body))
	sampleEntry.SHA256 = hex.EncodeToString(sum[:])
	controllerEntry := testCatalogEntry("controller", "1.0.0", true, "")
	controllerEntry.Capabilities = []string{"native.extension.lifecycle"}
	// The required controller ships an exact installed receipt, as
	// production installs do; only the optional bytes download.
	require.NoError(t, packages.WriteReceipt(filepath.Join(root, "controller"), controllerEntry))
	certPath := filepath.Join(root, "test-ca.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0o600))
	t.Setenv("SSL_CERT_FILE", certPath)

	m := NewManager(root, "controller")
	m.env = []string{"FORGEJO_EXTENSION_TEST_HELPER=1"}
	configureTestCatalog(t, m, root, controllerEntry, sampleEntry)
	require.NoError(t, m.SetCallbackHandlerFactory(func(instanceID string) http.Handler {
		return testAuthorizeMux(m, instanceID)
	}))
	require.NoError(t, m.Start(context.Background()))
	t.Cleanup(func() { _ = m.Close() })
	sha := m.catalogSHA
	requestEnabledAccept(t, m, "sample", "0", sha, true)
	waitActiveOpDone(t, m)
	downloaded := lifecycleResult(t, m.Observe("sample"))
	require.Equal(t, packages.LifecycleEnabled, downloaded.State)
	require.False(t, downloaded.Unconfirmed)
	require.NoError(t, packages.ValidateInstalled(root, sampleEntry))
}

func buildSampleArchive(t *testing.T) []byte {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	backend, err := os.ReadFile(executable)
	require.NoError(t, err)
	manifest := `{"protocol":1,"id":"sample","name":"Test","version":"1.0.0","executable":"backend","capabilities":["native.actor.read"],"lifecycle_controller":"controller"}`
	files := map[string][]byte{
		"extension.json": []byte(manifest),
		"backend":        backend,
		"LICENSE":        []byte("test fixture"),
	}
	var archive bytes.Buffer
	unzip, err := gzip.NewWriterLevel(&archive, gzip.BestSpeed)
	require.NoError(t, err)
	writer := tar.NewWriter(unzip)
	for name, body := range files {
		mode := int64(0o644)
		if name == "backend" {
			mode = 0o755
		}
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Format: tar.FormatUSTAR}))
		_, err := writer.Write(body)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	require.NoError(t, unzip.Close())
	return archive.Bytes()
}

// The authorize matrix pins callback validation without processes: token
// variant, deadline, instance, pin, UUID, generation, pending/desired,
// and body equality each fail closed with the mapped HTTP status.
func TestVerifyLifecycleCallMatrix(t *testing.T) {
	root := shortTestRoot(t)
	m := NewManager(root)
	controllerEntry := testCatalogEntry("controller", "1.0.0", true, "")
	sampleEntry := testCatalogEntry("sample", "1.0.0", false, "controller")
	m.catalog = []sdk.CatalogPackage{controllerEntry, sampleEntry}
	m.controllers = map[string]controllerState{"controller": {instanceID: "instance-a", ready: true}}
	installation, err := packages.EnsureInstallation(root, "sample")
	require.NoError(t, err)
	activate := packages.IntentActivate
	_, err = packages.StoreIntent(root, "sample", installation, true, &activate, "0")
	require.NoError(t, err)

	call := sdk.LifecycleCall{ControllerID: "controller", ControllerInstanceID: "instance-a", PackageID: "sample", PackageVersion: "1.0.0", PackageSHA256: sampleEntry.SHA256, InstallationID: installation, Generation: "1", Operation: sdk.LifecycleObserve, DeadlineUnix: time.Now().Unix() + 60}
	issue := func(call sdk.LifecycleCall, desired, cleanup bool) string {
		token, err := m.IssueLifecycleAdmission(&BackgroundAdmission{Lifecycle: true, InstallationID: installation, InstanceID: "instance-a", Call: call, DesiredEnabled: desired, CleanupException: cleanup})
		require.NoError(t, err)
		return token
	}
	verification, authErr := m.VerifyLifecycleCall(issue(call, true, false), call)
	require.Nil(t, authErr)
	require.True(t, verification.DesiredEnabled)

	_, authErr = m.VerifyLifecycleCall("bogus", call)
	require.Error(t, authErr)
	require.Equal(t, 403, authErr.HTTP)

	expired := call
	expired.DeadlineUnix = time.Now().Unix() - 1
	_, authErr = m.VerifyLifecycleCall(issue(expired, true, false), expired)
	require.Error(t, authErr)
	require.Equal(t, 403, authErr.HTTP)

	mismatched := call
	mismatched.Generation = "99"
	_, authErr = m.VerifyLifecycleCall(issue(call, true, false), mismatched)
	require.Error(t, authErr)
	require.Equal(t, 403, authErr.HTTP)

	activateCall := call
	activateCall.Operation = sdk.LifecycleActivate
	_, authErr = m.VerifyLifecycleCall(issue(activateCall, true, false), activateCall)
	require.Nil(t, authErr)

	deactivateCall := call
	deactivateCall.Operation = sdk.LifecycleDeactivate
	_, authErr = m.VerifyLifecycleCall(issue(deactivateCall, false, false), deactivateCall)
	require.Error(t, authErr, "deactivate must not verify while desired is true")
	require.Equal(t, 403, authErr.HTTP)
}
