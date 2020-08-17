// Copyright (c) 2020 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package terraformer

import (
	"bytes"
	"io"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type store interface {
	Object() runtime.Object
	Read(key string) io.Reader
	Store(key string, value *bytes.Buffer)
}

var _ store = &configMapStore{}

type configMapStore struct {
	*corev1.ConfigMap
}

func (c *configMapStore) Object() runtime.Object {
	return c.ConfigMap
}

func (c *configMapStore) Read(key string) io.Reader {
	return strings.NewReader(c.Data[key])
}

func (c *configMapStore) Store(key string, value *bytes.Buffer) {
	if c.ConfigMap.Data == nil {
		c.ConfigMap.Data = make(map[string]string, 1)
	}

	c.ConfigMap.Data[key] = value.String()
}

var _ store = &secretStore{}

type secretStore struct {
	*corev1.Secret
}

func (s *secretStore) Object() runtime.Object {
	return s.Secret
}

func (s *secretStore) Read(key string) io.Reader {
	return bytes.NewReader(s.Data[key])
}

func (s *secretStore) Store(key string, value *bytes.Buffer) {
	if s.Secret.Data == nil {
		s.Secret.Data = make(map[string][]byte, 1)
	}

	s.Secret.Data[key] = value.Bytes()
}
