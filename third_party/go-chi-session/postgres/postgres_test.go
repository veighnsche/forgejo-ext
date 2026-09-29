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
	"testing"

	"code.forgejo.org/go-chi/session"
	"code.forgejo.org/go-chi/session/internal/test"
)

func Test_PostgresProvider(t *testing.T) {
	test.Provider(t, session.Options{
		Provider:       "postgres",
		ProviderConfig: "user=postgres password=postgres host=pgsql dbname=testsession port=5432 sslmode=disable",
	})
}
