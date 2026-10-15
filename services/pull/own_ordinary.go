// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package pull

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	nativeoperation_model "forgejo.org/models/nativeoperation"
	execcontext "forgejo.org/modules/nativeoperation"
	"forgejo.org/modules/setting"
)

// Keep the leaf implementation of the canonical claim protocol shared by
// collaboration and ref writers; importing the service would create a cycle.
func withPullOwnership(ctx context.Context, family, resource, scopeJSON string, fn func(context.Context) error) error {
	if execcontext.FromContext(ctx) != nil {
		return fn(ctx)
	}
	// A canceled caller cannot hold the domain: claiming would strand
	// the reservation when the release below fails on the same ctx.
	if err := ctx.Err(); err != nil {
		return err
	}
	if resource == "" {
		return errors.New("ordinary ownership requires a writer identity")
	}

	dir := filepath.Join(setting.AppDataPath, "nativeop-exec")
	path, secret, err := execcontext.WriteCapabilityFile(dir)
	if err != nil {
		return err
	}
	nonce := make([]byte, 8)
	_, _ = rand.Read(nonce)
	owner := "ord:" + family + "/" + resource + "/" + hex.EncodeToString(nonce)
	release := func() {
		_ = os.Remove(path)
	}

	claimed, err := nativeoperation_model.ClaimOrdinary(ctx, owner, scopeJSON, execcontext.Verifier(secret))
	if err != nil {
		release()
		if errors.Is(err, nativeoperation_model.ErrBusy) {
			return fmt.Errorf("%w: ordinary writer %s is fenced", nativeoperation_model.ErrBusy, family)
		}
		if errors.Is(err, nativeoperation_model.ErrInhibited) {
			return fmt.Errorf("%w: ordinary writer %s refuses while offline recovery holds the domain", nativeoperation_model.ErrInhibited, family)
		}
		return err
	}
	owned := execcontext.NewContext(ctx, &execcontext.Execution{Owner: owner, Generation: claimed.Generation, CapabilityPath: path})
	fnErr := fn(owned)
	if err := nativeoperation_model.ReleaseOwner(context.WithoutCancel(ctx), owner); err != nil {
		release()
		if fnErr != nil {
			return fmt.Errorf("%w (and failed to release %s: %v)", fnErr, owner, err)
		}
		return fmt.Errorf("failed to release %s: %w", owner, err)
	}
	release()
	return fnErr
}
