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
	"context"
	"path"
	"sync"
)

const (
	tfStateInDir   = "/tfstate"
	tfStateOutDir  = "/tfstate-out"
	tfConfigDir    = "/tf"
	tfVarsDir      = "/tfvars"
	tfProvidersDir = "/terraformer-providers"
	tfPluginsDir   = ".terraform/plugins/linux_amd64"
)

var (
	tfStateInPath    = path.Join(tfStateInDir, "/terraform.tfstate")
	tfStateOutPath   = path.Join(tfStateOutDir, "/terraform.tfstate")
	tfConfigMainPath = path.Join(tfConfigDir, "/main.tf")
	tfConfigVarsPath = path.Join(tfConfigDir, "/variables.tf")
	tfVarsPath       = path.Join(tfVarsDir, "/terraform.tfvars")
)

func (t *Terraformer) fetchConfig(ctx context.Context) error {
	return nil
}

func (t *Terraformer) storeState(ctx context.Context) error {
	return nil
}

func (t *Terraformer) startFileWatcher(ctx context.Context, wg *sync.WaitGroup) error {
	wg.Add(1)

	go func() {
		wg.Done()
	}()

	return nil
}
