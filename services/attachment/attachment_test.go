// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package attachment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forgejo.org/models/db"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

func TestUploadAttachment(t *testing.T) {
	require.NoError(t, unittest.PrepareTestDatabase())

	user := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 1})

	fPath := "./attachment_test.go"
	f, err := os.Open(fPath)
	require.NoError(t, err)
	defer f.Close()

	attach, err := NewAttachment(db.DefaultContext, &repo_model.Attachment{
		RepoID:     1,
		UploaderID: user.ID,
		Name:       filepath.Base(fPath),
	}, f, -1)
	require.NoError(t, err)

	attachment, err := repo_model.GetAttachmentByUUID(db.DefaultContext, attach.UUID)
	require.NoError(t, err)
	assert.Equal(t, user.ID, attachment.UploaderID)
	assert.Equal(t, int64(0), attachment.DownloadCount)

	assert.NotEmpty(t, attachment.Digest)
	assert.True(t, strings.HasPrefix(attachment.Digest, "sha256:"),
		"digest should start with sha256:, got: %s", attachment.Digest)
	assert.Len(t, attachment.Digest, len("sha256:")+64,
		"digest should be sha256:<64 hex chars>, got: %s", attachment.Digest)
}
