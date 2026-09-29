package test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"code.forgejo.org/go-chi/session"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Provider(t *testing.T, opt session.Options) {
	t.Run("Basic operation", func(t *testing.T) {
		c := chi.NewRouter()
		c.Use(session.Sessioner(opt))
		var initialSid string

		c.Get("/", func(_ http.ResponseWriter, req *http.Request) {
			sess := session.GetSession(req)
			assert.NoError(t, sess.Set("uname", "unknwon"))
			initialSid = sess.ID()
		})
		c.Get("/reg", func(resp http.ResponseWriter, req *http.Request) {
			sess := session.GetSession(req)
			assert.Equal(t, initialSid, sess.ID())
			raw, err := session.RegenerateSession(resp, req)
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
			sess := session.GetSession(req)
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

	t.Run("Regenerate empty session", func(t *testing.T) {
		c := chi.NewRouter()
		c.Use(session.Sessioner(opt))
		c.Get("/", func(resp http.ResponseWriter, req *http.Request) {
			sess := session.GetSession(req)
			raw, err := sess.RegenerateID(resp, req)
			assert.NoError(t, err)
			assert.NotNil(t, raw)
		})

		resp := httptest.NewRecorder()
		req, err := http.NewRequest("GET", "/", nil)
		require.NoError(t, err)
		req.Header.Set("Cookie", "MacaronSession=ad2c7e3cbecfcf48; Path=/;")
		c.ServeHTTP(resp, req)
	})

	t.Run("GC session", func(t *testing.T) {
		if opt.Provider == "redis" || opt.Provider == "memcache" {
			t.Skip("Doesn't implement GC")
		}

		c := chi.NewRouter()
		opt2 := opt
		opt2.Gclifetime = 1
		c.Use(session.Sessioner(opt2))

		c.Get("/", func(_ http.ResponseWriter, req *http.Request) {
			sess := session.GetSession(req)
			assert.NoError(t, sess.Set("uname", "unknwon"))
			assert.NotEmpty(t, sess.ID())
			uname := sess.Get("uname")
			assert.NotNil(t, uname)
			assert.EqualValues(t, "unknwon", uname)

			assert.NoError(t, sess.Flush())
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
		c.Use(session.Sessioner(opt))
		c.Get("/", func(_ http.ResponseWriter, req *http.Request) {
			sess := session.GetSession(req)
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
