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
	"sync"
	"testing"
)

type fileTestGC struct {
	sync.Mutex
	stopped bool
}

type fileTestProvider struct {
	FileProvider
	gc *fileTestGC
}

func (p *fileTestProvider) GC() {
	p.gc.Lock()
	defer p.gc.Unlock()
	if !p.gc.stopped {
		p.FileProvider.GC()
	}
}

func Test_FileProvider(t *testing.T) {
	root := t.TempDir()
	gc := new(fileTestGC)
	name := t.Name()
	RegisterFn(name, func() Provider { return &fileTestProvider{gc: gc} })
	t.Cleanup(func() {
		// Sessioner timers have no cancellation API. Wait for active GC and
		// prevent further filesystem access before TempDir removes the root.
		gc.Lock()
		gc.stopped = true
		gc.Unlock()
		delete(providers, name)
	})
	testProvider(t, Options{
		Provider:       name,
		ProviderConfig: root,
	})
}
