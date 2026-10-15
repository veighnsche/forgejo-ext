// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

// This package is built separately from Forgejo. It deliberately imports only
// the public extension SDK, not Forgejo's models or services.
package main

import (
	"encoding/json" //nolint:depguard // The independently built example imports only the public SDK.
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"unicode/utf8"

	extensions "forgejo.org/extension-sdk"

	"github.com/coder/websocket"
)

const maxNoteBytes = 16 << 10

func main() {
	if err := extensions.Serve(extensions.Application{HTTP: newHandler(extensions.DataDir())}); err != nil {
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
	handler.HandleFunc("GET /stream", streamHandler())
	return handler
}

// streamHandler serves the admitted notes-panel stream: the host proxies an
// authenticated browser websocket here carrying only host-minted admission,
// never browser credentials. Admission mirrors the integration fixture:
// native panel authority, no leaked credential headers, and a native
// callback round-trip before the socket is accepted. The panel greets with
// {"hello": username} and echoes JSON payloads (ping replies).
func streamHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authority, err := extensions.RequestContext(r)
		if err != nil || authority.Actor.ID == "" || authority.Contribution.Kind != "panel" || authority.Contribution.ID != "notes" {
			http.Error(w, "Missing native panel authority", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Sec-WebSocket-Protocol") != "" || r.Header.Get(extensions.SessionGenerationHeader) != "" {
			http.Error(w, "browser credential leaked", http.StatusForbidden)
			return
		}
		actor, err := authority.Native().CurrentActor(r.Context())
		if err != nil {
			http.Error(w, "native callback", http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		hello, err := json.Marshal(map[string]string{"hello": actor.Username})
		if err != nil {
			return
		}
		if err := conn.Write(r.Context(), websocket.MessageText, hello); err != nil {
			return
		}
		for {
			kind, body, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			if kind != websocket.MessageText && kind != websocket.MessageBinary {
				continue
			}
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				continue
			}
			echo, err := json.Marshal(payload)
			if err != nil {
				return
			}
			if err := conn.Write(r.Context(), websocket.MessageText, echo); err != nil {
				return
			}
		}
	}
}

func notesHandler(dataDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authority, err := extensions.RequestContext(r)
		if err != nil || authority.Actor.ID == "" || authority.Contribution.Kind != "panel" || authority.Contribution.ID != "notes" {
			http.Error(w, "Missing native panel authority", http.StatusUnauthorized)
			return
		}
		if dataDir == "" {
			http.Error(w, "Notes storage is unavailable", http.StatusInternalServerError)
			return
		}
		path := filepath.Join(dataDir, "notes", authority.Actor.ID+".txt")
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
