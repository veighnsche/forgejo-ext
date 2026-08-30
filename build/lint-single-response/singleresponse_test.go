// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package singleresponse

import (
	"testing"

	"forgejo.org/modules/testhelper"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestSingleResponse(t *testing.T) {
	testhelper.Setup(t)
	analysistest.Run(t, analysistest.TestData(), Analyzer, "a")
}
