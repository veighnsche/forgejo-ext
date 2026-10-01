// Copyright 2019 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package private

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	asymkey_model "forgejo.org/models/asymkey"
	"forgejo.org/models/perm"
	user_model "forgejo.org/models/user"
	"forgejo.org/modules/setting"
)

// KeyAndOwner is the response from ServNoCommand
type KeyAndOwner struct {
	Key   *asymkey_model.PublicKey `json:"key"`
	Owner *user_model.User         `json:"user"`
}

// ServNoCommand returns information about the provided key
func ServNoCommand(ctx context.Context, keyID int64) (*asymkey_model.PublicKey, *user_model.User, error) {
	reqURL := setting.LocalURL + fmt.Sprintf("api/internal/serv/none/%d", keyID)
	req := newInternalRequest(ctx, reqURL, "GET")
	keyAndOwner, extra := requestJSONResp(req, &KeyAndOwner{})
	if extra.HasError() {
		return nil, nil, extra.Error
	}
	return keyAndOwner.Key, keyAndOwner.Owner, nil
}

// ServCommandResults are the results of a call to the private route serv
type ServCommandResults struct {
	IsWiki      bool
	DeployKeyID int64
	KeyID       int64  // public key
	KeyName     string // this field is ambiguous, it can be the name of DeployKey, or the name of the PublicKey
	UserName    string
	UserEmail   string
	UserID      int64
	OwnerName   string
	RepoName    string
	RepoID      int64
	// Owner and Generation name the held SSH receive owner when the
	// server claimed the reservation for a receive-pack execution.
	// They are empty without a claim, and the serv process releases
	// them through ReleaseSSHReceive after the receiver exits.
	Owner      string
	Generation int64
}

// ServCommand preps for a serv call
func ServCommand(ctx context.Context, keyID int64, ownerName, repoName string, mode perm.AccessMode, verbs ...string) (*ServCommandResults, ResponseExtra) {
	return ServCommandWithReceive(ctx, keyID, ownerName, repoName, mode, "", verbs...)
}

// ServCommandWithReceive preps for a serv call, claiming the reservation
// for a receive-pack execution when verifier names the serv process's
// execution capability. The secret never crosses the channel: only its
// verifier travels, and the server holds no reusable bearer.
func ServCommandWithReceive(ctx context.Context, keyID int64, ownerName, repoName string, mode perm.AccessMode, verifier string, verbs ...string) (*ServCommandResults, ResponseExtra) {
	var reqURL strings.Builder
	reqURL.WriteString(setting.LocalURL + fmt.Sprintf("api/internal/serv/command/%d/%s/%s?mode=%d",
		keyID,
		url.PathEscape(ownerName),
		url.PathEscape(repoName),
		mode,
	))
	for _, verb := range verbs {
		if verb != "" {
			fmt.Fprintf(&reqURL, "&verb=%s", url.QueryEscape(verb))
		}
	}
	if verifier != "" {
		fmt.Fprintf(&reqURL, "&exec_verifier=%s", url.QueryEscape(verifier))
	}
	req := newInternalRequest(ctx, reqURL.String(), "GET")
	return requestJSONResp(req, &ServCommandResults{})
}

// SSHReleaseOption releases one held SSH receive owner after its
// receiver exits. Only the exact owner and generation release.
type SSHReleaseOption struct {
	Owner      string
	Generation int64
}

// ReleaseSSHReceive releases one held SSH receive owner.
func ReleaseSSHReceive(ctx context.Context, owner string, generation int64) error {
	reqURL := setting.LocalURL + "api/internal/serv/release"
	req := newInternalRequest(ctx, reqURL, "POST", &SSHReleaseOption{Owner: owner, Generation: generation})
	_, extra := requestJSONResp(req, &ResponseText{})
	return extra.Error
}
