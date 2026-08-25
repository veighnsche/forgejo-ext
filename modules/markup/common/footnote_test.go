// Copyright 2023 The Gitea Authors. All rights reserved.
// Copyright 2023 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT
package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanValue(t *testing.T) {
	testhelper.Setup(t)
	tests := []struct {
		param  string
		expect string
	}{
		// Github behavior test cases
		{"", ""},
		{"test.0.1", "test-0-1"},
		{"test(0)", "test-0"},
		{"test!1", "test-1"},
		{"test:2", "test-2"},
		{"test*3", "test-3"},
		{"test！4", "test-4"},
		{"test：5", "test-5"},
		{"test*6", "test-6"},
		{"test：6 a", "test-6-a"},
		{"test：6 !b", "test-6-b"},
		{"test：ad # df", "test-ad-df"},
		{"test：ad #23 df 2*/*", "test-ad-23-df-2"},
		{"test：ad 23 df 2*/*", "test-ad-23-df-2"},
		{"test：ad # 23 df 2*/*", "test-ad-23-df-2"},
		{"Anchors in Markdown", "anchors-in-markdown"},
		{"a_b_c", "a_b_c"},
		{"a-b-c", "a-b-c"},
		{"a-b-c----", "a-b-c"},
		{"test：6a", "test-6a"},
		{"test：a6", "test-a6"},
		{"tes a a   a  a", "tes-a-a-a-a"},
		{"  tes a a   a  a  ", "tes-a-a-a-a"},
		{"Header with \"double quotes\"", "header-with-double-quotes"},
		{"Placeholder to force scrolling on link's click", "placeholder-to-force-scrolling-on-link-s-click"},
		{"tes（）", "tes"},
		{"tes（0）", "tes-0"},
		{"tes{0}", "tes-0"},
		{"tes[0]", "tes-0"},
		{"test【0】", "test-0"},
		{"tes…@a", "tes-a"},
		{"tes￥& a", "tes-a"},
		{"tes= a", "tes-a"},
		{"tes|a", "tes-a"},
		{"tes\\a", "tes-a"},
		{"tes/a", "tes-a"},
		{"a啊啊b", "a啊啊b"},
		{"c🤔️🤔️d", "c-d"},
		{"a⚡a", "a-a"},
		{"e.~f", "e-f"},
	}
	for _, test := range tests {
		assert.Equal(t, []byte(test.expect), CleanValue([]byte(test.param)), test.param)
	}
}
