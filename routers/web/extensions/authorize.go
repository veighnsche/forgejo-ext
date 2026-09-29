// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	extension "forgejo.org/extension-sdk"
	runtime "forgejo.org/services/extensions"
)

const (
	contributionAuthorizationPath = "/v1/contribution/authorize"
	maxContributionResponseBytes  = 64 << 10
	contributionAuthorizationTTL  = 2 * time.Second
)

func authorizesContribution(ctx context.Context, descriptor runtime.Descriptor, transport http.RoundTripper, contribution extension.Contribution, repositoryID string, actorID int64) bool {
	if !hasCapability(descriptor, extension.CapabilityContributionAuthorize) {
		return true
	}
	if ctx == nil || transport == nil || actorID < 1 || contribution.ID == "" || contribution.Kind == "" || contribution.Scope == "" || contribution.Action == "" {
		return false
	}
	data, err := json.Marshal(extension.ContributionRequest{
		ActorID:      strconv.FormatInt(actorID, 10),
		Contribution: contribution,
		RepositoryID: repositoryID,
	})
	if err != nil {
		return false
	}
	callCtx, cancel := context.WithTimeout(ctx, contributionAuthorizationTTL)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodPost, "http://extension"+contributionAuthorizationPath, bytes.NewReader(data))
	if err != nil {
		return false
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := transport.RoundTrip(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || err != nil || mediaType != "application/json" {
		return false
	}
	data, err = io.ReadAll(io.LimitReader(response.Body, maxContributionResponseBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxContributionResponseBytes {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decision extension.ContributionDecision
	if decoder.Decode(&decision) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		return false
	}
	return decision.Allowed
}

func hasCapability(descriptor runtime.Descriptor, capability string) bool {
	for _, declared := range descriptor.Manifest.Capabilities {
		if declared == capability {
			return true
		}
	}
	return false
}

func pageContribution(page extension.Page, action string) extension.Contribution {
	return extension.Contribution{ID: page.ID, Kind: "page", Scope: page.Scope, Action: strings.ToLower(action)}
}
