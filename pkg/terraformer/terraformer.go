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
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gardener/terraformer/pkg/utils"
)

const (
	continuousStateUpdateTimeout = 5 * time.Minute
	finalStateUpdateTimeout      = 5 * time.Minute
)

// NewTerraformer creates a new Terraformer
func NewTerraformer(config *Config) *Terraformer {
	return &Terraformer{log: runtimelog.Log, config: config}
}

func (t *Terraformer) Run(command Command) error {
	if _, ok := SupportedCommands[command]; !ok {
		return fmt.Errorf("terraform command %q is not supported", command)
	}

	return t.execute(command)
}

func (t *Terraformer) execute(command Command) error {
	if c, err := client.New(t.config.RESTConfig, client.Options{}); err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	} else {
		t.client = c
	}

	intCh, killCh := setupSignalChannels(t.log)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-intCh:
		case <-killCh:
		case <-ctx.Done():
			return
		}
		cancel()
	}()

	if err := t.ensureTFDirs(); err != nil {
		return fmt.Errorf("failed to create needed directories: %w", err)
	}

	if err := t.fetchConfig(ctx); err != nil {
		return err
	}

	var fileWatcherWaitGroup sync.WaitGroup
	// file watcher should run in background and should only be cancelled when this function returns, i.e. when any
	// running terraform processes have finished
	fileWatcherCtx, fileWatcherCancel := context.WithCancel(context.Background())

	// always try to store state once again before exiting
	defer func() {
		// stop file watcher and wait for it to be done before storing state explicitly to avoid conflicts
		fileWatcherCancel()
		fileWatcherWaitGroup.Wait()

		log := t.stepLogger("storeState")
		log.Info("storing state before exiting")

		// run storeState in background, ctx might have been cancelled already
		storeCtx, storeCancel := context.WithTimeout(context.Background(), finalStateUpdateTimeout)
		defer storeCancel()

		if err := t.storeState(storeCtx, log); err != nil {
			log.Error(err, "failed to store terraform state")
		}
		log.Info("successfully stored terraform state")
	}()

	// continuously update state configmap as soon as state file changes on disk
	if err := t.startFileWatcher(fileWatcherCtx, &fileWatcherWaitGroup); err != nil {
		return fmt.Errorf("failed to start state file watcher: %w", err)
	}

	// initialize terraform plugins
	if err := t.executeTerraform(Init, intCh, killCh); err != nil {
		return fmt.Errorf("error executing terraform %s: %w", command, err)
	}

	// execute main terraform command
	if err := t.executeTerraform(command, intCh, killCh); err != nil {
		return fmt.Errorf("error executing terraform %s: %w", command, err)
	}

	if command == Validate {
		if err := t.executeTerraform(Plan, intCh, killCh); err != nil {
			return fmt.Errorf("error executing terraform %s: %w", Plan, err)
		}
	}

	return nil
}

func (t *Terraformer) executeTerraform(command Command, intCh, killCh <-chan struct{}) error {
	// TODO: figure out, what to do with these commands:
	//# workaround for `terraform init`; required to make `terraform validate` work (plugin_path file ignored?)
	//cp -r "$DIR_PROVIDERS"/* "$DIR_PLUGIN_BINARIES"/.

	log := t.stepLogger("executeTerraform")

	args := []string{string(command)}

	switch command {
	case Init:
		args = append(args, "-plugin-dir="+tfProvidersDir)
	case Plan:
		args = append(args, "-var-file="+tfVarsPath, "-parallelism=4", "-detailed-exitcode", "-state="+tfStatePath)
	case Apply:
		args = append(args, "-var-file="+tfVarsPath, "-parallelism=4", "-auto-approve", "-state="+tfStatePath)
	case Destroy:
		args = append(args, "-var-file="+tfVarsPath, "-parallelism=4", "-auto-approve", "-state="+tfStatePath)
	}

	args = append(args, tfConfigDir)

	log.Info("executing terraform", "command", command, "args", strings.Join(args[1:], " "))
	tfCmd := exec.Command("terraform", args...)
	tfCmd.Stdout = os.Stdout
	tfCmd.Stderr = os.Stderr

	if err := tfCmd.Start(); err != nil {
		return err
	}

	var wg sync.WaitGroup
	wg.Add(1)
	// wait for signal handler goroutine to finish properly before returning
	defer wg.Wait()

	doneCh := make(chan struct{})
	defer close(doneCh)

	// setup signal handler relaying signals to terraform process
	go func() {
		defer wg.Done()
		select {
		case <-doneCh:
			return
		case <-intCh:
			log.V(1).Info("relaying SIGINT to terraform process")
			if err := tfCmd.Process.Signal(os.Interrupt); err != nil {
				log.Error(err, "failed to relay SIGINT to terraform process")
			}
		case <-killCh:
			log.V(1).Info("relaying SIGKILL to terraform process")
			if err := tfCmd.Process.Signal(os.Kill); err != nil {
				log.Error(err, "failed to relay SIGKILL to terraform process")
			}
		}
	}()

	if err := tfCmd.Wait(); err != nil {
		log.Error(err, "terraform process finished with error", "command", command)
		return utils.WithExitCode{Code: tfCmd.ProcessState.ExitCode(), Underlying: err}
	}

	log.Info("terraform process finished successfully", "command", command)
	return nil
}

func setupSignalChannels(log logr.Logger) (intCh, killCh <-chan struct{}) {
	intOutCh, killOutCh := make(chan struct{}), make(chan struct{})
	sigintCh, sigkillCh := make(chan os.Signal, 1), make(chan os.Signal, 1)
	signal.Notify(sigintCh, os.Interrupt)
	signal.Notify(sigkillCh, os.Kill)

	go func() {
		<-sigintCh
		log.Info("SIGINT received")
		close(intOutCh)
	}()

	go func() {
		<-sigkillCh
		log.Info("SIGKILL received")
		close(killOutCh)
	}()

	return intOutCh, killOutCh
}
