// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	extension "forgejo.org/extension-sdk"
	"forgejo.org/models/db"
	"forgejo.org/models/perm"
	access_model "forgejo.org/models/perm/access"
	repo_model "forgejo.org/models/repo"
	"forgejo.org/models/unittest"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/git"

	_ "forgejo.org/modules/testimport"

	"code.forgejo.org/go-chi/session"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	unittest.MainTest(m)
}

type sourceTestSession struct {
	session.Store
	uid    int64
	reads  int
	onRead func(int)
}

func (s *sourceTestSession) Read(string) (session.RawStore, error) {
	s.reads++
	if s.onRead != nil {
		s.onRead(s.reads)
	}
	return callbackTestRawStore{uid: s.uid}, nil
}

type sourceTestFixture struct {
	repository *repo_model.Repository
	work       string
	base, tip  string
	session    *sourceTestSession
	authority  extension.Authority
	token      string
}

func newSourceTestFixture(t *testing.T) sourceTestFixture {
	t.Helper()
	unittest.PrepareTestEnv(t)
	repository := unittest.AssertExistsAndLoadBean(t, &repo_model.Repository{ID: 2})
	require.NoError(t, os.RemoveAll(repository.RepoPath()))
	work := t.TempDir()
	sourceTestGit(t, work, nil, "init", "--initial-branch=master")
	for name, content := range map[string][]byte{
		"setup.sh": []byte("#!/bin/sh\nprintf setup\\n\n"),
		"check.sh": []byte("#!/bin/sh\nprintf check\\n\n"),
		"README":   []byte("original source\n"),
		"large":    bytes.Repeat([]byte("x"), maxSourceFileBytes+1),
	} {
		require.NoError(t, os.WriteFile(filepath.Join(work, name), content, 0o644))
	}
	require.NoError(t, os.Chmod(filepath.Join(work, "setup.sh"), 0o755))
	require.NoError(t, os.Symlink("README", filepath.Join(work, "link")))
	for i := range 5 {
		require.NoError(t, os.WriteFile(filepath.Join(work, "bounded-"+strconv.Itoa(i)), bytes.Repeat([]byte{byte('a' + i)}, maxSourceFileBytes), 0o644))
	}
	sourceTestGit(t, work, nil, "add", ".")
	sourceTestGit(t, work, nil, "commit", "-m", "approved base")
	base := sourceTestGit(t, work, nil, "rev-parse", "HEAD")
	require.NoError(t, os.WriteFile(filepath.Join(work, "README"), []byte("current source\n"), 0o644))
	sourceTestGit(t, work, nil, "add", "README")
	sourceTestGit(t, work, nil, "update-index", "--add", "--cacheinfo", "160000", base, "module")
	sourceTestGit(t, work, nil, "commit", "-m", "default branch tip")
	tip := sourceTestGit(t, work, nil, "rev-parse", "HEAD")
	require.NoError(t, os.MkdirAll(filepath.Dir(repository.RepoPath()), 0o755))
	sourceTestGit(t, work, nil, "clone", "--bare", work, repository.RepoPath())
	actor := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 2})
	store := &sourceTestSession{uid: actor.ID}
	authority := extension.Authority{
		ExtensionID: "soda", InstanceID: "source-test", SessionGeneration: "source-test-generation",
		Contribution: extension.Contribution{ID: "spaces", Kind: "page", Scope: "global", Action: "post"},
		Actor:        extension.Actor{ID: "2", Username: actor.Name, SiteAdmin: actor.IsAdmin},
	}
	requestCtx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	token, err := nativeAdmissions.issue(&callbackAdmission{
		instanceID: "source-test", actorID: actor.ID, sessionID: "source-test-session", session: store,
		authority: authority, capabilities: map[string]struct{}{extension.CapabilityRepositoryRead: {}},
		requestCtx: requestCtx, cancel: cancel,
	})
	require.NoError(t, err)
	t.Cleanup(func() { nativeAdmissions.revoke(token) })
	return sourceTestFixture{repository: repository, work: work, base: base, tip: tip, session: store, authority: authority, token: token}
}

func sourceTestGit(t *testing.T, dir string, input []byte, args ...string) string {
	t.Helper()
	// Every argument comes from this fixture's fixed commands or generated
	// local paths/object IDs; no callback request enters this test helper.
	out, _, err := git.NewCommand(t.Context()).AddArguments(git.ToTrustedCmdArgs(args)...).RunStdString(&git.RunOpts{
		Dir: dir, Stdin: bytes.NewReader(input),
		Env: []string{"GIT_AUTHOR_NAME=Source test", "GIT_AUTHOR_EMAIL=source@example.invalid", "GIT_COMMITTER_NAME=Source test", "GIT_COMMITTER_EMAIL=source@example.invalid"},
	})
	require.NoError(t, err)
	return strings.TrimSpace(out)
}

func (fixture sourceTestFixture) request(t *testing.T, request extension.CallbackRequest) (*httptest.ResponseRecorder, *extension.RepositorySource) {
	t.Helper()
	request.Operation = extension.OperationRepositorySource
	request.RepositoryID = "2"
	request.Authority = fixture.authority
	body, err := json.Marshal(request)
	require.NoError(t, err)
	httpRequest := httptest.NewRequest(http.MethodPost, extension.NativeCallbackPath, bytes.NewReader(body))
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set(extension.AdmissionHeader, fixture.token)
	response := httptest.NewRecorder()
	CallbackHandlerForInstance("source-test").ServeHTTP(response, httpRequest)
	if response.Code != http.StatusOK {
		require.NotContains(t, response.Body.String(), "source-test-session")
		require.NotContains(t, response.Body.String(), fixture.token)
		return response, nil
	}
	var decoded extension.CallbackResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &decoded))
	require.NotNil(t, decoded.Source)
	return response, decoded.Source
}

func TestRepositorySourceCallbackReturnsExactSelectedNativeBytesAndBundle(t *testing.T) {
	fixture := newSourceTestFixture(t)
	response, source := fixture.request(t, extension.CallbackRequest{SourcePaths: []string{"setup.sh", "check.sh", "README"}, SourceActors: []string{"2", "4", "3", "999999"}})
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	require.Equal(t, "2", source.RepositoryID)
	require.Equal(t, fixture.tip, source.Commit)
	require.Equal(t, []byte("current source\n"), source.Files["README"])
	approvedSetup, err := os.ReadFile(filepath.Join(fixture.work, "setup.sh"))
	require.NoError(t, err)
	require.Equal(t, sha256.Sum256(approvedSetup), sha256.Sum256(source.Files["setup.sh"]))
	require.Len(t, source.Files, 3)
	require.Equal(t, map[string]bool{"2": true, "4": false, "3": false, "999999": false}, source.Writers)
	bundlePath := filepath.Join(t.TempDir(), "source.bundle")
	require.NoError(t, os.WriteFile(bundlePath, source.Bundle, 0o600))
	clone := filepath.Join(t.TempDir(), "clone.git")
	sourceTestGit(t, fixture.work, nil, "clone", "--bare", bundlePath, clone)
	require.Equal(t, fixture.tip, sourceTestGit(t, clone, nil, "rev-parse", "HEAD"))

	response, source = fixture.request(t, extension.CallbackRequest{SourceCommit: fixture.base, SourcePaths: []string{"README"}})
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, fixture.base, source.Commit)
	require.Equal(t, []byte("original source\n"), source.Files["README"])
	require.Empty(t, source.Writers)
	response, source = fixture.request(t, extension.CallbackRequest{})
	require.Equal(t, http.StatusOK, response.Code)
	require.Empty(t, source.Files)
	require.NotEmpty(t, source.Bundle)
}

func TestRepositorySourceCallbackRefusesInvalidPathsObjectsAndBounds(t *testing.T) {
	fixture := newSourceTestFixture(t)
	for _, name := range []string{"../setup.sh", "/setup.sh", "./setup.sh", "setup.sh/../README", ".git/config", "nested/.git/config", ".soda-home/secret", "a\\b", "a\x00b", ".", "..", "", strings.Repeat("x", 1025)} {
		t.Run("path "+strconv.Quote(name), func(t *testing.T) {
			response, source := fixture.request(t, extension.CallbackRequest{SourcePaths: []string{name}})
			require.Equal(t, http.StatusBadRequest, response.Code)
			require.Nil(t, source)
		})
	}
	for name, request := range map[string]extension.CallbackRequest{
		"duplicate path":     {SourcePaths: []string{"setup.sh", "setup.sh"}},
		"too many paths":     {SourcePaths: make([]string, maxSourcePaths+1)},
		"abbreviated SHA":    {SourceCommit: fixture.tip[:12]},
		"nonhex SHA":         {SourceCommit: strings.Repeat("z", 40)},
		"leading-zero actor": {SourceActors: []string{"02"}},
		"duplicate actor":    {SourceActors: []string{"2", "2"}},
		"too many actors":    {SourceActors: make([]string, 9)},
	} {
		t.Run(name, func(t *testing.T) {
			response, _ := fixture.request(t, request)
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
	for _, name := range []string{"missing", "link", "module"} {
		t.Run("object "+name, func(t *testing.T) {
			response, source := fixture.request(t, extension.CallbackRequest{SourcePaths: []string{name}})
			require.Equal(t, http.StatusNotFound, response.Code)
			require.Nil(t, source)
		})
	}
	response, _ := fixture.request(t, extension.CallbackRequest{SourcePaths: []string{"large"}})
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	response, source := fixture.request(t, extension.CallbackRequest{SourcePaths: []string{"bounded-0", "bounded-1", "bounded-2", "bounded-3"}})
	require.Equal(t, http.StatusOK, response.Code)
	require.Len(t, source.Files["bounded-0"], maxSourceFileBytes)
	response, _ = fixture.request(t, extension.CallbackRequest{SourcePaths: []string{"bounded-0", "bounded-1", "bounded-2", "bounded-3", "bounded-4"}})
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	response, _ = fixture.request(t, extension.CallbackRequest{SourceCommit: strings.Repeat("0", 40)})
	require.Equal(t, http.StatusNotFound, response.Code)
	tree := sourceTestGit(t, fixture.repository.RepoPath(), nil, "rev-parse", fixture.tip+"^{tree}")
	response, _ = fixture.request(t, extension.CallbackRequest{SourceCommit: tree})
	require.Equal(t, http.StatusNotFound, response.Code)
	unreachable := sourceTestGit(t, fixture.repository.RepoPath(), []byte("unrelated source\n"), "commit-tree", tree)
	response, _ = fixture.request(t, extension.CallbackRequest{SourceCommit: unreachable})
	require.Equal(t, http.StatusNotFound, response.Code)
}

func TestRepositorySourceCallbackBoundsActualNativeBundle(t *testing.T) {
	fixture := newSourceTestFixture(t)
	content := make([]byte, 1<<20)
	_, err := rand.Read(content)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(fixture.work, "unselected-large"), content, 0o644))
	sourceTestGit(t, fixture.work, nil, "add", "unselected-large")
	sourceTestGit(t, fixture.work, nil, "commit", "-m", "oversized source")
	sourceTestGit(t, fixture.repository.RepoPath(), nil, "fetch", fixture.work, "master:master")
	response, source := fixture.request(t, extension.CallbackRequest{SourcePaths: []string{"setup.sh"}})
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	require.Nil(t, source)
}

func TestRepositorySourceCallbackRechecksCurrentReaderAndWriterAuthority(t *testing.T) {
	fixture := newSourceTestFixture(t)
	_, err := db.GetEngine(t.Context()).Where("repo_id = ? AND user_id = ?", 2, 4).Delete(&access_model.Access{})
	require.NoError(t, err)
	_, err = db.GetEngine(t.Context()).Insert(&access_model.Access{RepoID: 2, UserID: 4, Mode: perm.AccessModeWrite})
	require.NoError(t, err)
	response, source := fixture.request(t, extension.CallbackRequest{SourceActors: []string{"4"}})
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, map[string]bool{"4": true}, source.Writers)
	_, err = db.GetEngine(t.Context()).Where("repo_id = ? AND user_id = ?", 2, 4).Delete(&access_model.Access{})
	require.NoError(t, err)
	response, source = fixture.request(t, extension.CallbackRequest{SourceActors: []string{"4"}})
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, map[string]bool{"4": false}, source.Writers)

	fixture.session.reads = 0
	fixture.session.onRead = func(read int) {
		if read == 2 {
			fixture.session.uid = 4
		}
	}
	response, source = fixture.request(t, extension.CallbackRequest{SourcePaths: []string{"README"}})
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Nil(t, source)
	fixture.session.onRead = nil
	fixture.session.uid = 2
	nativeAdmissions.revoke(fixture.token)
	response, source = fixture.request(t, extension.CallbackRequest{})
	require.Equal(t, http.StatusUnauthorized, response.Code)
	require.Nil(t, source)
}

func TestRepositorySourceCallbackRefusesCurrentPrivateRepositoryRevocation(t *testing.T) {
	fixture := newSourceTestFixture(t)
	reader := unittest.AssertExistsAndLoadBean(t, &user_model.User{ID: 4})
	entry := nativeAdmissions.get(fixture.token)
	entry.actorID = reader.ID
	entry.authority.Actor = extension.Actor{ID: "4", Username: reader.Name, SiteAdmin: reader.IsAdmin}
	fixture.authority = entry.authority
	fixture.session.uid = reader.ID
	_, err := db.GetEngine(t.Context()).Where("repo_id = ? AND user_id = ?", 2, 4).Delete(&access_model.Access{})
	require.NoError(t, err)
	_, err = db.GetEngine(t.Context()).Insert(&access_model.Access{RepoID: 2, UserID: 4, Mode: perm.AccessModeRead})
	require.NoError(t, err)
	fixture.session.onRead = func(read int) {
		if read == 2 {
			_, err := db.GetEngine(t.Context()).Where("repo_id = ? AND user_id = ?", 2, 4).Delete(&access_model.Access{})
			require.NoError(t, err)
		}
	}
	response, source := fixture.request(t, extension.CallbackRequest{SourcePaths: []string{"README"}})
	require.Equal(t, http.StatusNotFound, response.Code)
	require.Nil(t, source)
}
