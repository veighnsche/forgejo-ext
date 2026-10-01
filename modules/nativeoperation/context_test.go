// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package nativeoperation

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapabilityFileIsHostPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "exec")
	path, secret, err := WriteCapabilityFile(dir)
	require.NoError(t, err)
	require.NotEmpty(t, secret)
	require.Equal(t, dir, filepath.Dir(path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dirInfo, err := os.Stat(dir)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

	read, err := ReadCapabilityFile(path)
	require.NoError(t, err)
	require.Equal(t, secret, read)

	verifier := Verifier(secret)
	require.NotEmpty(t, verifier)
	require.NotContains(t, verifier, secret)
	require.True(t, VerifyProof(verifier, secret))
	require.False(t, VerifyProof(verifier, secret+"x"))
	require.False(t, VerifyProof(verifier, ""))
	require.False(t, VerifyProof("", secret))
}

func TestReadCapabilityFileRejectsLinksAndAbsence(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadCapabilityFile(filepath.Join(dir, "missing"))
	require.Error(t, err)

	target := filepath.Join(dir, "target")
	require.NoError(t, os.WriteFile(target, []byte("secret"), 0o600))
	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(target, link))
	_, err = ReadCapabilityFile(link)
	require.Error(t, err)

	_, err = ReadCapabilityFile("")
	require.Error(t, err)
}

func TestExecutionContextAndEnv(t *testing.T) {
	require.Nil(t, FromContext(context.Background()))
	exec := &Execution{Owner: "cond:i/op", Generation: 3, CapabilityPath: "/run/nativeop/exec-1"}
	ctx := NewContext(context.Background(), exec)
	require.Same(t, exec, FromContext(ctx))

	env := AppendExecEnv([]string{"A=1"}, exec)
	require.Contains(t, env, "A=1")
	require.Contains(t, env, EnvExecFile+"=/run/nativeop/exec-1")
	require.Equal(t, []string{"A=1"}, AppendExecEnv([]string{"A=1"}, nil))
}
