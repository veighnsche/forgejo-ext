// Copyright 2021 The Gitea Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package setting

import (
	"net"
	"net/url"
	"strings"

	"forgejo.org/modules/hostmatcher"
	"forgejo.org/modules/log"

	"github.com/42wim/httpsig"
)

// Federation settings
var (
	Federation = struct {
		Enabled                   bool
		ShareUserStatistics       bool
		MaxSize                   int64
		SignatureAlgorithms       []string
		DigestAlgorithm           string
		GetHeaders                []string
		PostHeaders               []string
		InsecureAllowInvalidHosts bool
		AllowedHosts              []string
		BlockedHosts              []string
	}{
		Enabled:                   false,
		ShareUserStatistics:       true,
		MaxSize:                   4,
		SignatureAlgorithms:       []string{"rsa-sha256", "rsa-sha512", "ed25519"},
		DigestAlgorithm:           "SHA-256",
		GetHeaders:                []string{"(request-target)", "Date", "Host"},
		PostHeaders:               []string{"(request-target)", "Date", "Host", "Digest"},
		InsecureAllowInvalidHosts: false,
		AllowedHosts:              nil,
		BlockedHosts:              nil,
	}

	// FederationAllowedHostList is the compiled allowlist of hosts this
	// instance federates with. Empty (the default) means no external host is
	// allowed: federation is opt-in and the administrator must list the hosts
	// explicitly. It accepts hostmatcher syntax, e.g. "example.com,
	// *.example.com, external, private, loopback, 10.0.0.0/8" or "*" to allow
	// every host.
	FederationAllowedHostList *hostmatcher.HostMatchList

	// FederationBlockedHostList is the compiled denylist of hosts that must
	// never be contacted for federation, regardless of the allowlist.
	FederationBlockedHostList *hostmatcher.HostMatchList
)

// HttpsigAlgs is a constant slice of httpsig algorithm objects
var HttpsigAlgs []httpsig.Algorithm

func loadFederationFrom(rootCfg ConfigProvider) {
	if err := rootCfg.Section("federation").MapTo(&Federation); err != nil {
		log.Fatal("Failed to map Federation settings: %v", err)
	} else if !httpsig.IsSupportedDigestAlgorithm(Federation.DigestAlgorithm) {
		log.Fatal("unsupported digest algorithm: %s", Federation.DigestAlgorithm)
		return
	}

	// Get MaxSize in bytes instead of MiB
	Federation.MaxSize = 1 << 20 * Federation.MaxSize

	HttpsigAlgs = make([]httpsig.Algorithm, len(Federation.SignatureAlgorithms))
	for i, alg := range Federation.SignatureAlgorithms {
		HttpsigAlgs[i] = httpsig.Algorithm(alg)
	}

	FederationAllowedHostList = hostmatcher.ParseHostMatchList("federation.ALLOWED_HOSTS", strings.Join(Federation.AllowedHosts, ","))
	FederationBlockedHostList = hostmatcher.ParseHostMatchList("federation.BLOCKED_HOSTS", strings.Join(Federation.BlockedHosts, ","))
}

// FederationHostAllowed reports whether the given host (as "hostname" or
// "hostname:port", e.g. taken from a URL) is allowed to be contacted for
// federation, either inbound (HTTP-signature-verified requests) or outbound
// (fetching remote actors and keys, delivering activities).
//
// The instance's own host, as configured by ROOT_URL, is always allowed. A
// host matching the blocklist is always denied. Otherwise the host must match
// the allowlist: an empty allowlist denies every external host.
func FederationHostAllowed(host string) bool {
	if FederationBlockedHostList.MatchHostName(host) {
		return false
	}
	if Federation.InsecureAllowInvalidHosts {
		return true
	}
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	if appURL, err := url.Parse(AppURL); err == nil {
		if strings.EqualFold(appURL.Hostname(), hostname) {
			return true
		}
	}
	if FederationAllowedHostList.IsEmpty() {
		return false
	}
	return FederationAllowedHostList.MatchHostName(host)
}
