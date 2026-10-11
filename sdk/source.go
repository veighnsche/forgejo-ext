// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"context"
	"errors"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxRepositorySourcePaths  = 64
	MaxRepositorySourceFile   = 32 << 10
	MaxRepositorySourceTotal  = 128 << 10
	MaxRepositorySourceBundle = 512 << 10
)

// RepositorySource is an exact, bounded native Git source read. Selected
// actor permissions are observations, never grants or acting identities.
type RepositorySource struct {
	RepositoryID string            `json:"repository_id"`
	Commit       string            `json:"commit"`
	Bundle       []byte            `json:"bundle"`
	Files        map[string][]byte `json:"files"`
	Writers      map[string]bool   `json:"writers,omitempty"`
}

func sourceCommit(commit string) bool {
	if len(commit) != 40 {
		return false
	}
	for _, c := range commit {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func ValidateRepositorySourceRequest(request CallbackRequest) error {
	invalid := errors.New("invalid repository source selection")
	if !decimalID(request.RepositoryID) || request.SourceCommit != "" && !sourceCommit(request.SourceCommit) || len(request.SourcePaths) > MaxRepositorySourcePaths || len(request.SourceActors) > 8 {
		return invalid
	}
	seen := make(map[string]bool)
	for _, name := range request.SourcePaths {
		if name == "" || len(name) > 1024 || !utf8.ValidString(name) || path.IsAbs(name) || path.Clean(name) != name || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00\r\n") || seen[name] {
			return invalid
		}
		for _, part := range strings.Split(name, "/") {
			if part == ".git" || part == ".soda-home" {
				return invalid
			}
		}
		seen[name] = true
	}
	seen = make(map[string]bool)
	for _, actor := range request.SourceActors {
		if !decimalID(actor) || seen[actor] {
			return invalid
		}
		if _, err := strconv.ParseInt(actor, 10, 64); err != nil {
			return invalid
		}
		seen[actor] = true
	}
	return nil
}

func ValidRepositorySource(source *RepositorySource, request CallbackRequest) bool {
	if ValidateRepositorySourceRequest(request) != nil || source == nil || source.RepositoryID != request.RepositoryID || !sourceCommit(source.Commit) || request.SourceCommit != "" && source.Commit != request.SourceCommit || len(source.Bundle) == 0 || len(source.Bundle) > MaxRepositorySourceBundle || len(source.Files) != len(request.SourcePaths) || len(source.Writers) != len(request.SourceActors) {
		return false
	}
	total := 0
	for _, name := range request.SourcePaths {
		contents, found := source.Files[name]
		if !found || len(contents) > MaxRepositorySourceFile {
			return false
		}
		total += len(contents)
	}
	for _, actor := range request.SourceActors {
		if _, found := source.Writers[actor]; !found {
			return false
		}
	}
	return total <= MaxRepositorySourceTotal
}

// RepositorySource reads an approved-branch source through this request's
// current native admission. Empty commit selects the observed default tip.
func (a Authority) RepositorySource(ctx context.Context, id, commit string, paths, actors []string) (RepositorySource, error) {
	request := CallbackRequest{Operation: OperationRepositorySource, RepositoryID: id, SourceCommit: commit, SourcePaths: paths, SourceActors: actors}
	if err := ValidateRepositorySourceRequest(request); err != nil {
		return RepositorySource{}, err
	}
	result, err := a.nativeClient().call(ctx, request)
	if err != nil {
		return RepositorySource{}, err
	}
	return *result.Source, nil
}
