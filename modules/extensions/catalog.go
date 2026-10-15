// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package extensions

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	sdk "forgejo.org/extension-sdk"
)

const (
	// acquireTimeout bounds one catalog download end to end.
	acquireTimeout = 5 * time.Minute
	// acquireConnectTimeout bounds TCP connect and TLS handshake.
	acquireConnectTimeout = 15 * time.Second
	// acquireHeaderTimeout bounds waiting for the response header.
	acquireHeaderTimeout = 30 * time.Second
	// maximumRedirects caps followed redirects per acquisition.
	maximumRedirects = 5
)

// Bounded acquisition failures carry the package id only: URLs, signed
// query strings, archive bytes, filesystem paths and arbitrary peer
// error bodies never reach the UI.
var (
	errAcquireDownload = errors.New("package download failed")
	errAcquireSize     = errors.New("package size mismatch")
	errAcquireDigest   = errors.New("package checksum mismatch")
	errAcquireRedirect = errors.New("package redirect rejected")
)

// Acquire downloads one declared catalog package, verifies its exact size
// and SHA-256 into the shared cache, then extracts the staged directory
// for installation. The response streams into a task-owned mode-0600 file
// while hashing; archives are never buffered in RAM. It performs no
// package change itself: pass the result to the package store, which swaps
// under the lock. The caller removes the staging directory.
func Acquire(ctx context.Context, root string, entry sdk.CatalogPackage) (string, error) {
	return AcquireWithClient(ctx, root, entry, nil)
}

// AcquireWithClient is Acquire with an injected HTTP client for fixtures.
// A nil client uses the bounded default client.
func AcquireWithClient(ctx context.Context, root string, entry sdk.CatalogPackage, client *http.Client) (string, error) {
	if err := sdk.ValidateCatalogPackage(entry); err != nil {
		return "", err
	}
	if client == nil {
		client = newAcquireClient()
	}
	cached, err := downloadToCache(ctx, root, entry, client)
	if err != nil {
		return "", err
	}
	stage, err := stageFromCache(root, entry, cached)
	if err != nil {
		_ = os.RemoveAll(stage)
		return "", err
	}
	return stage, nil
}

// newAcquireClient builds the bounded download client: a five-minute total
// envelope comes from the caller's context, while connect/TLS and
// response-header legs have their own bounds. Redirects stay HTTPS without
// user info or fragments; signed CDN query strings are allowed on
// redirects only. No credentials or cookies are ever attached.
func newAcquireClient() *http.Client {
	return &http.Client{
		CheckRedirect: checkAcquireRedirect,
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout: acquireConnectTimeout,
			}).DialContext,
			TLSHandshakeTimeout:   acquireConnectTimeout,
			ResponseHeaderTimeout: acquireHeaderTimeout,
		},
	}
}

// checkAcquireRedirect enforces the redirect policy: at most five
// redirects, every one remaining HTTPS with no user info or fragment.
func checkAcquireRedirect(request *http.Request, via []*http.Request) error {
	if len(via) > maximumRedirects {
		return errAcquireRedirect
	}
	if request.URL.Scheme != "https" || request.URL.Host == "" {
		return errAcquireRedirect
	}
	if request.URL.User != nil || request.URL.Fragment != "" {
		return errAcquireRedirect
	}
	return nil
}

// cachePath returns the verified-archive location for one digest.
func cachePath(root, sha string) string {
	return filepath.Join(root, ".cache", "packages", sha+".tar.gz")
}

// downloadToCache streams one catalog entry into the shared SHA-verified
// cache, enforcing the exact declared byte count with or without a
// Content-Length. An intact cached entry is reused after rechecking its
// size and digest; a corrupt entry is removed so it can be reacquired
// without touching other entries.
func downloadToCache(ctx context.Context, root string, entry sdk.CatalogPackage, client *http.Client) (string, error) {
	cached := cachePath(root, entry.SHA256)
	if info, err := os.Stat(cached); err == nil {
		if info.Mode().IsRegular() && info.Size() == entry.Size {
			if ok, _ := verifyFileDigest(cached, entry.SHA256); ok {
				return cached, nil
			}
		}
		_ = os.Remove(cached)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	ctx, cancel := context.WithTimeout(ctx, acquireTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL, nil)
	if err != nil {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	response, err := client.Do(request)
	if err != nil {
		if errors.Is(err, errAcquireRedirect) {
			return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireRedirect)
		}
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	directory := filepath.Join(root, ".cache", "packages")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	temporary, err := os.CreateTemp(directory, ".download-")
	if err != nil {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	temporaryName := temporary.Name()
	failed := true
	defer func() {
		if failed {
			_ = temporary.Close()
			_ = os.Remove(temporaryName)
		}
	}()
	hash := sha256.New()
	var written int64
	buffer := make([]byte, 128<<10)
	for {
		remaining := entry.Size + 1 - written
		n, readErr := response.Body.Read(buffer[:min(int64(len(buffer)), remaining)])
		if n > 0 {
			written += int64(n)
			hash.Write(buffer[:n])
			if _, writeErr := temporary.Write(buffer[:n]); writeErr != nil {
				return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
		}
		if written > entry.Size {
			break
		}
	}
	if written != entry.Size {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireSize)
	}
	if hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDigest)
	}
	if err := temporary.Sync(); err != nil {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	if err := os.Chmod(temporaryName, 0o600); err != nil {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	if err := os.Rename(temporaryName, cached); err != nil {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	failed = false
	if err := fsyncDirectory(directory); err != nil {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	return cached, nil
}

// verifyFileDigest rechecks an existing file's size and SHA-256.
func verifyFileDigest(path, sha string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return false, err
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, err
	}
	return hex.EncodeToString(hash.Sum(nil)) == sha, nil
}

// stageFromCache extracts and validates one verified cached archive into
// a private staging directory and generates its receipt. A corrupt exact
// cache entry is removed so it can be reacquired.
func stageFromCache(root string, entry sdk.CatalogPackage, cached string) (string, error) {
	info, err := os.Stat(cached)
	if err != nil || !info.Mode().IsRegular() || info.Size() != entry.Size {
		_ = os.Remove(cached)
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	if ok, _ := verifyFileDigest(cached, entry.SHA256); !ok {
		_ = os.Remove(cached)
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDigest)
	}
	stage, err := os.MkdirTemp(root, ".acquire-")
	if err != nil {
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, errAcquireDownload)
	}
	if err := extractArchive(cached, stage); err != nil {
		_ = os.RemoveAll(stage)
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, err)
	}
	manifest, err := sdk.LoadManifest(stage)
	if err != nil {
		_ = os.RemoveAll(stage)
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, err)
	}
	if err := MatchEntryManifest(entry, manifest); err != nil {
		_ = os.RemoveAll(stage)
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, err)
	}
	if err := checkPackageFiles(stage, manifest); err != nil {
		_ = os.RemoveAll(stage)
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, err)
	}
	if err := WriteReceipt(stage, entry); err != nil {
		_ = os.RemoveAll(stage)
		return "", fmt.Errorf("acquire package %q: %w", entry.ID, err)
	}
	return stage, nil
}

// MatchEntryManifest checks catalog identity against an extracted
// manifest: ID, version, protocol, capability set and lifecycle
// controller must agree exactly. Browser contract is checked by the
// SDK and host, not here.
func MatchEntryManifest(entry sdk.CatalogPackage, manifest sdk.Manifest) error {
	if manifest.ID != entry.ID || manifest.Version != entry.Version {
		return errors.New("archive identity does not match catalog")
	}
	if manifest.Protocol != entry.FountainProto {
		return errors.New("archive protocol does not match catalog")
	}
	want := append([]string(nil), entry.Capabilities...)
	got := append([]string(nil), manifest.Capabilities...)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(want, got) {
		return errors.New("archive capabilities do not match catalog")
	}
	if manifest.LifecycleController != entry.LifecycleController {
		return errors.New("archive lifecycle controller does not match catalog")
	}
	return nil
}

// extractArchive validates one gzip tar archive exactly and extracts its
// regular files into target: USTAR regular-file and directory entries
// only, no dot-prefixed top-level entries, no receipt, no absolute
// names, backslashes, dot components, duplicates, links, devices, FIFOs,
// sparse files, PAX/GNU extensions, over-long paths, or entries below a
// regular file. Limits are 512 MiB of file data, 10,000 entries including
// directories and 255 bytes per path. Exactly one gzip member, a valid
// checksum and trailer, a complete tar terminator and no nonzero trailing
// decoded data are required. Regular files land through os.OpenRoot so
// writes stay inside staging.
func extractArchive(archive, target string) error {
	file, err := os.Open(archive)
	if err != nil {
		return errAcquireDownload
	}
	defer file.Close()
	unzip, err := gzip.NewReader(file)
	if err != nil {
		return errors.New("invalid package archive")
	}
	defer unzip.Close()
	unzip.Multistream(false)
	stage, err := os.OpenRoot(target)
	if err != nil {
		return err
	}
	defer stage.Close()
	reader := tar.NewReader(unzip)
	var total int64
	var decoded int64
	entries := 0
	seen := make(map[string]bool)
	isFile := make(map[string]bool)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.New("invalid package archive")
		}
		if header.Format != tar.FormatUSTAR {
			return fmt.Errorf("package archive uses an unsupported format: %s", header.Name)
		}
		name := header.Name
		isDir := header.Typeflag == tar.TypeDir
		if isDir {
			name = strings.TrimSuffix(name, "/")
		}
		if !validArchiveName(name) {
			return fmt.Errorf("package archive escapes staging: %s", header.Name)
		}
		if len(name) > 255 {
			return fmt.Errorf("package archive path is too long: %s", header.Name)
		}
		top, _, _ := strings.Cut(name, "/")
		if strings.HasPrefix(top, ".") {
			return fmt.Errorf("package archive hides reserved state: %s", header.Name)
		}
		if slices.Contains(strings.Split(name, "/"), receiptFileName) {
			return fmt.Errorf("package archive supplies reserved state: %s", header.Name)
		}
		if seen[name] {
			return fmt.Errorf("package archive repeats a path: %s", header.Name)
		}
		seen[name] = true
		for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
			if isFile[parent] {
				return fmt.Errorf("package archive nests below a file: %s", header.Name)
			}
		}
		entries++
		if entries > 10000 {
			return errors.New("package exceeds 10000 files or 512 MiB")
		}
		decoded += 512
		switch header.Typeflag {
		case tar.TypeDir:
			if isFile[name] {
				return fmt.Errorf("package archive repeats a path: %s", header.Name)
			}
			if err := mkdirAll(stage, name); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size < 0 || header.Size > sdk.MaximumArchiveBytes {
				return errors.New("package exceeds 10000 files or 512 MiB")
			}
			total += header.Size
			if total > sdk.MaximumArchiveBytes {
				return errors.New("package exceeds 10000 files or 512 MiB")
			}
			decoded += (header.Size + 511) / 512 * 512
			isFile[name] = true
			if dir := filepath.Dir(name); dir != "." {
				if err := mkdirAll(stage, dir); err != nil {
					return err
				}
			}
			mode := os.FileMode(0o644)
			if header.FileInfo().Mode()&0o111 != 0 {
				mode = 0o755
			}
			out, err := stage.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			copied, copyErr := io.Copy(out, io.LimitReader(reader, header.Size+1))
			closeErr := out.Close()
			if copyErr == nil && copied != header.Size {
				copyErr = errors.New("package archive changed while extracting")
			}
			if copyErr != nil || closeErr != nil {
				return errors.Join(copyErr, closeErr)
			}
		default:
			return fmt.Errorf("package archive entry is not a file: %s", header.Name)
		}
	}
	// The tar terminator is two zero blocks; only zero padding may follow
	// inside the member, capped well above any producer's block padding.
	// A second pass over the whole stream then proves at most one gzip
	// member with a valid checksum/trailer exists: every decoded byte must
	// be accounted for by the validated headers, terminator and padding.
	// (Reader buffering makes a compressed-side offset check unreliable.)
	decoded += 1024
	rest, err := io.ReadAll(io.LimitReader(unzip, (1<<20)+1))
	if err != nil {
		return errors.New("invalid package archive")
	}
	if len(rest) > 1<<20 || bytes.Count(rest, []byte{0}) != len(rest) {
		return errors.New("package archive has trailing data")
	}
	decoded += int64(len(rest))
	return verifySingleMember(archive, decoded)
}

// verifySingleMember decodes the whole archive stream and requires the
// decoded length to equal the validated structure exactly. A second gzip
// member or trailing garbage changes the total or fails the decode.
func verifySingleMember(archive string, decoded int64) error {
	file, err := os.Open(archive)
	if err != nil {
		return errAcquireDownload
	}
	defer file.Close()
	unzip, err := gzip.NewReader(file)
	if err != nil {
		return errors.New("invalid package archive")
	}
	defer unzip.Close()
	streamed, err := io.Copy(io.Discard, unzip)
	if err != nil {
		return errors.New("package archive has trailing data")
	}
	if err := unzip.Close(); err != nil {
		return errors.New("package archive has trailing data")
	}
	if streamed != decoded {
		return errors.New("package archive has trailing data")
	}
	return nil
}

// mkdirAll creates one validated relative directory path inside root.
func mkdirAll(root *os.Root, name string) error {
	if name == "." {
		return nil
	}
	current := "."
	for segment := range strings.SplitSeq(name, "/") {
		current = current + "/" + segment
		rel := strings.TrimPrefix(current, "./")
		if err := root.Mkdir(rel, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	return nil
}

func validArchiveName(name string) bool {
	if name == "" || name == "." || filepath.IsAbs(name) || strings.Contains(name, `\`) {
		return false
	}
	if name != filepath.Clean(name) || name == ".." || strings.HasPrefix(name, "../") {
		return false
	}
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// fsyncDirectory persists one directory's entries.
func fsyncDirectory(directory string) error {
	handle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer handle.Close()
	return handle.Sync()
}

// LoadCatalogFile loads the release-supplied trusted catalog: an absolute
// configured path to one read-only JSON file, or empty for no available
// catalog packages (permitted only when no catalog-driven required
// bootstrap is configured; callers enforce that). A configured
// missing or malformed file is an explicit catalog-unavailable error,
// never an empty catalog. The file must be regular, opened without
// following its final symlink, at most 1 MiB, and not group or other
// writable. It returns the catalog and the SHA-256 of its exact bytes.
func LoadCatalogFile(path string) (sdk.Catalog, string, error) {
	if path == "" {
		return sdk.Catalog{Version: sdk.CatalogVersion}, "", nil
	}
	if !filepath.IsAbs(path) {
		return sdk.Catalog{}, "", errors.New("extension catalog path must be absolute")
	}
	// The final component must not be a symlink: lstat first, open,
	// then confirm the opened file is the same regular file.
	linfo, err := os.Lstat(path)
	if err != nil || !linfo.Mode().IsRegular() {
		return sdk.Catalog{}, "", errors.New("extension catalog is unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return sdk.Catalog{}, "", errors.New("extension catalog is unavailable")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(linfo, info) {
		return sdk.Catalog{}, "", errors.New("extension catalog is unavailable")
	}
	// The catalog is release-supplied and read-only: root or the service
	// UID owns it, and no group or other write bit is set.
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 && stat.Uid != uint32(os.Geteuid()) {
		return sdk.Catalog{}, "", errors.New("extension catalog is unavailable")
	}
	if info.Mode().Perm()&0o022 != 0 {
		return sdk.Catalog{}, "", errors.New("extension catalog is unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return sdk.Catalog{}, "", errors.New("extension catalog is unavailable")
	}
	sum := sha256.Sum256(data)
	catalog, err := sdk.DecodeCatalog(data)
	if err != nil {
		return sdk.Catalog{}, "", errors.New("extension catalog is unavailable")
	}
	if err := sdk.ValidateCatalog(catalog); err != nil {
		return sdk.Catalog{}, "", errors.New("extension catalog is unavailable")
	}
	return catalog, hex.EncodeToString(sum[:]), nil
}

// ValidateCatalogSelection enforces cross-entry release rules against the
// host configuration: the required-marked set must equal REQUIRED_IDS
// exactly, and every lifecycle controller must name a required package
// that is not itself controlled, preventing cycles without a solver.
func ValidateCatalogSelection(catalog sdk.Catalog, requiredIDs []string) error {
	required := make(map[string]bool, len(requiredIDs))
	for _, id := range requiredIDs {
		if !sdk.ValidID(id) || required[id] {
			return errors.New("invalid required extension configuration")
		}
		required[id] = true
	}
	marked := make(map[string]bool, len(catalog.Packages))
	controlled := make(map[string]bool, len(catalog.Packages))
	for _, entry := range catalog.Packages {
		if entry.Required {
			marked[entry.ID] = true
		}
		if entry.LifecycleController != "" {
			controlled[entry.ID] = true
		}
	}
	if len(marked) != len(required) {
		return errors.New("catalog required set does not match configuration")
	}
	for id := range marked {
		if !required[id] {
			return errors.New("catalog required set does not match configuration")
		}
	}
	byID := make(map[string]sdk.CatalogPackage, len(catalog.Packages))
	for _, entry := range catalog.Packages {
		byID[entry.ID] = entry
	}
	for _, entry := range catalog.Packages {
		if entry.LifecycleController == "" {
			continue
		}
		controller, ok := byID[entry.LifecycleController]
		if !ok || !controller.Required {
			return fmt.Errorf("catalog package %q names an unknown controller", entry.ID)
		}
		if controlled[controller.ID] {
			return fmt.Errorf("catalog package %q is controlled by another controlled package", entry.ID)
		}
	}
	return nil
}
