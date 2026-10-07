// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

const Protocol = 1

var slug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func ValidID(id string) bool { return slug.MatchString(id) }

type Manifest struct {
	Protocol           int      `json:"protocol"`
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Version            string   `json:"version"`
	Executable         string   `json:"executable"`
	Pages              []Page   `json:"pages,omitempty"`
	Panels             []Panel  `json:"panels,omitempty"`
	Capabilities       []string `json:"capabilities,omitempty"`
	Policies           []string `json:"policies,omitempty"`
	PreferredWorkspace bool     `json:"preferred_workspace,omitempty"`
}

type Page struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Scope      string `json:"scope"`
	Entry      string `json:"entry"`
	Permission string `json:"permission,omitempty"`
}

type Panel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Entry string `json:"entry"`
}

// LoadManifest reads and validates an extension package without executing it.
func LoadManifest(packageDir string) (Manifest, error) {
	f, err := os.Open(filepath.Join(packageDir, "extension.json"))
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	const maximumManifestBytes = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maximumManifestBytes+1))
	if err != nil {
		return Manifest{}, fmt.Errorf("read extension manifest: %w", err)
	}
	if len(data) > maximumManifestBytes {
		return Manifest{}, errors.New("extension manifest exceeds size limit")
	}
	var manifest Manifest
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode extension manifest: %w", err)
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return Manifest{}, errors.New("extension manifest must contain one JSON object")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate() error {
	if m.Protocol != Protocol {
		return fmt.Errorf("unsupported extension protocol %d", m.Protocol)
	}
	if !slug.MatchString(m.ID) || strings.TrimSpace(m.Name) == "" || strings.TrimSpace(m.Version) == "" {
		return errors.New("extension id, name, or version is invalid")
	}
	if !safeRelative(m.Executable) {
		return errors.New("extension executable path is invalid")
	}
	if err := validateNames(m.Capabilities, map[string]bool{
		CapabilityActorRead: true, CapabilityRepositoryRead: true,
		CapabilityOwnedRepositoriesSearch: true, CapabilityOrganizationOwnership: true,
		CapabilityPublicKeysRead: true, CapabilityContributionAuthorize: true,
		CapabilityServiceBridge: true, CapabilityBackgroundOperations: true,
	}, "capability"); err != nil {
		return err
	}
	if err := validateNames(m.Policies, map[string]bool{PolicyForgejoUsername: true}, "policy"); err != nil {
		return err
	}
	seenPages := make(map[string]bool)
	for _, p := range m.Pages {
		if !slug.MatchString(p.ID) || seenPages[p.ID] || strings.TrimSpace(p.Title) == "" || !assetEntry(p.Entry) {
			return fmt.Errorf("invalid or duplicate page %q", p.ID)
		}
		seenPages[p.ID] = true
		switch p.Scope {
		case "global":
			if p.Permission != "" {
				return fmt.Errorf("global page %q must not declare a permission", p.ID)
			}
		case "user":
			if p.Permission != "user" {
				return fmt.Errorf("user page %q requires user permission", p.ID)
			}
		case "admin":
			if p.Permission != "admin" {
				return fmt.Errorf("admin page %q requires admin permission", p.ID)
			}
		case "repository":
			if p.Permission != "read" && p.Permission != "write" && p.Permission != "admin" {
				return fmt.Errorf("repository page %q has invalid permission", p.ID)
			}
		default:
			return fmt.Errorf("page %q has invalid scope", p.ID)
		}
	}
	seenPanels := make(map[string]bool)
	for _, p := range m.Panels {
		if !slug.MatchString(p.ID) || seenPanels[p.ID] || strings.TrimSpace(p.Title) == "" || !assetEntry(p.Entry) {
			return fmt.Errorf("invalid or duplicate panel %q", p.ID)
		}
		seenPanels[p.ID] = true
	}
	return nil
}

func validateNames(names []string, allowed map[string]bool, kind string) error {
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if !allowed[name] || seen[name] {
			return fmt.Errorf("invalid or duplicate %s %q", kind, name)
		}
		seen[name] = true
	}
	return nil
}

func safeRelative(name string) bool {
	return name != "" && !filepath.IsAbs(name) && !strings.Contains(name, `\`) &&
		path.Clean(name) == name && name != "." && name != ".." && !strings.HasPrefix(name, "../")
}

func assetEntry(name string) bool {
	return safeRelative(name) && strings.HasSuffix(name, ".js")
}
