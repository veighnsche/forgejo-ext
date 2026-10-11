// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import "testing"

func TestCreationRecoverySelectorsFitNativeIDs(t *testing.T) {
	const largest = "9223372036854775807"
	req := CreationRecoveryRequest{RepositoryID: largest, ActorID: largest, AfterID: largest, Limit: CreationRecoveryPageLimit}
	if err := ValidateCreationRecoveryRequest(req); err != nil {
		t.Fatal("maximum native ID rejected", err)
	}
	for _, invalid := range []string{"9223372036854775808", "18446744073709551615", "0", "01", "-1"} {
		for _, field := range []string{"repository", "actor", "cursor"} {
			t.Run(field+"/"+invalid, func(t *testing.T) {
				candidate := req
				switch field {
				case "repository":
					candidate.RepositoryID = invalid
				case "actor":
					candidate.ActorID = invalid
				case "cursor":
					candidate.AfterID = invalid
				}
				if err := ValidateCreationRecoveryRequest(candidate); err == nil {
					t.Fatal("invalid selector admitted")
				}
			})
		}
	}
}
