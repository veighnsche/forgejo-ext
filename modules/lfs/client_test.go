// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package lfs

import (
	"net/url"
	"testing"

	"forgejo.org/modules/testhelper"

	"github.com/stretchr/testify/assert"
)

func TestNewClient(t *testing.T) {
	testhelper.Setup(t)
	u, _ := url.Parse("file:///test")
	c := NewClient(u, nil)
	assert.IsType(t, &FilesystemClient{}, c)

	u, _ = url.Parse("https://test.com/lfs")
	c = NewClient(u, nil)
	assert.IsType(t, &HTTPClient{}, c)
}
