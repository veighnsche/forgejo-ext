// Copyright 2013 Beego Authors
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

// Package session a middleware that provides the session management of Macaron.
package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"
)

// RawStore is the interface that operates the session data.
type RawStore interface {
	// Set sets value to given key in session.
	Set(any, any) error
	// Get gets value by given key in session.
	Get(any) any
	// Delete deletes a key from session.
	Delete(any) error
	// ID returns current session ID.
	ID() string
	// Release releases session resource and save data to provider.
	Release() error
	// Flush deletes all session data.
	Flush() error
	// Empty returns true if no keys have been set
	Empty() bool
}

// Store is the interface that contains all data for one session process with specific ID.
type Store interface {
	RawStore
	// Read returns raw session store by session ID.
	Read(string) (RawStore, error)
	// Destroy deletes a session.
	Destroy(http.ResponseWriter, *http.Request) error
	// RegenerateID regenerates a session store from old session ID to new one.
	RegenerateID(http.ResponseWriter, *http.Request) (RawStore, error)
	// Count counts and returns number of sessions.
	Count() int
	// GC calls GC to clean expired sessions.
	GC()
}

type store struct {
	RawStore
	*Manager
}

var _ Store = &store{}

// Options represents a struct for specifying configuration options for the session middleware.
type Options struct {
	// Name of provider. Default is "memory".
	Provider string
	// Provider configuration, it's corresponding to provider.
	ProviderConfig string
	// Cookie name to save session ID. Default is "MacaronSession".
	CookieName string
	// Cookie path to store. Default is "/".
	CookiePath string
	// GC interval time in seconds. Default is 3600.
	Gclifetime int64
	// Max life time in seconds. Default is whatever GC interval time is.
	Maxlifetime int64
	// Use HTTPS only. Default is false.
	Secure bool
	// Cookie life time. Default is 0.
	CookieLifeTime int
	// SameSite set the cookie SameSite
	SameSite http.SameSite
	// Cookie domain name. Default is empty.
	Domain string
	// Session ID length. Default is 16.
	IDLength int
	// Ignore release for websocket. Default is false.
	IgnoreReleaseForWebSocket bool
	// Defer Set-Cookie to avoid session cookies for empty sessions, see
	// comment before deferredSessionHandler()
	DeferSetCookie bool
}

// PrepareOptions gives some default values for options
func PrepareOptions(options []Options) Options {
	var opt Options
	if len(options) > 0 {
		opt = options[0]
	}

	if len(opt.Provider) == 0 {
		opt.Provider = "memory"
	}
	if len(opt.ProviderConfig) == 0 {
		opt.ProviderConfig = "data/sessions"
	}
	if len(opt.CookieName) == 0 {
		opt.CookieName = "MacaronSession"
	}
	if len(opt.CookiePath) == 0 {
		opt.CookiePath = "/"
	}
	if opt.Gclifetime == 0 {
		opt.Gclifetime = 3600
	}
	if opt.Maxlifetime == 0 {
		opt.Maxlifetime = opt.Gclifetime
	}
	if !opt.Secure {
		opt.Secure = false
	}
	if opt.IDLength == 0 {
		opt.IDLength = 16
	}

	return opt
}

// GetCookie returns given cookie value from request header.
func GetCookie(req *http.Request, name string) string {
	cookie, err := req.Cookie(name)
	if err != nil {
		return ""
	}
	val, _ := url.QueryUnescape(cookie.Value)
	return val
}

func immediateHandler(manager *Manager, w http.ResponseWriter, req *http.Request, next http.Handler) {
	sess, err := manager.Start(w, req)
	if err != nil {
		panic("session(start): " + err.Error())
	}

	s := store{
		RawStore: sess,
		Manager:  manager,
	}

	req = req.WithContext(context.WithValue(req.Context(), any("Session"), &s)) //nolint:staticcheck

	next.ServeHTTP(w, req)

	if manager.opt.IgnoreReleaseForWebSocket && req.Header.Get("Upgrade") == "websocket" {
		return
	}

	if err = s.Release(); err != nil {
		panic("session(release): " + err.Error())
	}
}

func sessionHandler(manager *Manager) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			immediateHandler(manager, w, req, next)
		})
	}
}

type DeferredResponseWriter struct {
	http.ResponseWriter
	s       *store
	written bool
	undone  bool
}

const hSetCookie = "Set-Cookie"

func (w *DeferredResponseWriter) WriteHeader(statusCode int) {
	if w.written {
		return
	}

	defer func() {
		w.ResponseWriter.WriteHeader(statusCode)
		w.written = true
	}()

	if !w.s.Empty() {
		return
	}

	cookies, has := w.Header()[hSetCookie]
	if !has {
		return
	}

	// remove cookie with CookieName from set cookies
	w.Header().Del(hSetCookie)
	pfx := w.s.opt.CookieName + "="
	for _, cookie := range cookies {
		if strings.HasPrefix(cookie, pfx) {
			continue
		}
		w.Header().Add("Set-Cookie", cookie)
	}

	w.undone = true
}

func (w *DeferredResponseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

// Sessioner variant which optimistically tries to avoid setting a cookie for empty sessions
//
// When we write headers, we check if the session is empty and, if so, remove
// the Set-Cookie header for the session. Adding the Set-Cookie header
// internally and then removing it is both simple and preserves the existing
// behavior from the perspective of the calling code.
//
// The caveat is that any changes to a newly created session during body write
// get lost. The user perspective for this case is that no session appears to be
// created where it would be without DeferSetCookie.
//
// So, in short, the DeferSetCookie option can not be used if sessions get initially
// populated during body write, but that is likely a very rare use case.
//
// Modifying a session during body write is no problem if it was already
// populated before the body write.

func deferredHandler(manager *Manager, w http.ResponseWriter, req *http.Request, next http.Handler) {
	// If the request expects a SSE stream, then we cannot defer a response as it's a long-'polling' request.
	// Nevertheless, no cookies should be set or adjusted on a response for such request, avoid any deferring behavior.
	if strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		immediateHandler(manager, w, req, next)
		return
	}

	sess, err := manager.Start(w, req)
	if err != nil {
		panic("session(start): " + err.Error())
	}

	s := store{
		RawStore: sess,
		Manager:  manager,
	}

	writer := &DeferredResponseWriter{
		ResponseWriter: w,
		s:              &s,
	}

	req = req.WithContext(context.WithValue(req.Context(), any("Session"), &s)) //nolint:staticcheck

	next.ServeHTTP(writer, req)

	if manager.opt.IgnoreReleaseForWebSocket && req.Header.Get("Upgrade") == "websocket" {
		return
	}

	if err = s.Release(); err != nil {
		panic("session(release): " + err.Error())
	}

	if !writer.undone {
		return
	}
	if err = manager.provider.Destroy(sess.ID()); err != nil {
		panic("session(destroy): " + err.Error())
	}
}

func deferredSessionHandler(manager *Manager) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			deferredHandler(manager, w, req, next)
		})
	}
}

// Sessioner is a middleware that maps a session.SessionStore service into the Macaron handler chain.
// An single variadic session.Options struct can be optionally provided to configure.
func Sessioner(options ...Options) func(next http.Handler) http.Handler {
	opt := PrepareOptions(options)
	manager, err := NewManager(opt.Provider, opt)
	if err != nil {
		panic(err)
	}
	go manager.startGC()
	if opt.DeferSetCookie {
		return deferredSessionHandler(manager)
	}
	return sessionHandler(manager)
}

// GetSession returns session store
func GetSession(req *http.Request) Store {
	sessCtx := req.Context().Value("Session")
	sess, _ := sessCtx.(*store)
	return sess
}

// RegenerateSession
func RegenerateSession(resp http.ResponseWriter, req *http.Request) (Store, error) {
	sess, ok := GetSession(req).(*store)
	if !ok {
		return nil, errors.New("no session in request context")
	}

	oldRawStore := sess.RawStore
	if err := oldRawStore.Release(); err != nil {
		return nil, err
	}

	store, err := sess.RegenerateID(resp, req)
	if err != nil {
		return nil, err
	}
	sess.RawStore = store
	return sess, nil
}

// Provider is the interface that provides session manipulations.
type Provider interface {
	// Init initializes session provider.
	Init(gclifetime int64, config string) error
	// Read returns raw session store by session ID.
	Read(sid string) (RawStore, error)
	// Exist returns true if session with given ID exists.
	Exist(sid string) bool
	// Destroy deletes a session by session ID.
	Destroy(sid string) error
	// Regenerate regenerates a session store from old session ID to new one.
	Regenerate(oldsid, sid string) (RawStore, error)
	// Count counts and returns number of sessions.
	Count() int
	// GC calls GC to clean expired sessions.
	GC()
}

var providers = make(map[string]func() Provider)

// Register registers a provider.
func Register(name string, provider Provider) {
	if reflect.TypeOf(provider).Kind() == reflect.Pointer {
		// Pointer:
		RegisterFn(name, func() Provider {
			return reflect.New(reflect.ValueOf(provider).Elem().Type()).Interface().(Provider)
		})
		return
	}

	// Not a Pointer
	RegisterFn(name, func() Provider {
		return reflect.New(reflect.TypeOf(provider)).Elem().Interface().(Provider)
	})
}

// RegisterFn registers a provider function.
func RegisterFn(name string, providerfn func() Provider) {
	if providerfn == nil {
		panic("session: cannot register provider with nil value")
	}
	if _, dup := providers[name]; dup {
		panic(fmt.Errorf("session: cannot register provider '%s' twice", name))
	}

	providers[name] = providerfn
}

// Manager represents a struct that contains session provider and its configuration.
type Manager struct {
	provider Provider
	opt      Options
}

// NewManager creates and returns a new session manager by given provider name and configuration.
// It returns an error when requested provider name isn't registered.
func NewManager(name string, opt Options) (*Manager, error) {
	fn, ok := providers[name]
	if !ok {
		return nil, fmt.Errorf("session: unknown provider '%s'(forgotten import?)", name)
	}

	p := fn()

	return &Manager{p, opt}, p.Init(opt.Maxlifetime, opt.ProviderConfig)
}

// sessionID generates a new session ID.
// Gives half of the ID length amount of entropy.
func (m *Manager) sessionID() string {
	buf := make([]byte, m.opt.IDLength/2)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}

	return hex.EncodeToString(buf)
}

// validSessionID tests whether a provided session ID is a valid session ID.
func (m *Manager) validSessionID(sid string) (bool, error) {
	if len(sid) != m.opt.IDLength {
		return false, fmt.Errorf("invalid 'sid': %s %d != %d", sid, len(sid), m.opt.IDLength)
	}

	for i := range sid {
		switch {
		case '0' <= sid[i] && sid[i] <= '9':
		case 'a' <= sid[i] && sid[i] <= 'f':
		default:
			return false, errors.New("invalid 'sid': " + sid)
		}
	}
	return true, nil
}

// Start starts a session by generating new one
// or retrieve existence one by reading session ID from HTTP request if it's valid.
func (m *Manager) Start(resp http.ResponseWriter, req *http.Request) (RawStore, error) {
	sid := GetCookie(req, m.opt.CookieName)
	valid, _ := m.validSessionID(sid)
	if len(sid) > 0 && valid && m.provider.Exist(sid) {
		return m.provider.Read(sid)
	}

	sid = m.sessionID()
	sess, err := m.provider.Read(sid)
	if err != nil {
		return nil, err
	}

	cookie := &http.Cookie{
		Name:     m.opt.CookieName,
		Value:    sid,
		Path:     m.opt.CookiePath,
		HttpOnly: true,
		Secure:   m.opt.Secure,
		Domain:   m.opt.Domain,
		SameSite: m.opt.SameSite,
	}
	if m.opt.CookieLifeTime >= 0 {
		cookie.MaxAge = m.opt.CookieLifeTime
	}
	http.SetCookie(resp, cookie)
	req.AddCookie(cookie)
	return sess, nil
}

// Read returns raw session store by session ID.
func (m *Manager) Read(sid string) (RawStore, error) {
	// Ensure we're trying to read a valid session ID
	if _, err := m.validSessionID(sid); err != nil {
		return nil, err
	}

	return m.provider.Read(sid)
}

// Destroy deletes a session by given ID.
func (m *Manager) Destroy(resp http.ResponseWriter, req *http.Request) error {
	sid := GetCookie(req, m.opt.CookieName)
	if len(sid) == 0 {
		return nil
	}

	if _, err := m.validSessionID(sid); err != nil {
		return err
	}

	if err := m.provider.Destroy(sid); err != nil {
		return err
	}
	cookie := &http.Cookie{
		Name:     m.opt.CookieName,
		Domain:   m.opt.Domain,
		Path:     m.opt.CookiePath,
		HttpOnly: true,
		Secure:   m.opt.Secure,
		SameSite: m.opt.SameSite,
		Expires:  time.Now(),
		MaxAge:   -1,
	}
	http.SetCookie(resp, cookie)
	return nil
}

// RegenerateID regenerates a session store from old session ID to new one.
func (m *Manager) RegenerateID(resp http.ResponseWriter, req *http.Request) (sess RawStore, err error) {
	sid := m.sessionID()
	oldsid := GetCookie(req, m.opt.CookieName)
	_, err = m.validSessionID(oldsid)
	if err != nil {
		return nil, err
	}
	sess, err = m.provider.Regenerate(oldsid, sid)
	if err != nil {
		return nil, err
	}
	cookie := &http.Cookie{
		Name:     m.opt.CookieName,
		Value:    sid,
		Path:     m.opt.CookiePath,
		HttpOnly: true,
		Secure:   m.opt.Secure,
		Domain:   m.opt.Domain,
		SameSite: m.opt.SameSite,
	}
	if m.opt.CookieLifeTime >= 0 {
		cookie.MaxAge = m.opt.CookieLifeTime
	}
	http.SetCookie(resp, cookie)
	req.AddCookie(cookie)
	return sess, nil
}

// Count counts and returns number of sessions.
func (m *Manager) Count() int {
	return m.provider.Count()
}

// GC starts GC job in a certain period.
func (m *Manager) GC() {
	m.provider.GC()
}

// startGC starts GC job in a certain period.
func (m *Manager) startGC() {
	m.GC()
	time.AfterFunc(time.Duration(m.opt.Gclifetime)*time.Second, func() { m.startGC() })
}

// SetSecure indicates whether to set cookie with HTTPS or not.
func (m *Manager) SetSecure(secure bool) {
	m.opt.Secure = secure
}
