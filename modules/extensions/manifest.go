// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
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

type Manifest struct {
	Protocol   int     `json:"protocol"`
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Version    string  `json:"version"`
	Executable string  `json:"executable"`
	Pages      []Page  `json:"pages,omitempty"`
	Panels     []Panel `json:"panels,omitempty"`
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
	var manifest Manifest
	dec := json.NewDecoder(io.LimitReader(f, 1<<20))
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

func safeRelative(name string) bool {
	return name != "" && !filepath.IsAbs(name) && !strings.Contains(name, `\`) &&
		path.Clean(name) == name && name != "." && name != ".." && !strings.HasPrefix(name, "../")
}

func assetEntry(name string) bool {
	return safeRelative(name) && strings.HasSuffix(name, ".js")
}
