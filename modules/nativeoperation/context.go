// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Package nativeoperation carries trusted native-operation execution context
// into participating native writers. It is dependency-neutral: it must not
// import models or services, so both the merge engine and the operation
// service can share it without an import cycle.
package nativeoperation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// EnvExecFile names the environment variable carrying the host-private
// execution-capability file path into a native Git child. Only the file's
// path travels in the environment; the secret stays in the 0600 file.
const EnvExecFile = "FORGEJO_NATIVEOP_EXEC"

// Owner kinds recorded in the durable reservation.
const (
	OwnerConditional = "conditional"
	OwnerOrdinary    = "ordinary"
)

// Execution binds one admitted native writer to its durable owner. Owner is
// the reservation owner string and Generation its fencing generation; neither
// is caller-selected authority.
type Execution struct {
	Owner          string
	Generation     int64
	CapabilityPath string
}

type executionContextKey struct{}

// NewContext returns a context carrying the execution for the final native
// Git child of this operation.
func NewContext(ctx context.Context, exec *Execution) context.Context {
	return context.WithValue(ctx, executionContextKey{}, exec)
}

// FromContext returns the carried execution, if any.
func FromContext(ctx context.Context) *Execution {
	exec, _ := ctx.Value(executionContextKey{}).(*Execution)
	return exec
}

// AppendExecEnv returns env with the execution-capability path for one native
// Git child. Callers must pass the result only to that child.
func AppendExecEnv(env []string, exec *Execution) []string {
	if exec == nil || exec.CapabilityPath == "" {
		return env
	}
	return append(env, EnvExecFile+"="+exec.CapabilityPath)
}

// WriteCapabilityFile creates a host-private capability file holding a fresh
// random secret. It returns the file path and the secret. The caller stores
// only Verifier(secret) durably and retires the file after reconciliation.
func WriteCapabilityFile(dir string) (path, secret string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	secret = base64.RawURLEncoding.EncodeToString(raw)
	file, err := os.CreateTemp(dir, "exec-*")
	if err != nil {
		return "", "", err
	}
	name := file.Name()
	if _, err := file.WriteString(secret); err != nil {
		_ = file.Close()
		_ = os.Remove(name)
		return "", "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", "", err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		_ = os.Remove(name)
		return "", "", err
	}
	// Keep the file inside dir even against surprising temp names.
	if filepath.Dir(name) != dir {
		_ = os.Remove(name)
		return "", "", errors.New("capability file escaped its directory")
	}
	return name, secret, nil
}

// ReadCapabilityFile reads a capability secret from a hook child environment
// path. It rejects missing, non-regular, symlinked and over-large files. The
// returned secret must never be logged or recorded.
func ReadCapabilityFile(path string) (string, error) {
	if path == "" || len(path) > 4096 {
		return "", errors.New("execution capability is unavailable")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("execution capability is unavailable")
	}
	if info.Size() == 0 || info.Size() > 4096 {
		return "", errors.New("execution capability is unavailable")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 4096 {
		return "", errors.New("execution capability is unavailable")
	}
	return string(raw), nil
}

// Verifier binds a capability secret for durable storage. The verifier
// detects the permitted execution without holding a reusable bearer.
func Verifier(secret string) string {
	sum := sha256.Sum256([]byte("native-operation-execution\x00" + secret))
	return hex.EncodeToString(sum[:])
}

// VerifyProof reports whether the presented proof matches the stored verifier.
func VerifyProof(verifier, proof string) bool {
	if verifier == "" || proof == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(verifier), []byte(Verifier(proof))) == 1
}
