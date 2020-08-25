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
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/gardener/terraformer/pkg/utils"
)

const (
	tfConfigDir    = "/tf"
	tfVarsDir      = "/tfvars"
	tfStateInDir   = "/tfstate"
	tfStateOutDir  = "/tfstate-out"
	tfProvidersDir = "/terraform-providers"

	// TODO: still needed?
	tfPluginsDir   = ".terraform/plugins/linux_amd64"

	tfConfigMainKey = "main.tf"
	tfConfigVarsKey = "variables.tf"
	tfVarsKey       = "terraform.tfvars"
	tfStateKey      = "terraform.tfstate"

	terminationLogPath = corev1.TerminationMessagePathDefault
)

var (
	//tfConfigMainPath = path.Join(tfConfigDir, tfConfigMainKey)
	//tfConfigVarsPath = path.Join(tfConfigDir, tfConfigVarsKey)
	tfVarsPath     = path.Join(tfVarsDir, tfVarsKey)
	tfStateInPath  = path.Join(tfStateInDir, tfStateKey)
	tfStateOutPath = path.Join(tfStateOutDir, tfStateKey)
)

func (t *Terraformer) ensureTFDirs() error {
	log := t.stepLogger("ensureTFDirs")
	log.Info("ensuring terraform directories")

	for _, dir := range []string{
		tfConfigDir,
		tfVarsDir,
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
		allErrs = &multierror.Error{
			ErrorFormat: utils.NewErrorFormatFuncWithPrefix("failed to fetch terraform config"),
		}
		errCh = make(chan error)
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
	return fetchObject(ctx, log.WithValues("kind", "ConfigMap"), c, ns, name, &configMapStore{&corev1.ConfigMap{}}, optional, dir, dataKeys...)
}

func fetchSecret(ctx context.Context, log logr.Logger, c client.Client, ns, name string, optional bool, dir string, dataKeys ...string) error {
	return fetchObject(ctx, log.WithValues("kind", "Secret"), c, ns, name, &secretStore{&corev1.Secret{}}, optional, dir, dataKeys...)
}

func fetchObject(ctx context.Context, log logr.Logger, c client.Client, ns, name string, obj store, optional bool, dir string, dataKeys ...string) error {
	key := client.ObjectKey{Namespace: ns, Name: name}
	log = log.WithValues("object", key, "dir", dir)
	log.Info("fetching object")

	if err := c.Get(ctx, key, obj.Object()); err != nil {
		if apierrors.IsNotFound(err) && optional {
			log.V(1).Info("object not found but optional, cleaning up dir")

			for _, dataKey := range dataKeys {
				if err := os.Remove(filepath.Join(dir, dataKey)); err != nil {
					if !os.IsNotExist(err) {
						return err
					}
				}
			}

			return nil
		}
		return err
	}

	for _, dataKey := range dataKeys {
		if err := func() error {
			file, err := os.OpenFile(filepath.Join(dir, dataKey), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			defer file.Close()
			log.V(1).Info("copying contents into file", "dataKey", dataKey, "file", file.Name())

			_, err = io.Copy(file, obj.Read(dataKey))
			return err
		}(); err != nil {
			return err
		}
	}

	return nil
}

func (t *Terraformer) storeState(ctx context.Context) error {
	log := t.stepLogger("storeState")
	return storeSecret(ctx, log, t.client, t.config.Namespace, t.config.StateConfigMapName, tfStateOutDir, tfStateKey)
}

func storeSecret(ctx context.Context, log logr.Logger, c client.Client, ns, name string, dir string, dataKeys ...string) error {
	return storeObject(ctx, log.WithValues("kind", "Secret"), c,
		&secretStore{&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}}, dir, dataKeys...)
}

func storeObject(ctx context.Context, log logr.Logger, c client.Client, obj store, dir string, dataKeys ...string) error {
	key, err := client.ObjectKeyFromObject(obj.Object())
	if err != nil {
		return err
	}
	log = log.WithValues("object", key)

	// prepare individual mutate functions for each key to store, so we don't need to read every file again when retrying
	// because of a conflict
	mutateFuncs := make([]func(), len(dataKeys))

	for _, dataKey := range dataKeys {
		if err := func() error {
			file, err := os.Open(filepath.Join(dir, dataKey))
			if err != nil {
				return err
			}
			defer file.Close()
			log.V(1).Info("copying file contents into object", "dataKey", dataKey, "file", file.Name())

			var buf *bytes.Buffer
			_, err = io.Copy(buf, file)
			if err != nil {
				return err
			}

			mutateFuncs = append(mutateFuncs, func() {
				obj.Store(dataKey, buf)
			})
			return nil
		}(); err != nil {
			return err
		}
	}

	log.Info("updating object")
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, c, obj.Object(), func() error {
			for _, f := range mutateFuncs {
				f()
			}
			return nil
		})
		return err
	})
}

func (t *Terraformer) startFileWatcher(ctx context.Context, wg *sync.WaitGroup) error {
	wg.Add(1)

	go func() {
		wg.Done()
	}()

	return nil
}
