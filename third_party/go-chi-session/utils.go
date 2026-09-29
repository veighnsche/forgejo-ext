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
	"bytes"
	"encoding/gob"
)

func init() {
	gob.Register([]any{})
	gob.Register(map[int]any{})
	gob.Register(map[string]any{})
	gob.Register(map[any]any{})
	gob.Register(map[string]string{})
	gob.Register(map[int]string{})
	gob.Register(map[int]int{})
	gob.Register(map[int]int64{})
}

// EncodeGob encodes obj with gob
func EncodeGob(obj map[any]any) ([]byte, error) {
	for _, v := range obj {
		gob.Register(v)
	}
	buf := bytes.NewBuffer(nil)
	err := gob.NewEncoder(buf).Encode(obj)
	return buf.Bytes(), err
}

// DecodeGob decodes bytes to obj
func DecodeGob(encoded []byte) (out map[any]any, err error) {
	buf := bytes.NewBuffer(encoded)
	err = gob.NewDecoder(buf).Decode(&out)
	return out, err
}
