// Copyright 2013 Beego Authors
// Copyright 2014 The Macaron Authors
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
	"container/list"
	"fmt"
	"sync"
	"time"
)

// MemStore represents a in-memory session store implementation.
type MemStore struct {
	sid        string
	lock       sync.RWMutex
	data       map[any]any
	lastAccess time.Time
}

// NewMemStore creates and returns a memory session store.
func NewMemStore(sid string) *MemStore {
	return &MemStore{
		sid:        sid,
		data:       make(map[any]any),
		lastAccess: time.Now(),
	}
}

// Set sets value to given key in session.
func (s *MemStore) Set(key, val any) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.data[key] = val
	return nil
}

// Get gets value by given key in session.
func (s *MemStore) Get(key any) any {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.data[key]
}

// Delete deletes a key from session.
func (s *MemStore) Delete(key any) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	delete(s.data, key)
	return nil
}

// ID returns current session ID.
func (s *MemStore) ID() string {
	return s.sid
}

// Release releases resource and save data to provider.
func (*MemStore) Release() error {
	return nil
}

// Flush deletes all session data.
func (s *MemStore) Flush() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.data = make(map[any]any)
	return nil
}

// True if no keys have been set
func (s *MemStore) Empty() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return len(s.data) == 0
}

// MemProvider represents a in-memory session provider implementation.
type MemProvider struct {
	lock        sync.RWMutex
	maxLifetime int64
	data        map[string]*list.Element
	// A priority list whose lastAccess newer gets higher priority.
	list *list.List
}

// Init initializes memory session provider.
func (p *MemProvider) Init(maxLifetime int64, _ string) error {
	p.lock.Lock()
	p.list = list.New()
	p.data = make(map[string]*list.Element)
	p.maxLifetime = maxLifetime
	p.lock.Unlock()
	return nil
}

// Read returns the current session lifetime; expiry detaches the old store.
func (p *MemProvider) Read(sid string) (RawStore, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if e, ok := p.data[sid]; ok {
		store := e.Value.(*MemStore)
		if store.lastAccess.Unix()+p.maxLifetime >= time.Now().Unix() {
			store.lastAccess = time.Now()
			p.list.MoveToFront(e)
			return store, nil
		}
		p.list.Remove(e)
		delete(p.data, sid)
	}
	store := NewMemStore(sid)
	p.data[sid] = p.list.PushFront(store)
	return store, nil
}

// Exist returns true if session with given ID exists.
func (p *MemProvider) Exist(sid string) bool {
	p.lock.RLock()
	defer p.lock.RUnlock()

	_, ok := p.data[sid]
	return ok
}

// Destroy deletes a session by session ID.
func (p *MemProvider) Destroy(sid string) error {
	p.lock.Lock()
	defer p.lock.Unlock()

	e, ok := p.data[sid]
	if !ok {
		return nil
	}

	p.list.Remove(e)
	delete(p.data, sid)
	return nil
}

// Regenerate creates a detached store, so references retained by earlier
// requests cannot mutate the new lifetime. Provider state changes atomically.
func (p *MemProvider) Regenerate(oldsid, sid string) (RawStore, error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if _, exists := p.data[sid]; exists {
		return nil, fmt.Errorf("new sid '%s' already exists", sid)
	}
	next := NewMemStore(sid)
	if e, exists := p.data[oldsid]; exists {
		previous := e.Value.(*MemStore)
		if previous.lastAccess.Unix()+p.maxLifetime >= time.Now().Unix() {
			// Use the same value codec as the persistent providers to detach mutable
			// values as well as the top-level map (for example WebAuthn session data).
			previous.lock.RLock()
			encoded, err := EncodeGob(previous.data)
			previous.lock.RUnlock()
			if err != nil {
				return nil, err
			}
			next.data, err = DecodeGob(encoded)
			if err != nil {
				return nil, err
			}
		}
		p.list.Remove(e)
		delete(p.data, oldsid)
	}
	p.data[sid] = p.list.PushFront(next)
	return next, nil
}

// Count counts and returns number of sessions.
func (p *MemProvider) Count() int {
	p.lock.RLock()
	defer p.lock.RUnlock()
	return p.list.Len()
}

// GC calls GC to clean expired sessions.
func (p *MemProvider) GC() {
	p.lock.Lock()
	defer p.lock.Unlock()
	for {
		e := p.list.Back()
		if e == nil {
			return
		}
		store := e.Value.(*MemStore)
		if store.lastAccess.Unix()+p.maxLifetime >= time.Now().Unix() {
			return
		}
		p.list.Remove(e)
		delete(p.data, store.sid)
	}
}

func init() {
	Register("memory", &MemProvider{})
}
