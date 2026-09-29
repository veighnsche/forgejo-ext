// Copyright 2026 The Forgejo Authors. All rights reserved.
// SPDX-License-Identifier: MIT

package common

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forgejo.org/modules/json"
	_ "forgejo.org/modules/session"
	"forgejo.org/modules/setting"
	forgejo_context "forgejo.org/services/context"

	"code.forgejo.org/go-chi/session"
	"github.com/klauspost/compress/gzhttp"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/websocket"
)

// This uses an actual TLS connection: a recorder cannot prove hijacking through
// the context, compression and deferred-session response writers.
func TestNativeSessionWebSocket(t *testing.T) {
	testNativeSession(t, true)
}

func TestNativeSessionDelayedRelease(t *testing.T) {
	testNativeSession(t, false)
}

func testNativeSession(t *testing.T, stream bool) {
	for _, provider := range []string{"memory", "file"} {
		for _, invalidation := range []string{"logout", "regenerate", "expire", "failure"} {
			if provider == "memory" && invalidation == "failure" {
				continue
			}
			t.Run(provider+"/"+invalidation, func(t *testing.T) {
				root := t.TempDir()
				old := setting.SessionConfig
				t.Cleanup(func() { setting.SessionConfig = old })
				shadow, err := json.Marshal(session.Options{Provider: provider, ProviderConfig: root})
				require.NoError(t, err)
				setting.SessionConfig.Provider = "VirtualSession"
				setting.SessionConfig.ProviderConfig = string(shadow)
				setting.SessionConfig.CookieName = "native-session"
				setting.SessionConfig.CookiePath = "/"
				setting.SessionConfig.Maxlifetime = 1
				setting.SessionConfig.Gclifetime = 86400
				started, finish, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
				requestDone := make(chan struct{})
				handler := Sessioner()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w = forgejo_context.WrapResponseWriter(w)
					s := session.GetSession(r)
					switch r.URL.Path {
					case "/login":
						require.NoError(t, s.Set("uid", int64(42)))
					case "/logout":
						require.NoError(t, s.Flush())
						require.NoError(t, s.Destroy(w, r))
					case "/regenerate":
						_, err := session.RegenerateSession(w, r)
						require.NoError(t, err)
					case "/check":
						if s.Get("uid") != int64(42) {
							w.WriteHeader(http.StatusUnauthorized)
							return
						}
					case "/stream":
						if s.Get("uid") != int64(42) {
							w.WriteHeader(http.StatusUnauthorized)
							return
						}
						if !stream {
							close(started)
							<-finish
							if provider == "file" {
								require.NoError(t, s.Set("completed", true))
							}
							w.WriteHeader(http.StatusNoContent)
							return
						}
						websocket.Handler(func(conn *websocket.Conn) {
							close(started)
							var value string
							if err := websocket.Message.Receive(conn, &value); err != nil {
								t.Error(err)
								return
							}
							if err := websocket.Message.Send(conn, value); err != nil {
								t.Error(err)
								return
							}
							<-finish
						}).ServeHTTP(w, r)
						return
					}
					w.WriteHeader(http.StatusNoContent)
				}))
				compress, err := gzhttp.NewWrapper(gzhttp.RandomJitter(32, 0, false), gzhttp.MinSize(gzhttp.DefaultMinSize))
				require.NoError(t, err)
				wrapped := compress(handler)
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					defer func() {
						if recover() != nil {
							w.WriteHeader(http.StatusInternalServerError)
						}
						if r.URL.Path == "/stream" {
							close(released)
						}
					}()
					wrapped.ServeHTTP(forgejo_context.WrapResponseWriter(w), r)
				}))
				t.Cleanup(server.Close)
				client := server.Client()
				client.Timeout = 5 * time.Second
				request := func(path string, cookie *http.Cookie) *http.Response {
					req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
					require.NoError(t, err)
					if cookie != nil {
						req.AddCookie(cookie)
					}
					resp, err := client.Do(req)
					require.NoError(t, err)
					_, err = io.Copy(io.Discard, resp.Body)
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
					return resp
				}
				login := request("/login", nil)
				require.Equal(t, http.StatusNoContent, login.StatusCode)
				require.Len(t, login.Cookies(), 1)
				cookie := login.Cookies()[0]
				if stream {
					config, err := websocket.NewConfig("wss"+strings.TrimPrefix(server.URL, "https")+"/stream", server.URL)
					require.NoError(t, err)
					config.TlsConfig = client.Transport.(*http.Transport).TLSClientConfig
					config.Header.Set("Cookie", cookie.String())
					config.Header.Set("Accept-Encoding", "gzip")
					conn, err := websocket.DialConfig(config)
					require.NoError(t, err)
					t.Cleanup(func() { _ = conn.Close() })
					require.NoError(t, conn.SetDeadline(time.Now().Add(8*time.Second)))
					<-started
					require.NoError(t, websocket.Message.Send(conn, "echo"))
					var echo string
					require.NoError(t, websocket.Message.Receive(conn, &echo))
					require.Equal(t, "echo", echo)
				} else {
					go func() {
						defer close(requestDone)
						request("/stream", cookie)
					}()
					<-started
				}
				sid, err := url.QueryUnescape(cookie.Value)
				require.NoError(t, err)
				file := filepath.Join(root, sid[:1], sid[1:2], sid)
				switch invalidation {
				case "logout", "regenerate":
					require.Equal(t, http.StatusNoContent, request("/"+invalidation, cookie).StatusCode)
				case "expire":
					if provider == "file" {
						expired := time.Now().Add(-10 * time.Second)
						require.NoError(t, os.Chtimes(file, expired, expired))
					} else {
						time.Sleep(2100 * time.Millisecond)
					}
				case "failure":
					require.NoError(t, os.WriteFile(file, []byte("invalid session gob"), 0o600))
				}
				close(finish)
				select {
				case <-released:
				case <-time.After(5 * time.Second):
					t.Fatal("stream did not return")
				}
				if !stream {
					<-requestDone
				}
				expected := http.StatusUnauthorized
				if invalidation == "failure" {
					expected = http.StatusInternalServerError
				}
				require.Equal(t, expected, request("/check", cookie).StatusCode)
			})
		}
	}
}
