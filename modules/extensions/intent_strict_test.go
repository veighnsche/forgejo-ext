// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func writeIntentDoc(t *testing.T, root, id, doc string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".intents"), 0o700))
	require.NoError(t, os.WriteFile(IntentPath(root, id), []byte(doc), 0o600))
}

// An on-disk generation "0" is malformed: the virtual absent reading
// exists only for a missing file.
func TestLoadIntentRejectsPersistedZeroGeneration(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "sample")
	writeIntentDoc(t, root, "sample", `{"schema":1,"package_id":"sample","installation_id":"`+installation+`","generation":"0","desired_enabled":false,"pending_operation":null}`)
	_, err := LoadIntent(root, "sample")
	require.Error(t, err)
}

// Omitted booleans and markers never default silently.
func TestLoadIntentRejectsMissingFields(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "sample")
	docs := []string{
		`{"schema":1,"package_id":"sample","installation_id":"` + installation + `","generation":"1","pending_operation":null}`,
		`{"schema":1,"package_id":"sample","installation_id":"` + installation + `","generation":"1","desired_enabled":true}`,
		`{"schema":1,"package_id":"sample","generation":"1","desired_enabled":true,"pending_operation":null}`,
		`{"schema":1,"installation_id":"` + installation + `","generation":"1","desired_enabled":true,"pending_operation":null}`,
	}
	for _, doc := range docs {
		writeIntentDoc(t, root, "sample", doc)
		_, err := LoadIntent(root, "sample")
		require.Error(t, err, "doc: %s", doc)
	}
}

func TestLoadIntentRejectsDuplicateKeys(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "sample")
	writeIntentDoc(t, root, "sample", `{"schema":1,"package_id":"sample","package_id":"sample","installation_id":"`+installation+`","generation":"1","desired_enabled":true,"pending_operation":null}`)
	_, err := LoadIntent(root, "sample")
	require.Error(t, err)
}

func TestLoadIntentRejectsEscapedEquivalentDuplicates(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "sample")
	// The escaped key decodes to desired_enabled: two values for one
	// field are rejected by decoded spelling, not raw bytes.
	writeIntentDoc(t, root, "sample", `{"schema":1,"package_id":"sample","installation_id":"`+installation+`","generation":"1","desired_enabled":true,"desired_`+"\\u0065"+`nabled":false,"pending_operation":null}`)
	_, err := LoadIntent(root, "sample")
	require.Error(t, err)
}

// A null desired flag is not an explicit boolean: encoding/json would
// otherwise decode it as false and silently manufacture disabled state.
// pending_operation stays nullable by contract.
func TestLoadIntentRejectsNullDesiredEnabled(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "sample")
	writeIntentDoc(t, root, "sample", `{"schema":1,"package_id":"sample","installation_id":"`+installation+`","generation":"1","desired_enabled":null,"pending_operation":null}`)
	_, err := LoadIntent(root, "sample")
	require.Error(t, err)
}

func TestLoadIntentRejectsTrailingDataAndWrongTypes(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "sample")
	writeIntentDoc(t, root, "sample", `{"schema":1,"package_id":"sample","installation_id":"`+installation+`","generation":"1","desired_enabled":true,"pending_operation":null} trailing`)
	_, err := LoadIntent(root, "sample")
	require.Error(t, err)
	writeIntentDoc(t, root, "sample", `{"schema":1,"package_id":"sample","installation_id":"`+installation+`","generation":1,"desired_enabled":true,"pending_operation":null}`)
	_, err = LoadIntent(root, "sample")
	require.Error(t, err)
	writeIntentDoc(t, root, "sample", `{"schema":1,"package_id":"sample","installation_id":"`+installation+`","generation":"1","desired_enabled":"true","pending_operation":null}`)
	_, err = LoadIntent(root, "sample")
	require.Error(t, err)
	writeIntentDoc(t, root, "sample", `{"schema":1,"package_id":"sample","installation_id":"`+installation+`","generation":"1","desired_enabled":true,"pending_operation":null,"extra":1}`)
	_, err = LoadIntent(root, "sample")
	require.Error(t, err)
}

// Pending begin, successful completion, and boot reconciliation preserve
// the generation, installation UUID, and desired flag: only an explicit
// accepted retry increments, exactly once.
func TestPendingSameGenerationPreservesIdentity(t *testing.T) {
	root := t.TempDir()
	installation := ensureTestInstallation(t, root, "sample")

	begin, err := StoreIntent(root, "sample", installation, true, new(IntentActivate), "0")
	require.NoError(t, err)
	require.Equal(t, "1", begin.Generation)

	marked, err := StorePendingSameGeneration(root, "sample", true, new(IntentActivate), "1")
	require.NoError(t, err)
	require.Equal(t, "1", marked.Generation)
	require.Equal(t, installation, marked.InstallationID)
	require.True(t, marked.DesiredEnabled)

	completed, err := StorePendingSameGeneration(root, "sample", true, nil, "1")
	require.NoError(t, err)
	require.Equal(t, "1", completed.Generation)
	require.Equal(t, installation, completed.InstallationID)
	require.True(t, completed.DesiredEnabled)
	require.Nil(t, completed.PendingOperation)

	loaded, err := LoadIntent(root, "sample")
	require.NoError(t, err)
	require.Equal(t, completed, loaded)

	_, err = StorePendingSameGeneration(root, "sample", true, nil, "0")
	require.ErrorContains(t, err, "changed")

	retry, err := StoreIntent(root, "sample", installation, true, new(IntentActivate), "1")
	require.NoError(t, err)
	require.Equal(t, "2", retry.Generation)
}

func TestPendingSameGenerationRejectsVirtual(t *testing.T) {
	root := t.TempDir()
	_, err := StorePendingSameGeneration(root, "sample", true, new(IntentActivate), "0")
	require.Error(t, err)
}
