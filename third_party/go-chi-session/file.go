// Copyright 2013 Beego Authors
// Copyright 2014 The Macaron Authors
// Copyright 2024 The Forgejo Authors
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
	"bytes"
	"crypto/rand"
	"encoding/gob"
	"errors"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gofrs/flock"
)

// ErrSessionConflict means another request persisted changes after this snapshot
// was read. Its changes have not been written; blindly retrying would lose data.
var ErrSessionConflict = errors.New("session changed since it was read")

const fileRecordFormat = "forgejo-session-1"

// A generation identifies a session lifetime, and a revision identifies one
// persisted snapshot within that lifetime. Neither is browser-visible.
type fileRecord struct {
	Format     string
	Generation string
	Revision   uint64
	Data       []byte
}

type FileStore struct {
	p          *FileProvider
	sid        string
	lock       sync.RWMutex
	data       map[any]any
	generation string
	revision   uint64
	dirty      bool
}

// NewFileStore constructs a store. Persistence requires a generation bound by
// FileProvider.Read; a manually constructed snapshot cannot invent authority.
func NewFileStore(p *FileProvider, sid string, kv map[any]any) *FileStore {
	return &FileStore{p: p, sid: sid, data: kv}
}

func (s *FileStore) Set(key, val any) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data[key] = val
	s.dirty = true
	return nil
}

func (s *FileStore) Get(key any) any {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return s.data[key]
}

func (s *FileStore) Delete(key any) error {
	s.lock.Lock()
	defer s.lock.Unlock()
	delete(s.data, key)
	s.dirty = true
	return nil
}

func (s *FileStore) ID() string { return s.sid }

// Release never creates a missing session. An invalidated snapshot is discarded;
// a live but conflicting revision reports an error instead of losing newer data.
func (s *FileStore) Release() error {
	s.lock.Lock()
	defer s.lock.Unlock()
	if !s.dirty {
		return nil
	}
	return s.p.withLock(func() error {
		current, _, err := s.p.load(s.sid)
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if current.Generation != s.generation {
			return nil
		}
		if current.Revision != s.revision {
			return ErrSessionConflict
		}
		if current.Revision == ^uint64(0) {
			return errors.New("session revision exhausted")
		}
		current.Revision++
		if err := s.p.save(s.sid, current, s.data); err != nil {
			return err
		}
		s.revision = current.Revision
		s.dirty = false
		return nil
	})
}

func (s *FileStore) Flush() error {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.data = make(map[any]any)
	s.dirty = true
	return nil
}

func (s *FileStore) Empty() bool {
	s.lock.RLock()
	defer s.lock.RUnlock()
	return len(s.data) == 0
}

// FileProvider serializes lifecycle operations locally and across independent
// providers sharing the same directory. The lock file must never be removed
// while that directory is in service: unlinking it would split the lock domain.
type FileProvider struct {
	lock        sync.Mutex
	fileLock    *flock.Flock
	maxlifetime int64
	rootPath    string
}

func (p *FileProvider) Init(maxlifetime int64, rootPath string) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		return err
	}
	p.maxlifetime = maxlifetime
	p.rootPath = rootPath
	p.fileLock = flock.New(filepath.Join(rootPath, ".session.lock"))
	return nil
}

func (p *FileProvider) withLock(fn func() error) (err error) {
	p.lock.Lock()
	defer p.lock.Unlock()
	if err = p.fileLock.Lock(); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, p.fileLock.Unlock()) }()
	return fn()
}

func (p *FileProvider) filepath(sid string) string {
	return filepath.Join(p.rootPath, sid[:1], sid[1:2], sid)
}

func validFileSID(sid string) bool {
	if len(sid) < 2 {
		return false
	}
	for _, c := range sid {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// load neither creates a missing session nor extends its lifetime. Every caller
// holds the provider's lifecycle lock, including Release and GC.
func (p *FileProvider) load(sid string) (record fileRecord, data map[any]any, err error) {
	if !validFileSID(sid) {
		return record, nil, errors.New("invalid session ID")
	}
	filename := p.filepath(sid)
	info, err := os.Stat(filename)
	if err != nil {
		return record, nil, err
	}
	if info.ModTime().Unix()+p.maxlifetime < time.Now().Unix() {
		if err := os.Remove(filename); err != nil {
			return record, nil, err
		}
		return record, nil, fs.ErrNotExist
	}
	encoded, err := os.ReadFile(filename)
	if err != nil {
		return record, nil, err
	}
	if err = gob.NewDecoder(bytes.NewReader(encoded)).Decode(&record); err != nil {
		return record, nil, err
	}
	if record.Format != fileRecordFormat || record.Generation == "" || record.Revision == 0 {
		return record, nil, errors.New("invalid session record format")
	}
	data, err = DecodeGob(record.Data)
	if err == nil && data == nil {
		data = make(map[any]any)
	}
	return record, data, err
}

func (p *FileProvider) save(sid string, record fileRecord, data map[any]any) (err error) {
	record.Data, err = EncodeGob(data)
	if err != nil {
		return err
	}
	var encoded bytes.Buffer
	if err = gob.NewEncoder(&encoded).Encode(record); err != nil {
		return err
	}
	filename := p.filepath(sid)
	if err = os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(filename), ".session-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, writeErr := f.Write(encoded.Bytes())
	closeErr := f.Close()
	if err = errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}

func (p *FileProvider) read(sid string) (RawStore, error) {
	record, data, err := p.load(sid)
	if errors.Is(err, fs.ErrNotExist) {
		record = fileRecord{Format: fileRecordFormat, Generation: rand.Text(), Revision: 1}
		data = make(map[any]any)
		if err = p.save(sid, record, data); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	now := time.Now()
	if err = os.Chtimes(p.filepath(sid), now, now); err != nil {
		return nil, err
	}
	store := NewFileStore(p, sid, data)
	store.generation, store.revision = record.Generation, record.Revision
	return store, nil
}

func (p *FileProvider) Read(sid string) (store RawStore, err error) {
	err = p.withLock(func() error { store, err = p.read(sid); return err })
	return store, err
}

func (p *FileProvider) Exist(sid string) bool {
	var exists bool
	err := p.withLock(func() error {
		if !validFileSID(sid) {
			return errors.New("invalid session ID")
		}
		_, err := os.Stat(p.filepath(sid))
		exists = err == nil
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
	return err == nil && exists
}

func (p *FileProvider) Destroy(sid string) error {
	return p.withLock(func() error {
		if !validFileSID(sid) {
			return errors.New("invalid session ID")
		}
		err := os.Remove(p.filepath(sid))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	})
}

func (p *FileProvider) Regenerate(oldsid, sid string) (store RawStore, err error) {
	err = p.withLock(func() error {
		if !validFileSID(oldsid) || !validFileSID(sid) {
			return errors.New("invalid session ID")
		}
		if _, err := os.Stat(p.filepath(sid)); err == nil {
			return errors.New("new session ID already exists")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		_, data, err := p.load(oldsid)
		if errors.Is(err, fs.ErrNotExist) {
			data = make(map[any]any)
		} else if err != nil {
			return err
		}
		// Remove the old authority first: a failed replacement must not leave two
		// authenticated generations alive. All operations hold the shared lock.
		if err = os.Remove(p.filepath(oldsid)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		record := fileRecord{Format: fileRecordFormat, Generation: rand.Text(), Revision: 1}
		if err = p.save(sid, record, data); err != nil {
			return err
		}
		result := NewFileStore(p, sid, data)
		result.generation, result.revision = record.Generation, record.Revision
		store = result
		return nil
	})
	return store, err
}

func (p *FileProvider) Count() int {
	count := 0
	err := p.withLock(func() error {
		return filepath.WalkDir(p.rootPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && validFileSID(d.Name()) {
				count++
			}
			return nil
		})
	})
	if err != nil {
		log.Printf("error counting session files: %v", err)
		return 0
	}
	return count
}

func (p *FileProvider) GC() {
	err := p.withLock(func() error {
		return filepath.WalkDir(p.rootPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !(validFileSID(d.Name()) || strings.HasPrefix(d.Name(), ".session-")) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if info.ModTime().Unix()+p.maxlifetime < time.Now().Unix() {
				return os.Remove(path)
			}
			return nil
		})
	})
	if err != nil {
		log.Printf("error garbage collecting session files: %v", err)
	}
}

func init() { Register("file", &FileProvider{}) }
