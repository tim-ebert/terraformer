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
	"os"
	"path"
	"sync"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	tfConfigDir    = "/tf"
	tfVarsDir      = "/tfvars"
	tfStateInDir   = "/tfstate"
	tfStateOutDir  = "/tfstate-out"
	tfProvidersDir = "/terraformer-providers"
	tfPluginsDir   = ".terraform/plugins/linux_amd64"

	tfConfigMainKey = "main.tf"
	tfConfigVarsKey = "variables.tf"
	tfVarsKey       = "terraform.tfvars"
	tfStateKey      = "terraform.tfstate"
)

var (
	//tfConfigMainPath = path.Join(tfConfigDir, tfConfigMainKey)
	//tfConfigVarsPath = path.Join(tfConfigDir, tfConfigVarsKey)
	tfVarsPath       = path.Join(tfVarsDir, tfVarsKey)
	tfStateInPath    = path.Join(tfStateInDir, tfStateKey)
	tfStateOutPath   = path.Join(tfStateOutDir, tfStateKey)
)

func (t *Terraformer) ensureTFDirs() error {
	log := t.stepLogger("ensureTFDirs")
	log.Info("ensuring terraform directories")

	for _, dir := range []string{
		tfConfigDir,
		tfStateInDir,
		tfStateOutDir,
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		log.V(1).Info("directory ensured", "dir", dir)
	}

	return nil
}

func (t *Terraformer) fetchConfig(ctx context.Context) error {
	log := t.stepLogger("fetchConfig")

	var (
		wg      wait.Group
		allErrs *multierror.Error
		errCh   = make(chan error)
	)

	wg.Start(func() {
		errCh <- fetchConfigMap(ctx, log, t.client, t.config.Namespace, t.config.ConfigurationConfigMapName, false,
			tfConfigDir, tfConfigMainKey, tfConfigVarsKey,
		)
	})
	wg.Start(func() {
		errCh <- fetchConfigMap(ctx, log, t.client, t.config.Namespace, t.config.StateConfigMapName, true,
			tfStateInDir, tfStateKey,
		)
	})
	wg.Start(func() {
		errCh <- fetchSecret(ctx, log, t.client, t.config.Namespace, t.config.VariablesSecretName, false,
			tfVarsDir, tfVarsKey,
		)
	})

	go func() {
		for err := range errCh {
			allErrs = multierror.Append(allErrs, err)
		}
	}()

	wg.Wait()

	return allErrs.ErrorOrNil()
}

func fetchConfigMap(ctx context.Context, log logr.Logger, c client.Client, ns, name string, optional bool, dir string, dataKeys ...string) error {
	key := client.ObjectKey{Namespace: ns, Name: name}
	log.V(1).Info("fetching ConfigMap", key)

	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, key, cm); err != nil {
		if apierrors.IsNotFound(err) && optional {
			return nil
		}
		return err
	}
}

func fetchSecret(ctx context.Context, log logr.Logger, c client.Client, ns, name string, optional bool, dir string, dataKeys ...string) error {
	key := client.ObjectKey{Namespace: ns, Name: name}
	log.V(1).Info("fetching Secret", key)

	cm := &corev1.ConfigMap{}
	if err := c.Get(ctx, key, cm); err != nil {
		if apierrors.IsNotFound(err) && optional {
			return nil
		}
		return err
	}
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
