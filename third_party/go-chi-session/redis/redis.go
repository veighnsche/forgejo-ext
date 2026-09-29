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
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"code.forgejo.org/go-chi/session"
)

// since we do not use context define global once
var ctx = context.TODO()

// RedisStore represents a redis session store implementation.
type RedisStore struct {
	c           *redis.Client
	prefix, sid string
	duration    time.Duration
	lock        sync.RWMutex
	data        map[any]any
}

// NewRedisStore creates and returns a redis session store.
func NewRedisStore(c *redis.Client, prefix, sid string, dur time.Duration, kv map[any]any) *RedisStore {
	return &RedisStore{
		c:        c,
		prefix:   prefix,
		sid:      sid,
		duration: dur,
		data:     kv,
	}
}

// Set sets value to given key in session.
func (s *RedisStore) Set(key, val any) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.data[key] = val
	return nil
}

// Get gets value by given key in session.
func (s *RedisStore) Get(key any) any {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.data[key]
}

// Delete delete a key from session.
func (s *RedisStore) Delete(key any) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	delete(s.data, key)
	return nil
}

// ID returns current session ID.
func (s *RedisStore) ID() string {
	return s.sid
}

// Release releases resource and save data to provider.
func (s *RedisStore) Release() error {
	// Skip encoding if the data is empty
	if len(s.data) == 0 {
		return nil
	}

	data, err := session.EncodeGob(s.data)
	if err != nil {
		return err
	}

	return s.c.Set(ctx, s.prefix+s.sid, string(data), s.duration).Err()
}

// Flush deletes all session data.
func (s *RedisStore) Flush() error {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.data = make(map[any]any)
	return nil
}

// True if no keys have been set
func (s *RedisStore) Empty() bool {
	return len(s.data) == 0
}

// RedisProvider represents a redis session provider implementation.
type RedisProvider struct {
	c        *redis.Client
	duration time.Duration
	prefix   string
}

// Init initializes redis session provider.
// config: network=tcp,addr=:6379,password=macaron,db=0,pool_size=100,idle_timeout=180,prefix=session;
func (p *RedisProvider) Init(maxlifetime int64, config string) (err error) {
	p.duration, err = time.ParseDuration(fmt.Sprintf("%ds", maxlifetime))
	if err != nil {
		return err
	}

	settings := strings.Split(config, ",")

	opt := &redis.Options{
		Network: "tcp",
	}
	for _, setting := range settings {
		k, v, found := strings.Cut(setting, "=")
		if !found {
			return fmt.Errorf("session/redis: cannot find '=': %q", setting)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)

		switch k {
		case "network":
			opt.Network = v
		case "addr":
			opt.Addr = v
		case "password":
			opt.Password = v
		case "db":
			opt.DB, err = strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("error parsing db: %w", err)
			}
		case "pool_size":
			opt.PoolSize, err = strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("error parsing pool_size: %w", err)
			}
		case "idle_timeout":
			opt.ConnMaxIdleTime, err = time.ParseDuration(v + "s")
			if err != nil {
				return fmt.Errorf("error parsing idle timeout: %w", err)
			}
		case "prefix":
			p.prefix = v
		default:
			return fmt.Errorf("session/redis: unsupported option '%s'", k)
		}
	}

	p.c = redis.NewClient(opt)
	return p.c.Ping(ctx).Err()
}

// Read returns raw session store by session ID.
func (p *RedisProvider) Read(sid string) (session.RawStore, error) {
	psid := p.prefix + sid
	if !p.Exist(sid) {
		if err := p.c.Set(ctx, psid, "", p.duration).Err(); err != nil {
			return nil, err
		}
	}

	var kv map[any]any
	kvs, err := p.c.Get(ctx, psid).Result()
	if err != nil {
		return nil, err
	}
	if len(kvs) == 0 {
		kv = make(map[any]any)
	} else {
		kv, err = session.DecodeGob([]byte(kvs))
		if err != nil {
			return nil, err
		}
	}

	return NewRedisStore(p.c, p.prefix, sid, p.duration, kv), nil
}

// Exist returns true if session with given ID exists.
func (p *RedisProvider) Exist(sid string) bool {
	count, err := p.c.Exists(ctx, p.prefix+sid).Result()
	return err == nil && count == 1
}

// Destroy deletes a session by session ID.
func (p *RedisProvider) Destroy(sid string) error {
	return p.c.Del(ctx, p.prefix+sid).Err()
}

// Regenerate regenerates a session store from old session ID to new one.
func (p *RedisProvider) Regenerate(oldsid, sid string) (_ session.RawStore, err error) {
	poldsid := p.prefix + oldsid
	psid := p.prefix + sid

	if p.Exist(sid) {
		return nil, fmt.Errorf("new sid '%s' already exists", sid)
	} else if !p.Exist(oldsid) {
		// Make a fake old session.
		if err = p.c.Set(ctx, poldsid, "", p.duration).Err(); err != nil {
			return nil, err
		}
	}

	if err = p.c.Rename(ctx, poldsid, psid).Err(); err != nil {
		return nil, err
	}

	var kv map[any]any
	kvs, err := p.c.Get(ctx, psid).Result()
	if err != nil {
		return nil, err
	}

	if len(kvs) == 0 {
		kv = make(map[any]any)
	} else {
		kv, err = session.DecodeGob([]byte(kvs))
		if err != nil {
			return nil, err
		}
	}

	return NewRedisStore(p.c, p.prefix, sid, p.duration, kv), nil
}

// Count counts and returns number of sessions.
func (p *RedisProvider) Count() int {
	count, err := p.c.DBSize(ctx).Result()
	if err != nil {
		return 0
	}
	return int(count)
}

// GC calls GC to clean expired sessions.
func (*RedisProvider) GC() {}

func init() {
	session.Register("redis", &RedisProvider{})
}
