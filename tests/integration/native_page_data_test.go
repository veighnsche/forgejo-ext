// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package integration

import (
	"encoding/json" //nolint:depguard // Native protocol and persisted JSON require standard-library semantics.
	"net/http"
	"testing"

	"forgejo.org/models/db"
	user_model "forgejo.org/models/user"
	"forgejo.org/tests"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Read real handler output, including every leaf's declarative data. Parsing
// script text directly exercises html/template's JSON escaping, not a browser
// evaluator or a synthetic reconstruction of the page configuration.
func nativePageJSON(t *testing.T, doc *HTMLDoc) map[string][]json.RawMessage {
	t.Helper()
	blocks := make(map[string][]json.RawMessage)
	doc.Find(`script[type="application/json"][data-native-page-data]`).Each(func(_ int, node *goquery.Selection) {
		key, exists := node.Attr("data-native-page-data")
		require.True(t, exists)
		require.NotEmpty(t, key)
		var data json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(node.Text()), &data), "native page JSON %q", key)
		blocks[key] = append(blocks[key], data)
	})
	return blocks
}

func decodeNativePageJSON[T any](t *testing.T, blocks map[string][]json.RawMessage, key string, index int) T {
	t.Helper()
	require.Contains(t, blocks, key)
	require.Greater(t, len(blocks[key]), index)
	var value T
	require.NoError(t, json.Unmarshal(blocks[key][index], &value), "native page data %q[%d]", key, index)
	return value
}

func TestNativePageDataRendering(t *testing.T) {
	defer tests.PrepareTestEnv(t)()
	session := loginUser(t, "user2")
	page := func(t *testing.T, route string) (*HTMLDoc, map[string][]json.RawMessage) {
		t.Helper()
		response := session.MakeRequest(t, NewRequest(t, http.MethodGet, route), http.StatusOK)
		doc := NewHTMLParser(t, response.Body)
		doc.AssertElement(t, ".page-content", true)
		doc.AssertElement(t, "#navbar", true)
		return doc, nativePageJSON(t, doc)
	}

	t.Run("dashboard", func(t *testing.T) {
		doc, blocks := page(t, "/")
		require.Len(t, blocks["dashboardRepoList"], 1)
		data := decodeNativePageJSON[struct {
			TextMyRepos           string
			TextSearchRepos       string
			IsStarsEnabled        bool
			IsOrganization        bool
			CanCreateOrganization bool
			OrganizationID        *int64
			Organizations         []struct{ Name string }
		}](t, blocks, "dashboardRepoList", 0)
		assert.Equal(t, "Repositories", data.TextMyRepos)
		assert.NotEmpty(t, data.TextSearchRepos)
		assert.True(t, data.IsStarsEnabled)
		assert.False(t, data.IsOrganization)
		assert.Nil(t, data.OrganizationID)
		assert.True(t, data.CanCreateOrganization)
		assert.NotNil(t, data.Organizations)
		doc.AssertElement(t, "#dashboard-repo-list", true)
		doc.AssertElement(t, "#navbar a[href='/user/settings']", true)
	})

	t.Run("repository branch", func(t *testing.T) {
		doc, blocks := page(t, "/user2/repo1/src/branch/master")
		require.Len(t, blocks["branchDropdownDataList"], 1)
		data := decodeNativePageJSON[struct {
			RepoLink               string
			RepoDefaultBranch      string
			DefaultSelectedRefName string
			BranchName             string
			ViewType               string
			BranchURLPrefix        string
			TextBranches           string
		}](t, blocks, "branchDropdownDataList", 0)
		assert.Equal(t, "/user2/repo1", data.RepoLink)
		assert.Equal(t, "master", data.RepoDefaultBranch)
		assert.Equal(t, "master", data.DefaultSelectedRefName)
		assert.Equal(t, "master", data.BranchName)
		assert.Equal(t, "branch", data.ViewType)
		assert.Equal(t, "/user2/repo1/src/branch/", data.BranchURLPrefix)
		assert.Equal(t, "Branches", data.TextBranches)
		doc.AssertElement(t, ".js-branch-tag-selector", true)
		doc.AssertElement(t, "#repo-clone-url[readonly]", true)
		doc.AssertElement(t, "a[href='/user2/repo1/issues']", true)
	})

	t.Run("multiple release selectors", func(t *testing.T) {
		doc, blocks := page(t, "/user2/repo1/releases")
		require.GreaterOrEqual(t, len(blocks["branchDropdownDataList"]), 2)
		require.Len(t, blocks["branchDropdownDataList"], doc.Find(".js-branch-tag-selector").Length())
		tags := make(map[string]bool)
		for i := range blocks["branchDropdownDataList"] {
			data := decodeNativePageJSON[struct {
				RepoLink     string
				TagURLPrefix string
				TagURLSuffix string
				Release      *struct{ TagName string }
			}](t, blocks, "branchDropdownDataList", i)
			assert.Equal(t, "/user2/repo1", data.RepoLink)
			assert.Equal(t, "/user2/repo1/compare/", data.TagURLPrefix)
			require.NotNil(t, data.Release)
			assert.Equal(t, "..."+data.Release.TagName, data.TagURLSuffix)
			tags[data.Release.TagName] = true
		}
		assert.Contains(t, tags, "v1.0")
		assert.Contains(t, tags, "v1.1")
		doc.AssertElement(t, "a[href='/user2/repo1/releases/tag/v1.1']", true)
	})

	t.Run("pull merge and native context", func(t *testing.T) {
		doc, blocks := page(t, "/user2/repo1/pulls/5")
		require.Len(t, blocks["pullRequestMergeForm"], 1)
		data := decodeNativePageJSON[struct {
			BaseLink          string
			PullHeadCommitID  string
			DefaultMergeStyle string
			TextCancel        string
			MergeStyles       []struct {
				Name                string
				Allowed             bool
				MergeTitleFieldText string
			}
		}](t, blocks, "pullRequestMergeForm", 0)
		assert.Equal(t, "/user2/repo1/pulls/5", data.BaseLink)
		assert.Regexp(t, `^[a-f0-9]{40}$`, data.PullHeadCommitID)
		assert.Equal(t, "merge", data.DefaultMergeStyle)
		assert.Equal(t, "Cancel", data.TextCancel)
		require.Len(t, data.MergeStyles, 6)
		assert.Equal(t, "merge", data.MergeStyles[0].Name)
		assert.True(t, data.MergeStyles[0].Allowed)
		assert.Contains(t, data.MergeStyles[0].MergeTitleFieldText, "pull5")
		assert.Equal(t, "5", doc.GetInputValueByID("issueIndex"))
		assert.Equal(t, "1", doc.GetInputValueByID("repoId"))
		doc.AssertElement(t, "#pull-request-merge-form", true)
		doc.AssertElement(t, "form#comment-form[action='/user2/repo1/issues/5/comments'][method='post']", true)
	})

	t.Run("pull diff metadata", func(t *testing.T) {
		doc, blocks := page(t, "/user2/repo1/pulls/3/files")
		require.Len(t, blocks["diffFileInfo"], 1)
		data := decodeNativePageJSON[struct {
			Files []struct {
				Name     string
				NameHash string
				Type     int
				IsBin    bool
				Addition int
				Deletion int
				IsViewed bool
			}
			IsIncomplete      bool
			BinaryFileMessage string
			LinkLoadMore      string
		}](t, blocks, "diffFileInfo", 0)
		require.NotEmpty(t, data.Files)
		assert.False(t, data.IsIncomplete)
		assert.NotEmpty(t, data.BinaryFileMessage)
		assert.Contains(t, data.LinkLoadMore, "file-only=true")
		assert.Len(t, data.Files, doc.Find(".diff-file-box.file-content").Length())
		filenames := make([]string, 0, len(data.Files))
		for _, file := range data.Files {
			filenames = append(filenames, file.Name)
			assert.NotEmpty(t, file.NameHash)
			assert.Positive(t, file.Type)
			assert.False(t, file.IsViewed)
			assert.NotZero(t, file.Addition+file.Deletion)
			node := doc.Find("#diff-" + file.NameHash)
			require.Equal(t, 1, node.Length())
			assert.Equal(t, file.Name, node.AttrOr("data-new-filename", ""))
		}
		assert.ElementsMatch(t, []string{"3", "iso-8859-1.txt"}, filenames)
		doc.AssertElement(t, "#diff-container", true)
		doc.AssertElement(t, "#review-box", true)
	})

	t.Run("mention escaping and native comment controls", func(t *testing.T) {
		// Only the prepared integration database is changed. Real account text
		// containing quotes, a newline and a script terminator must round-trip.
		name := "Native \"quoted\" café <name> & \\ text\n</script>"
		original := new(user_model.User)
		exists, err := db.GetEngine(db.DefaultContext).ID(1).Get(original)
		require.NoError(t, err)
		require.True(t, exists)
		t.Cleanup(func() {
			_, restoreErr := db.GetEngine(db.DefaultContext).ID(1).Cols("full_name").Update(&user_model.User{FullName: original.FullName})
			require.NoError(t, restoreErr)
		})
		updated, err := db.GetEngine(db.DefaultContext).ID(1).Cols("full_name").Update(&user_model.User{FullName: name})
		require.NoError(t, err)
		require.EqualValues(t, 1, updated)
		doc, blocks := page(t, "/user2/repo1/issues/1")
		require.Len(t, blocks["mentionValues"], 1)
		data := decodeNativePageJSON[[]struct {
			Key      string
			Value    string
			Name     string
			Fullname string
			Avatar   string
		}](t, blocks, "mentionValues", 0)
		require.NotEmpty(t, data)
		found := false
		for _, mention := range data {
			if mention.Name != "user1" {
				continue
			}
			found = true
			assert.Equal(t, "user1", mention.Value)
			assert.Equal(t, name, mention.Fullname)
			assert.Equal(t, "user1 "+name, mention.Key)
			assert.NotEmpty(t, mention.Avatar)
		}
		assert.True(t, found, "native issue poster remains mentionable")
		assert.Equal(t, "1", doc.GetInputValueByID("issueIndex"))
		doc.AssertElement(t, "form#comment-form[action='/user2/repo1/issues/1/comments'][method='post']", true)
	})
}
