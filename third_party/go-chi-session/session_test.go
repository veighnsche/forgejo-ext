// Copyright 2014 The Macaron Authors
// Copyright 2024,2026 The Forgejo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License"): you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
// WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
// License for the specific language governing permissions and limitations
// under the License.

package session

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	chi "github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_Sessioner(t *testing.T) {
	t.Run("Use session middleware", func(t *testing.T) {
		c := chi.NewRouter()
		c.Use(Sessioner())
		c.Get("/", func(_ http.ResponseWriter, _ *http.Request) {})

		resp := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", nil)
		require.NoError(t, err)

		c.ServeHTTP(resp, req)
	})

	t.Run("Register invalid provider", func(t *testing.T) {
		t.Run("Provider not exists", func(t *testing.T) {
			assert.Panics(t, func() {
				c := chi.NewRouter()
				c.Use(Sessioner(Options{
					Provider: "fake",
				}))
			})
		})

		t.Run("Provider value is nil", func(t *testing.T) {
			assert.Panics(t, func() {
				Register("fake", nil)
			})
		})

		t.Run("Register twice", func(t *testing.T) {
			assert.Panics(t, func() {
				Register("memory", &MemProvider{})
			})
		})
	})
}

func testProvider(t *testing.T, opt Options) {
	t.Run("-", func(t *testing.T) {
		testProviderBase(t, opt)
	})
	t.Run("DeferSetCookie", func(t *testing.T) {
		addOpt := opt
		addOpt.DeferSetCookie = true
		testProviderBase(t, addOpt)
	})
}

func testProviderBase(t *testing.T, opt Options) {
	t.Run("Basic operation", func(t *testing.T) {
		c := chi.NewRouter()
		c.Use(Sessioner(opt))
		var initialSid string

		c.Get("/", func(_ http.ResponseWriter, req *http.Request) {
			sess := GetSession(req)
			assert.NoError(t, sess.Set("uname", "unknwon"))
			initialSid = sess.ID()
		})
		c.Get("/reg", func(resp http.ResponseWriter, req *http.Request) {
			sess := GetSession(req)
			assert.Equal(t, initialSid, sess.ID())
			raw, err := RegenerateSession(resp, req)
			assert.NoError(t, err)
			assert.NotNil(t, sess)
			assert.Equal(t, sess, raw)

			assert.NotEqual(t, initialSid, sess.ID())

			uname := sess.Get("uname")
			assert.NotNil(t, uname)
			assert.EqualValues(t, "unknwon", uname)

			assert.NoError(t, sess.Set("uname", "lunny"))
			uname = sess.Get("uname")
			assert.NotNil(t, uname)
			assert.EqualValues(t, "lunny", uname)
		})
		c.Get("/get", func(resp http.ResponseWriter, req *http.Request) {
			sess := GetSession(req)
			sid := sess.ID()
			assert.NotEmpty(t, sid)

			raw, err := sess.Read(sid)
			assert.NoError(t, err)
			assert.NotNil(t, raw)

			uname := sess.Get("uname")
			assert.NotNil(t, uname)
			assert.EqualValues(t, "lunny", uname)

			assert.NoError(t, sess.Delete("uname"))
			assert.Nil(t, sess.Get("uname"))

			assert.NoError(t, sess.Destroy(resp, req))
		})

		resp := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", nil)
		require.NoError(t, err)
		c.ServeHTTP(resp, req)

		cookie := resp.Header().Get("Set-Cookie")

		resp = httptest.NewRecorder()
		req, err = http.NewRequest("GET", "/reg", nil)
		require.NoError(t, err)
		req.Header.Set("Cookie", cookie)
		c.ServeHTTP(resp, req)

		cookie = resp.Header().Get("Set-Cookie")

		resp = httptest.NewRecorder()
		req, err = http.NewRequest("GET", "/get", nil)
		require.NoError(t, err)
		req.Header.Set("Cookie", cookie)
		c.ServeHTTP(resp, req)
	})

	t.Run("Empty session", func(t *testing.T) {
		c := chi.NewRouter()
		c.Use(Sessioner(opt))
		c.Get("/", func(resp http.ResponseWriter, req *http.Request) {
			sess := GetSession(req)
			assert.NotNil(t, sess)
			assert.True(t, sess.Empty())
			_, err := resp.Write(nil)
			assert.NoError(t, err)
		})

		resp := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", nil)
		require.NoError(t, err)
		c.ServeHTTP(resp, req)

		cookie := resp.Header().Get("Set-Cookie")
		if opt.DeferSetCookie {
			require.Empty(t, cookie)
		} else {
			require.NotEmpty(t, cookie)
		}
	})

	if opt.DeferSetCookie {
		t.Run("No defer on text/event-stream", func(t *testing.T) {
			c := chi.NewRouter()
			c.Use(Sessioner(opt))
			c.Get("/", func(resp http.ResponseWriter, req *http.Request) {
				sess := GetSession(req)
				assert.NotNil(t, sess)
				assert.True(t, sess.Empty())
				_, err := resp.Write(nil)
				assert.NoError(t, err)
			})

			// DeferSetCookie is disabled by "text/event-stream"
			resp := httptest.NewRecorder()
			req, err := http.NewRequest("GET", "/", nil)
			req.Header.Set("Accept", "text/event-stream")
			require.NoError(t, err)
			c.ServeHTTP(resp, req)

			cookie := resp.Header().Get("Set-Cookie")
			require.NotEmpty(t, cookie)

			// If a cookie exists, no new cookie is set
			resp = httptest.NewRecorder()
			req, err = http.NewRequest("GET", "/", nil)
			require.NoError(t, err)
			req.Header.Set("Cookie", cookie)
			c.ServeHTTP(resp, req)

			require.Empty(t, resp.Header().Get("Set-Cookie"))
		})
	}

	t.Run("Regenerate empty session", func(t *testing.T) {
		c := chi.NewRouter()
		c.Use(Sessioner(opt))
		c.Get("/", func(resp http.ResponseWriter, req *http.Request) {
			sess := GetSession(req)
			raw, err := sess.RegenerateID(resp, req)
			assert.NoError(t, err)
			assert.NotNil(t, raw)
			assert.True(t, sess.Empty())
			_, err = resp.Write(nil)
			assert.NoError(t, err)
		})

		resp := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", nil)
		require.NoError(t, err)
		req.Header.Set("Cookie", "MacaronSession=ad2c7e3cbecfcf48; Path=/;")
		c.ServeHTTP(resp, req)

		cookie := resp.Header().Get("Set-Cookie")
		if opt.DeferSetCookie {
			require.Empty(t, cookie)
		} else {
			require.NotEmpty(t, cookie)
		}
	})

	t.Run("GC session", func(t *testing.T) {
		c := chi.NewRouter()
		opt2 := opt
		opt2.Gclifetime = 1
		c.Use(Sessioner(opt2))

		c.Get("/", func(_ http.ResponseWriter, req *http.Request) {
			sess := GetSession(req)
			assert.NoError(t, sess.Set("uname", "unknwon"))
			assert.NotEmpty(t, sess.ID())
			uname := sess.Get("uname")
			assert.NotNil(t, uname)
			assert.EqualValues(t, "unknwon", uname)

			assert.NoError(t, sess.Flush())
			assert.True(t, sess.Empty())
			assert.Nil(t, sess.Get("uname"))

			time.Sleep(2 * time.Second)
			sess.GC()
			assert.Zero(t, sess.Count())
		})

		resp := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", nil)
		require.NoError(t, err)
		c.ServeHTTP(resp, req)
	})
	t.Run("Detect invalid sid", func(t *testing.T) {
		c := chi.NewRouter()
		c.Use(Sessioner(opt))
		c.Get("/", func(_ http.ResponseWriter, req *http.Request) {
			sess := GetSession(req)
			raw, err := sess.Read("../session/ad2c7e3cbecfcf486")
			assert.Contains(t, err.Error(), "invalid 'sid'")
			assert.Nil(t, raw)
		})

		resp := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", nil)
		require.NoError(t, err)
		c.ServeHTTP(resp, req)
	})
}
