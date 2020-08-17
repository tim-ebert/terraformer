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
	"sync"

	"github.com/pkg/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gardener/terraformer/pkg/utils"
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

	intCh, killCh := setupSignalChannels()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-intCh:
		case <-killCh:
		}
		cancel()
	}()

	if err := t.ensureTFDirs(); err != nil {
		return fmt.Errorf("failed to create needed directories: %w", err)
	}

	if err := t.fetchConfig(ctx); err != nil {
		return err
	}

	fileWatcherCtx, fileWatcherCancel := context.WithCancel(ctx)
	defer fileWatcherCancel()

	var fileWatcherWaitGroup sync.WaitGroup
	if err := t.startFileWatcher(fileWatcherCtx, &fileWatcherWaitGroup); err != nil {
		return fmt.Errorf("failed to start state file watcher: %w", err)
	}

	if err := t.executeTerraform(command, intCh, killCh); err != nil {
		return fmt.Errorf("error executing terraform %s: %w", command, err)
	}

	if command == Validate {
		if err := t.executeTerraform(Plan, intCh, killCh); err != nil {
			return fmt.Errorf("error executing terraform %s: %w", Plan, err)
		}
	}

	// stop file watcher and wait for it to be done before storing state explicitly
	fileWatcherCancel()
	fileWatcherWaitGroup.Wait()

	// TODO: ctx might already be cancelled
	if err := t.storeState(ctx); err != nil {
		return errors.Wrap(err, "failed to store terraform state")
	}

	return nil
}

func (t *Terraformer) executeTerraform(command Command, intCh, killCh <-chan struct{}) error {
	// TODO: figure out, what to do with these commands:
	//# required to initialize the provider plugins
	//terraform init -plugin-dir="$DIR_PROVIDERS" "$DIR_CONFIGURATION"
	//# workaround for `terraform init`; required to make `terraform validate` work (plugin_path file ignored?)
	//cp -r "$DIR_PROVIDERS"/* "$DIR_PLUGIN_BINARIES"/.

	log := t.stepLogger("executeTerraform")

	args := []string{string(command), "-var-file=" + tfVarsPath}

	switch command {
	case Plan:
		args = append(args, "-parallelism=4",
			"-detailed-exitcode", "-state="+tfStateInPath)
	case Apply:
		args = append(args, "-parallelism=4", "-auto-approve",
			"-state="+tfStateInPath, "-state-out="+tfStateOutPath)
	case Destroy:
		args = append(args, "-parallelism=4", "-auto-approve",
			"-state="+tfStateInPath, "-state-out="+tfStateOutPath)
	}

	args = append(args, tfConfigDir)

	log.Info("executing terraform with args", "args", args)
	tfCmd := exec.Command("terraform", args...)
	tfCmd.Stdout = os.Stdout
	tfCmd.Stderr = os.Stderr

	if err := tfCmd.Start(); err != nil {
		return err
	}

	doneCh := make(chan struct{})
	defer close(doneCh)

	// setup signal handler relaying signals to terraform process
	go func() {
		select {
		case <-intCh:
			if err := tfCmd.Process.Signal(os.Interrupt); err != nil {
				fmt.Printf("failed to relay SIGINT to terraform process: %v\n", err)
			}
		case <-killCh:
			if err := tfCmd.Process.Signal(os.Kill); err != nil {
				fmt.Printf("failed to relay SIGKILL to terraform process: %v\n", err)
			}
		case <-doneCh:
		}
	}()

	if err := tfCmd.Wait(); err != nil {
		return utils.WithExitCode{Code: tfCmd.ProcessState.ExitCode(), Underlying: err}
	}

	return nil
}

func setupSignalChannels() (intCh, killCh <-chan struct{}) {
	intOutCh, killOutCh := make(chan struct{}), make(chan struct{})
	sigintCh, sigkillCh := make(chan os.Signal, 1), make(chan os.Signal, 1)
	signal.Notify(sigintCh, os.Interrupt)
	signal.Notify(sigkillCh, os.Kill)

	go func() {
		<-sigintCh
		fmt.Println("SIGINT received")
		close(intOutCh)
	}()

	go func() {
		<-sigkillCh
		fmt.Println("SIGKILL received")
		close(killOutCh)
	}()

	return intOutCh, killOutCh
}
