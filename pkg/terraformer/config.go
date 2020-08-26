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
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/fsnotify/fsnotify"
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
	tfStateDir     = "/tfstate"
	tfProvidersDir = "/terraform-providers"

	// TODO: still needed?
	tfPluginsDir = ".terraform/plugins/linux_amd64"

	tfConfigMainKey = "main.tf"
	tfConfigVarsKey = "variables.tf"
	tfVarsKey       = "terraform.tfvars"
	tfStateKey      = "terraform.tfstate"

	terminationLogPath = corev1.TerminationMessagePathDefault
)

var (
	//tfConfigMainPath = path.Join(tfConfigDir, tfConfigMainKey)
	//tfConfigVarsPath = path.Join(tfConfigDir, tfConfigVarsKey)
	tfVarsPath  = path.Join(tfVarsDir, tfVarsKey)
	tfStatePath = path.Join(tfStateDir, tfStateKey)
)

func (t *Terraformer) ensureTFDirs() error {
	log := t.stepLogger("ensureTFDirs")
	log.Info("ensuring terraform directories")

	for _, dir := range []string{
		tfConfigDir,
		tfVarsDir,
		tfStateDir,
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
			tfStateDir, tfStateKey,
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
	return fetchObject(ctx, log, c, "ConfigMap", ns, name, &configMapStore{&corev1.ConfigMap{}}, optional, dir, dataKeys...)
}

func fetchSecret(ctx context.Context, log logr.Logger, c client.Client, ns, name string, optional bool, dir string, dataKeys ...string) error {
	return fetchObject(ctx, log, c, "Secret", ns, name, &secretStore{&corev1.Secret{}}, optional, dir, dataKeys...)
}

func fetchObject(ctx context.Context, log logr.Logger, c client.Client, kind, ns, name string, obj store, optional bool, dir string, dataKeys ...string) error {
	key := client.ObjectKey{Namespace: ns, Name: name}
	log = log.WithValues("kind", kind, "object", key, "dir", dir)
	log.V(1).Info("fetching object")

	if err := c.Get(ctx, key, obj.Object()); err != nil {
		if apierrors.IsNotFound(err) && optional {
			log.V(1).Info("object not found but optional")

			for _, dataKey := range dataKeys {
				filePath := filepath.Join(dir, dataKey)
				log.V(1).Info("creating empty file / truncating existing file", "dataKey", dataKey, "file", filePath)
				file, err := os.OpenFile(filePath, os.O_CREATE|os.O_TRUNC, 0644)
				if err != nil {
					return err
				}
				file.Close()
			}

			return nil
		}
		return err
	}

	for _, dataKey := range dataKeys {
		if err := func() error {
			filePath := filepath.Join(dir, dataKey)
			log.V(1).Info("copying contents into file", "dataKey", dataKey, "file", filePath)

			reader, err := obj.Read(dataKey)
			if err != nil {
				return fmt.Errorf("failed reading from %s %q: %w", kind, key, err)
			}

			file, err := os.OpenFile(filePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			defer file.Close()

			_, err = io.Copy(file, reader)
			return err
		}(); err != nil {
			return err
		}
	}

	return nil
}

func (t *Terraformer) storeState(ctx context.Context, log logr.Logger) error {
	return storeConfigMap(ctx, log, t.client, t.config.Namespace, t.config.StateConfigMapName, tfStateDir, tfStateKey)
}

func storeConfigMap(ctx context.Context, log logr.Logger, c client.Client, ns, name string, dir string, dataKeys ...string) error {
	return storeObject(ctx, log.WithValues("kind", "ConfigMap"), c,
		&configMapStore{&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}}, dir, dataKeys...)
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
	mutateFuncs := make([]func(), 0, len(dataKeys))

	for _, dataKey := range dataKeys {
		if err := func() error {
			file, err := os.Open(filepath.Join(dir, dataKey))
			if err != nil {
				return err
			}
			defer file.Close()
			log.V(1).Info("copying file content into object", "dataKey", dataKey, "file", file.Name())

			buf := &bytes.Buffer{}
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

	log.V(1).Info("updating object")
	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		_, err := controllerutil.CreateOrUpdate(ctx, c, obj.Object(), func() error {
			for _, f := range mutateFuncs {
				f()
			}
			return nil
		})
		return err
	}); err != nil {
		return err
	}
	log.V(1).Info("successfully updated object")
	return nil
}

func (t *Terraformer) startFileWatcher(ctx context.Context, wg *sync.WaitGroup) error {
	log := t.stepLogger("fileWatcher")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	go func() {
		wg.Add(1)
		defer wg.Done()

		for {
			select {
			case <-ctx.Done():
				log.V(1).Info("stopping file watcher")
				_ = watcher.Close()
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				fileLog := log.WithValues("file", event.Name)
				fileLog.V(1).Info("received event for file", "op", event.Op.String())

				if event.Name == tfStatePath && event.Op&fsnotify.Write == fsnotify.Write {
					fileLog.V(1).Info("trigger storing state")

					if err := func() error {
						// run storeState in background, ctx might have been cancelled already
						storeCtx, storeCancel := context.WithTimeout(context.Background(), continuousStateUpdateTimeout)
						defer storeCancel()

						return t.storeState(storeCtx, log)
					}(); err != nil {
						log.Error(err, "failed storing state after state file update")
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Error(err, "error while watching state file")
			}
		}
	}()

	log.Info("starting file watcher for state file", "file", tfStatePath)
	if err := watcher.Add(tfStatePath); err != nil {
		return err
	}

	return nil
}
