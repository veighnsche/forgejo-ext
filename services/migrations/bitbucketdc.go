// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package migrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"forgejo.org/modules/log"
	base "forgejo.org/modules/migration"
	"forgejo.org/modules/structs"
	"forgejo.org/services/migrations/allowlist"
)

var (
	_ base.Downloader        = &BitbucketDataCenterDownloader{}
	_ base.DownloaderFactory = &BitbucketDataCenterDownloaderFactory{}

	errBitbucketNotFound = errors.New("bitbucket resource not found")
)

func init() {
	RegisterDownloaderFactory(&BitbucketDataCenterDownloaderFactory{})
}

// BitbucketDataCenterDownloaderFactory defines a downloader factory
type BitbucketDataCenterDownloaderFactory struct{}

// New returns a downloader related to this factory according MigrateOptions
func (f *BitbucketDataCenterDownloaderFactory) New(ctx context.Context, opts base.MigrateOptions) (base.Downloader, error) {
	u, err := url.Parse(opts.CloneAddr)
	if err != nil {
		return nil, err
	}

	baseURL, project, repoSlug, err := parseBitbucketDataCenterURL(u)
	if err != nil {
		return nil, err
	}

	log.Trace("Create Bitbucket Data Center downloader. BaseURL: %s Project: %s Repo: %s", baseURL, project, repoSlug)
	return NewBitbucketDataCenterDownloader(ctx, baseURL, project, repoSlug, opts.AuthToken), nil
}

// GitServiceType returns the type of git service.
func (f *BitbucketDataCenterDownloaderFactory) GitServiceType() structs.GitServiceType {
	return structs.BitbucketDataCenterService
}

// parseBitbucketDataCenterURL extracts the REST base URL, project key and repo slug from a Bitbucket clone URL: [context]/scm/{project}/{slug}.git
func parseBitbucketDataCenterURL(u *url.URL) (baseURL *url.URL, project, repoSlug string, err error) {
	ctxPath, after, ok := strings.Cut("/"+strings.Trim(u.Path, "/"), "/scm/")
	fields := strings.SplitN(strings.Trim(after, "/"), "/", 2)
	if !ok || len(fields) != 2 {
		return nil, "", "", fmt.Errorf("invalid Bitbucket clone path, expected …/scm/{project}/{repository}: %s", u.Path)
	}
	return &url.URL{Scheme: u.Scheme, Host: u.Host, Path: ctxPath}, fields[0], strings.TrimSuffix(fields[1], ".git"), nil
}

// BitbucketDataCenterDownloader implements Downloader for Bitbucket Data Center / Server.
type BitbucketDataCenterDownloader struct {
	base.NullDownloader
	ctx      context.Context
	client   *http.Client
	baseURL  *url.URL
	project  string
	repoSlug string
	token    string
}

// NewBitbucketDataCenterDownloader creates a Bitbucket Data Center downloader.
func NewBitbucketDataCenterDownloader(ctx context.Context, baseURL *url.URL, project, repoSlug, token string) *BitbucketDataCenterDownloader {
	return &BitbucketDataCenterDownloader{
		ctx:      ctx,
		client:   allowlist.NewMigrationHTTPClient(),
		baseURL:  baseURL,
		project:  project,
		repoSlug: repoSlug,
		token:    token,
	}
}

// SetContext set context
func (d *BitbucketDataCenterDownloader) SetContext(ctx context.Context) {
	d.ctx = ctx
}

// String implements Stringer
func (d *BitbucketDataCenterDownloader) String() string {
	return fmt.Sprintf("migration from Bitbucket Data Center server %s %s/%s", d.baseURL, d.project, d.repoSlug)
}

func (d *BitbucketDataCenterDownloader) LogString() string {
	if d == nil {
		return "<BitbucketDataCenterDownloader nil>"
	}
	return fmt.Sprintf("<BitbucketDataCenterDownloader %s %s/%s>", d.baseURL, d.project, d.repoSlug)
}

// FormatCloneURL injects credentials into the clone URL; the token is the Basic-auth password,
// with the account username for user tokens or "x-token-auth" for project/repository tokens.
func (d *BitbucketDataCenterDownloader) FormatCloneURL(opts base.MigrateOptions, remoteAddr string) (string, error) {
	u, err := url.Parse(remoteAddr)
	if err != nil {
		return "", err
	}
	if opts.AuthToken != "" {
		user := opts.AuthUsername
		if user == "" {
			user = "x-token-auth"
		}
		u.User = url.UserPassword(user, opts.AuthToken)
	} else if opts.AuthUsername != "" {
		u.User = url.UserPassword(opts.AuthUsername, opts.AuthPassword)
	}
	return u.String(), nil
}

// callAPI performs a GET against the Bitbucket Data Center REST API v1 and decodes the JSON result.
func (d *BitbucketDataCenterDownloader) callAPI(endpoint string, result any) error {
	u := d.baseURL.JoinPath("rest/api/1.0", endpoint)

	req, err := http.NewRequestWithContext(d.ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	if d.token != "" {
		req.Header.Set("Authorization", "Bearer "+d.token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return errBitbucketNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: unexpected status %d", endpoint, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(result)
}

// GetRepoInfo returns repository information.
// https://docs.atlassian.com/bitbucket-server/rest/latest/bitbucket-rest.html
func (d *BitbucketDataCenterDownloader) GetRepoInfo() (*base.Repository, error) {
	var repo struct {
		Slug        string `json:"slug"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Public      bool   `json:"public"`
		Links       struct {
			Clone []struct {
				Href string `json:"href"`
				Name string `json:"name"`
			} `json:"clone"`
			Self []struct {
				Href string `json:"href"`
			} `json:"self"`
		} `json:"links"`
	}
	if err := d.callAPI(fmt.Sprintf("projects/%s/repos/%s", d.project, d.repoSlug), &repo); err != nil {
		return nil, err
	}

	var cloneURL string
	for _, c := range repo.Links.Clone {
		if c.Name == "http" || c.Name == "https" {
			cloneURL = c.Href
			break
		}
	}
	if cloneURL == "" {
		return nil, fmt.Errorf("no HTTP clone link for %s/%s: HTTP(S) SCM hosting is probably disabled on the Bitbucket instance", d.project, d.repoSlug)
	}

	var originalURL string
	if len(repo.Links.Self) > 0 {
		originalURL = repo.Links.Self[0].Href
	}

	// Default branch is absent (404) for empty repositories; warn on any other failure.
	var defaultBranch struct {
		DisplayID string `json:"displayId"`
	}
	if err := d.callAPI(fmt.Sprintf("projects/%s/repos/%s/branches/default", d.project, d.repoSlug), &defaultBranch); err != nil && !errors.Is(err, errBitbucketNotFound) {
		log.Warn("Unable to get the default branch of %s/%s: %v", d.project, d.repoSlug, err)
	}

	name := repo.Slug
	if name == "" {
		name = d.repoSlug
	}

	return &base.Repository{
		Name:          name,
		Owner:         d.project,
		Description:   repo.Description,
		IsPrivate:     !repo.Public,
		CloneURL:      cloneURL,
		OriginalURL:   originalURL,
		DefaultBranch: defaultBranch.DisplayID,
	}, nil
}
