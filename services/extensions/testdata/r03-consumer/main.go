// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// Command r03-consumer is the R03 independently built SDK consumer fixture:
// a standalone extension binary compiled from this source against the
// canonical SDK (resolved through the parent module's replace directive to
// the clean-tree sdk bytes, never a published version or a copied checkout).
// It carries no test scaffolding: unlike the in-package test helper, it has
// no TestMain branch and ignores FORGEJO_EXTENSION_TEST_HELPER. The manager
// spawns it as an ordinary package executable; a successful service
// bootstrap through it proves the real extension manager/package handshake.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"

	sdk "forgejo.org/extension-sdk"
)

func main() {
	application := sdk.Application{HTTP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authority, err := sdk.RequestContext(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, authority.ExtensionID+":"+authority.Actor.Username)
	})}
	if err := sdk.Serve(application); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
