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
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	sdk "forgejo.org/extension-sdk"

	"github.com/stretchr/testify/require"
)

// tarGzip builds one gzip member holding the caller's tar entries.
func tarGzip(t *testing.T, write func(writer *tar.Writer)) []byte {
	t.Helper()
	var archive bytes.Buffer
	unzip := gzip.NewWriter(&archive)
	writer := tar.NewWriter(unzip)
	write(writer)
	require.NoError(t, writer.Close())
	require.NoError(t, unzip.Close())
	return archive.Bytes()
}

func validFixtureFiles(t *testing.T) map[string][]byte {
	t.Helper()
	manifest := sdk.Manifest{Protocol: sdk.Protocol, ID: "tailnet", Name: "Tailnet", Version: "1.2.3", Executable: "backend", Pages: []sdk.Page{{ID: "home", Title: "Home", Scope: "global", Entry: "main.js"}}}
	encoded, err := json.Marshal(manifest)
	require.NoError(t, err)
	return map[string][]byte{
		"extension.json": encoded,
		"backend":        []byte("prebuilt executable fixture"),
		"assets/main.js": []byte("export function mount() {}"),
	}
}

func writeFixtureTar(t *testing.T, writer *tar.Writer, files map[string][]byte) {
	t.Helper()
	for name, body := range files {
		mode := int64(0o644)
		if name == "backend" {
			mode = 0o755
		}
		isDir := strings.HasSuffix(name, "/")
		header := &tar.Header{Name: name, Mode: mode, Typeflag: tar.TypeReg}
		if isDir {
			header = &tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir}
		} else {
			header.Size = int64(len(body))
		}
		require.NoError(t, writer.WriteHeader(header))
		if !isDir {
			_, err := writer.Write(body)
			require.NoError(t, err)
		}
	}
}

func redirectClient(t *testing.T, server *httptest.Server) *http.Client {
	t.Helper()
	client := server.Client()
	client.CheckRedirect = checkAcquireRedirect
	return client
}

func TestAcquireRejectsTooManyRedirects(t *testing.T) {
	body := catalogArchive(t, nil)
	sum := sha256.Sum256(body)
	entry := sdk.CatalogPackage{
		ID: "tailnet", Version: "1.2.3", Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:]),
		FountainProto: sdk.Protocol, BrowserContract: sdk.BrowserContractVersion,
	}
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			_, _ = w.Write(body)
			return
		}
		var hops int
		_, _ = fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/r"), "%d", &hops)
		if hops >= 6 {
			_, _ = w.Write(body)
			return
		}
		http.Redirect(w, r, server.URL+"/r"+strconv.Itoa(hops+1), http.StatusFound)
	}))
	t.Cleanup(server.Close)
	entry.URL = server.URL + "/r0"
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, redirectClient(t, server))
	require.ErrorIs(t, err, errAcquireRedirect)
}

func TestAcquireAllowsFiveRedirects(t *testing.T) {
	body := catalogArchive(t, nil)
	server, entry := catalogServer(t, body, http.StatusOK)
	var redirector *httptest.Server
	redirector = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/final" {
			http.Redirect(w, r, server.URL+"/tailnet.tar.gz", http.StatusFound)
			return
		}
		var hops int
		_, _ = fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/r"), "%d", &hops)
		if hops >= 3 {
			http.Redirect(w, r, redirector.URL+"/final", http.StatusFound)
			return
		}
		http.Redirect(w, r, redirector.URL+"/r"+strconv.Itoa(hops+1), http.StatusFound)
	}))
	t.Cleanup(redirector.Close)
	entry.URL = redirector.URL + "/r0"
	stage, err := AcquireWithClient(context.Background(), t.TempDir(), entry, redirectClient(t, redirector))
	require.NoError(t, err)
	_ = os.RemoveAll(stage)
}

func TestAcquireRejectsUnsafeRedirects(t *testing.T) {
	body := catalogArchive(t, nil)
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(plain.Close)
	server, entry := catalogServer(t, body, http.StatusOK)
	mux := http.NewServeMux()
	mux.HandleFunc("/to-http", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/tailnet.tar.gz", http.StatusFound)
	})
	mux.HandleFunc("/to-fragment", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/tailnet.tar.gz#section", http.StatusFound)
	})
	mux.HandleFunc("/to-query", func(w http.ResponseWriter, r *http.Request) {
		// Signed CDN query strings are allowed on redirects.
		http.Redirect(w, r, server.URL+"/tailnet.tar.gz?sig=abc", http.StatusFound)
	})
	redirector := httptest.NewTLSServer(mux)
	t.Cleanup(redirector.Close)

	entry.URL = redirector.URL + "/to-http"
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, redirectClient(t, redirector))
	require.ErrorIs(t, err, errAcquireRedirect)

	entry.URL = redirector.URL + "/to-fragment"
	_, err = AcquireWithClient(context.Background(), t.TempDir(), entry, redirectClient(t, redirector))
	require.ErrorIs(t, err, errAcquireRedirect)

	entry.URL = redirector.URL + "/to-query"
	stage, err := AcquireWithClient(context.Background(), t.TempDir(), entry, redirectClient(t, redirector))
	require.NoError(t, err)
	_ = os.RemoveAll(stage)
}

func TestAcquireRejectsShortAndOversize(t *testing.T) {
	body := catalogArchive(t, nil)
	server, entry := catalogServer(t, body, http.StatusOK)

	oversize := entry
	oversize.Size = int64(len(body)) - 1
	_, err := AcquireWithClient(context.Background(), t.TempDir(), oversize, testClient(t, server))
	require.ErrorIs(t, err, errAcquireSize)

	short := entry
	short.Size = int64(len(body)) + 5
	_, err = AcquireWithClient(context.Background(), t.TempDir(), short, testClient(t, server))
	require.ErrorIs(t, err, errAcquireSize)
}

func TestAcquireRejectsTrailingData(t *testing.T) {
	body := catalogArchive(t, nil)
	for name, extra := range map[string][]byte{
		"zero padding":  make([]byte, 16),
		"garbage":       []byte("trailing garbage"),
		"second member": body,
	} {
		trailing := append(append([]byte{}, body...), extra...)
		server, entry := catalogServer(t, trailing, http.StatusOK)
		_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
		require.ErrorContains(t, err, "trailing data", "case %s", name)
	}
}

func TestAcquireRejectsCorruptArchive(t *testing.T) {
	body := catalogArchive(t, nil)
	corrupt := append([]byte{}, body...)
	corrupt[len(corrupt)-10] ^= 0xff
	server, entry := catalogServer(t, corrupt, http.StatusOK)
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "invalid package archive")

	truncated := body[:len(body)-10]
	server, entry = catalogServer(t, truncated, http.StatusOK)
	_, err = AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "invalid package archive")

	tiny := []byte{0x1f}
	server, entry = catalogServer(t, tiny, http.StatusOK)
	_, err = AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "invalid package archive")
}

func TestAcquireRejectsDangerousEntries(t *testing.T) {
	files := validFixtureFiles(t)
	longDir := strings.Repeat("p", 155)
	longName := strings.Repeat("n", 100)
	cases := map[string]func(writer *tar.Writer){
		"symlink": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "evil", Typeflag: tar.TypeSymlink, Linkname: "backend", Mode: 0o777}))
		},
		"hardlink": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "evil", Typeflag: tar.TypeLink, Linkname: "backend", Mode: 0o644}))
		},
		"absolute": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("escape")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "/absolute", Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"backslash": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("escape")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: `sub\..`, Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"dotfile": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("hidden")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: ".hidden/state", Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"receipt at root": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("{}")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: ".fountain-package.json", Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"receipt nested": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("{}")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "assets/.fountain-package.json", Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"duplicate": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("again")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "backend", Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0o755}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"below a file": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("nested")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "backend/evil", Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"non-canonical dir": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "assets/./extra/", Typeflag: tar.TypeDir, Mode: 0o755}))
		},
		"overlong path": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("long")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: longDir + "/" + longName, Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"pax format": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("pax")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "pax-file", Typeflag: tar.TypeReg, Format: tar.FormatPAX, PAXRecords: map[string]string{"comment": "pax"}, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"gnu format": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			payload := []byte("gnu")
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "gnu-file", Typeflag: tar.TypeReg, Format: tar.FormatGNU, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		},
		"device": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "device", Typeflag: tar.TypeChar, Mode: 0o644}))
		},
		"fifo": func(writer *tar.Writer) {
			writeFixtureTar(t, writer, files)
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: "pipe", Typeflag: tar.TypeFifo, Mode: 0o644}))
		},
	}
	for name, write := range cases {
		archive := tarGzip(t, write)
		server, entry := catalogServer(t, archive, http.StatusOK)
		_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
		require.Error(t, err, "case %s", name)
	}
}

func TestAcquireRejectsEntryCountAndDataCaps(t *testing.T) {
	// The tar writer cannot finish a short file, so the close error is
	// dropped: the header alone must trigger the data cap.
	var huge bytes.Buffer
	func() {
		unzip := gzip.NewWriter(&huge)
		writer := tar.NewWriter(unzip)
		payload := []byte("x")
		require.NoError(t, writer.WriteHeader(&tar.Header{Name: "big", Typeflag: tar.TypeReg, Size: sdk.MaximumArchiveBytes + 1, Mode: 0o644}))
		_, _ = writer.Write(payload)
		_ = writer.Close()
		require.NoError(t, unzip.Close())
	}()
	server, entry := catalogServer(t, huge.Bytes(), http.StatusOK)
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "exceeds 10000 files or 512 MiB")

	many := tarGzip(t, func(writer *tar.Writer) {
		payload := []byte("x")
		for i := 0; i < 10001; i++ {
			name := "f" + strconv.Itoa(i)
			require.NoError(t, writer.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Size: int64(len(payload)), Mode: 0o644}))
			_, err := writer.Write(payload)
			require.NoError(t, err)
		}
	})
	server, entry = catalogServer(t, many, http.StatusOK)
	_, err = AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "exceeds 10000 files or 512 MiB")
}

func TestAcquireRejectsCapabilityMismatch(t *testing.T) {
	archive := catalogArchive(t, func(files map[string][]byte) {
		manifest := sdk.Manifest{Protocol: sdk.Protocol, ID: "tailnet", Name: "Tailnet", Version: "1.2.3", Executable: "backend", Capabilities: []string{"native.actor.read"}, Pages: []sdk.Page{{ID: "home", Title: "Home", Scope: "global", Entry: "main.js"}}}
		encoded, err := json.Marshal(manifest)
		require.NoError(t, err)
		files["extension.json"] = encoded
	})
	server, entry := catalogServer(t, archive, http.StatusOK)
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.ErrorContains(t, err, "capabilities do not match")
}

func TestAcquireCachesVerifiedArchives(t *testing.T) {
	body := catalogArchive(t, nil)
	server, entry := catalogServer(t, body, http.StatusOK)
	root := t.TempDir()

	stage, err := AcquireWithClient(context.Background(), root, entry, testClient(t, server))
	require.NoError(t, err)
	_ = os.RemoveAll(stage)
	cached := filepath.Join(root, ".cache", "packages", entry.SHA256+".tar.gz")
	info, err := os.Stat(cached)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// Offline reuse rechecks the cache without touching the network.
	server.Close()
	stage, err = AcquireWithClient(context.Background(), root, entry, testClient(t, server))
	require.NoError(t, err)
	_ = os.RemoveAll(stage)

	// A corrupt cache entry is removed and reacquired, leaving no other
	// entries behind.
	server2, entry2 := catalogServer(t, catalogArchive(t, nil), http.StatusOK)
	stage2, err := AcquireWithClient(context.Background(), root, entry2, testClient(t, server2))
	require.NoError(t, err)
	_ = os.RemoveAll(stage2)
	other := filepath.Join(root, ".cache", "packages", entry2.SHA256+".tar.gz")
	require.FileExists(t, other)
	require.NoError(t, os.WriteFile(cached, []byte("corrupt"), 0o600))
	server3, entry3 := catalogServer(t, body, http.StatusOK)
	stage3, err := AcquireWithClient(context.Background(), root, entry3, testClient(t, server3))
	require.NoError(t, err)
	_ = os.RemoveAll(stage3)
	require.FileExists(t, other)
	require.NoError(t, EvictCache(root, entry2.SHA256))
	require.NoFileExists(t, other)
}

func TestAcquireNeverLeaksPeerURLs(t *testing.T) {
	body := catalogArchive(t, nil)
	server, entry := catalogServer(t, body, http.StatusServiceUnavailable)
	_, err := AcquireWithClient(context.Background(), t.TempDir(), entry, testClient(t, server))
	require.Error(t, err)
	require.NotContains(t, err.Error(), server.URL)
	require.NotContains(t, err.Error(), "http")
}
