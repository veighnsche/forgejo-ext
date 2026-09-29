// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// This package is built separately from Forgejo. It deliberately imports only
// the public extension SDK, not Forgejo's models or services.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"unicode/utf8"

	"forgejo.org/modules/extensions"
)

const maxNoteBytes = 16 << 10

func main() {
	if err := extensions.Serve(newHandler(extensions.DataDir())); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newHandler(dataDir string) http.Handler {
	handler := http.NewServeMux()
	handler.HandleFunc("GET /context", func(w http.ResponseWriter, r *http.Request) {
		authority, err := extensions.RequestContext(r)
		if err != nil {
			http.Error(w, "Missing native authority", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(authority)
	})
	handler.HandleFunc("GET /notes", notesHandler(dataDir))
	handler.HandleFunc("PUT /notes", notesHandler(dataDir))
	return handler
}

func notesHandler(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authority, err := extensions.RequestContext(r)
		if err != nil || authority.Actor.ID <= 0 || authority.Scope != "panel" || authority.PageID != "notes" {
			http.Error(w, "Missing native panel authority", http.StatusUnauthorized)
			return
		}
		if dataDir == "" {
			http.Error(w, "Notes storage is unavailable", http.StatusInternalServerError)
			return
		}
		path := filepath.Join(dataDir, "notes", fmt.Sprintf("%d.txt", authority.Actor.ID))
		if r.Method == http.MethodGet {
			body, err := os.ReadFile(path)
			if errors.Is(err, os.ErrNotExist) {
				body = nil
			} else if err != nil {
				http.Error(w, "Could not read notes", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write(body)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxNoteBytes+1))
		if err != nil {
			http.Error(w, "Could not read note", http.StatusBadRequest)
			return
		}
		if len(body) > maxNoteBytes {
			http.Error(w, "Note exceeds 16 KiB", http.StatusRequestEntityTooLarge)
			return
		}
		if !utf8.Valid(body) {
			http.Error(w, "Note must be UTF-8 text", http.StatusBadRequest)
			return
		}
		if err := writeNote(path, body); err != nil {
			http.Error(w, "Could not save note", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeNote(path string, body []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(directory, ".note-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(body)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	if writeErr != nil {
		_ = f.Close()
		return writeErr
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
