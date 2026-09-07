// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package updatechecker

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"

	"forgejo.org/modules/json"
	"forgejo.org/modules/log"
	"forgejo.org/modules/optional"
	"forgejo.org/modules/proxy"
	"forgejo.org/modules/setting"
	"forgejo.org/modules/system"

	"github.com/hashicorp/go-version"
)

// CheckerState stores the remote version from the JSON endpoint
type CheckerState struct {
	LatestVersion     string // legacy field that may be stored in appstate
	SupportedVersions []string
}

// Name returns the name of the state item for update checker
func (r *CheckerState) Name() string {
	return "update-checker"
}

// GiteaUpdateChecker returns error when new version of Gitea is available
func GiteaUpdateChecker(httpEndpoint, domainEndpoint string) error {
	var version []string
	var err error
	if domainEndpoint != "" {
		version, err = getVersionDNS(domainEndpoint)
	} else {
		v, err := getVersionHTTP(httpEndpoint)
		if err != nil {
			return nil
		}
		version = []string{v}
	}

	if err != nil {
		return err
	}

	return UpdateRemoteVersion(context.Background(), version)
}

var lookupTXT = net.LookupTXT

// getVersionDNS will request the TXT records for the domain. If a record starts
// with "forgejo_versions=" everything after that will be used as the latest
// version available.
func getVersionDNS(domainEndpoint string) (version []string, err error) {
	records, err := lookupTXT(domainEndpoint)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, errors.New("no TXT records were found")
	}

	var supportedVersions []string
	for _, record := range records {
		if after, ok := strings.CutPrefix(record, "forgejo_versions="); ok {
			// Get all supported versions, separated by a comma.
			for v := range strings.SplitSeq(after, ",") {
				supportedVersions = append(supportedVersions, strings.TrimSpace(v))
			}
		}
	}
	if len(supportedVersions) == 0 {
		return nil, errors.New("there is no TXT record with a valid value")
	}

	return supportedVersions, nil
}

// getVersionHTTP will make an HTTP request to the endpoint, and the returned
// content is JSON. The "latest.version" path's value will be used as the latest
// version available.
func getVersionHTTP(httpEndpoint string) (version string, err error) {
	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: proxy.Proxy(),
		},
	}

	req, err := http.NewRequest("GET", httpEndpoint, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	type respType struct {
		Latest struct {
			Version string `json:"version"`
		} `json:"latest"`
	}
	respData := respType{}
	err = json.Unmarshal(body, &respData)
	if err != nil {
		return "", err
	}
	return respData.Latest.Version, nil
}

// UpdateRemoteVersion updates the latest available version of Gitea
func UpdateRemoteVersion(ctx context.Context, versions []string) (err error) {
	return system.AppState.Set(ctx, &CheckerState{SupportedVersions: versions})
}

type MajorReleaseState int

const (
	// Current major release version is included in the list of supported releases.
	MajorReleaseSupported MajorReleaseState = iota
	// Current major release version is not included in the list of supported releases.
	MajorReleaseUnsupported
	// Current major release version exceeds all the supported versions, indicating that it is a pre-release environment
	// (dev environment, experimental deployment, etc.)
	MajorReleasePrerelease
)

type MinorReleaseState int

const (
	// Current minor release version is the current release; only relevant if major release state is MajorReleaseSupported.
	MinorReleaseCurrent MinorReleaseState = iota
	// Current minor release version is not the current release; only relevant if major release state is MajorReleaseSupported.
	MinorReleaseOutOfDate
)

type ReleaseState struct {
	MajorReleaseState  MajorReleaseState
	MinorReleaseState  MinorReleaseState
	RecommendedUpgrade optional.Option[string]
}

// Using the state stored from UpdateRemoteVersion, and the current version of this deployment, calculate a ReleaseState
// structure.  In any error situation, a "no upgrade required" state will be returned and warnings may be logged.
func GetReleaseState(ctx context.Context) *ReleaseState {
	item := new(CheckerState)
	if err := system.AppState.Get(ctx, item); err != nil {
		log.Warn("system appstate unable to retrieve update checker output: %s", err)
		return &ReleaseState{} // zero-value indicates no upgrade necessary
	}
	// Upgrade legacy LatestVersion field, single value, to the new array SupportedVersions
	if item.LatestVersion != "" && item.SupportedVersions == nil {
		item.SupportedVersions = []string{item.LatestVersion}
		item.LatestVersion = ""
	}

	currentVersion, err := version.NewVersion(setting.AppVer)
	if err != nil {
		log.Warn("update checker unable to parse current version; update checker will be disabled: %s", err)
		return &ReleaseState{} // zero-value indicates no upgrade necessary
	}

	supportedVersions := []*version.Version{}
	for _, v := range item.SupportedVersions {
		parsed, err := version.NewVersion(v)
		if err != nil {
			// If a remote version couldn't be parsed, log a warning but otherwise make the best attempt we can to
			// populate a ReleaseState ignoring that release:
			log.Warn("failure to parse remote version %q: %s", v, err)
			continue
		}
		supportedVersions = append(supportedVersions, parsed)
	}

	if len(supportedVersions) == 0 {
		// No versions indicated as current from remote, that we could parse.
		log.Warn("update checker could not identify any supported versions")
		return &ReleaseState{} // zero-value indicates no upgrade necessary
	}

	// Check if we're greater than all currently supported versions, indicating that we're running a pre-release build:
	if preRelease := tryCheckPrerelease(currentVersion, supportedVersions); preRelease != nil {
		return preRelease
	}

	// Check if the major version we're currently running is supported, and if so, what the latest release of it is:
	if minorReleaseTarget := tryGetMatchingMajorRelease(currentVersion, supportedVersions); minorReleaseTarget != nil {
		return minorReleaseTarget
	}

	// Not currently on a support major release.  Recommend upgrading to the LTS, following the logic that if this
	// release has fallen behind this much, the admins probably don't want to be on the bleeding edge which requires
	// more frequent, riskier upgrades.
	if ltsTarget := tryGetHighestLTSRelease(supportedVersions); ltsTarget != nil {
		return ltsTarget
	}

	// Couldn't find a supported LTS.  Choose the higest supported version.  Must be non-nil because an empty
	// supportedVersions already exited earlier.
	return getHighestRelease(supportedVersions)
}

func tryCheckPrerelease(currentVersion *version.Version, supportedVersions []*version.Version) *ReleaseState {
	greaterThanAll := true
	for _, s := range supportedVersions {
		if !currentVersion.GreaterThan(s) {
			greaterThanAll = false
			break
		}
	}
	if !greaterThanAll {
		return nil
	}
	return &ReleaseState{
		MajorReleaseState: MajorReleasePrerelease,
	}
}

func tryGetMatchingMajorRelease(currentVersion *version.Version, supportedVersions []*version.Version) *ReleaseState {
	currentSeg := currentVersion.Segments()
	var matchingMajorVersion *version.Version
	for _, s := range supportedVersions {
		remoteSeg := s.Segments()
		if len(currentSeg) > 0 && len(remoteSeg) > 0 && s.Segments()[0] == currentVersion.Segments()[0] {
			if matchingMajorVersion == nil || s.GreaterThan(matchingMajorVersion) {
				// Same major release, highest version found...
				matchingMajorVersion = s
			}
		}
	}
	if matchingMajorVersion == nil {
		return nil
	}
	state := &ReleaseState{
		MajorReleaseState: MajorReleaseSupported,
	}
	if matchingMajorVersion.GreaterThan(currentVersion) {
		state.MinorReleaseState = MinorReleaseOutOfDate
		state.RecommendedUpgrade = optional.Some(matchingMajorVersion.Original())
	} else {
		state.MinorReleaseState = MinorReleaseCurrent
	}
	return state
}

func tryGetHighestLTSRelease(supportedVersions []*version.Version) *ReleaseState {
	var targetLtsVersion *version.Version
	for _, s := range supportedVersions {
		seg := s.Segments()
		// Forgejo's LTS releases are 11, 15, 19, ... n%4 == 3.  Hard-coding this is a bit odd, but if the strategy
		// changes we can backport to the supported releases, and anyone very out-of-date wouldn't be terribly served by
		// this logic.  It probably works poorly for any distributed (non-SaaS) forks, but they can change it.
		if len(seg) > 0 && (seg[0]%4) == 3 {
			if targetLtsVersion == nil || s.GreaterThan(targetLtsVersion) {
				targetLtsVersion = s
			}
		}
	}
	if targetLtsVersion == nil {
		return nil
	}
	return &ReleaseState{
		MajorReleaseState:  MajorReleaseUnsupported,
		MinorReleaseState:  MinorReleaseOutOfDate,
		RecommendedUpgrade: optional.Some(targetLtsVersion.Original()),
	}
}

func getHighestRelease(supportedVersions []*version.Version) *ReleaseState {
	var highestVersion *version.Version
	for _, s := range supportedVersions {
		if highestVersion == nil || s.GreaterThan(highestVersion) {
			highestVersion = s
		}
	}
	return &ReleaseState{
		MajorReleaseState:  MajorReleaseUnsupported,
		MinorReleaseState:  MinorReleaseOutOfDate,
		RecommendedUpgrade: optional.Some(highestVersion.Original()),
	}
}
