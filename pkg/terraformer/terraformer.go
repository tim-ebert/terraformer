// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/util/clock"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gardener/terraformer/pkg/utils"
)

var (
	// TerraformBinary is the name of the terraform binary, it allows to overwrite it for testing purposes
	TerraformBinary = "terraform"

	// allow redirecting output in tests
	Stdout, Stderr io.Writer = os.Stdout, os.Stderr

	// SignalNotify allows mocking signal.Notify in tests
	SignalNotify = signal.Notify
)

// NewDefaultTerraformer creates a new Terraformer with the default PathSet and logger.
func NewDefaultTerraformer(config *Config) (*Terraformer, error) {
	return NewTerraformer(config, runtimelog.Log, DefaultPaths().WithBaseDir(config.BaseDir), clock.RealClock{})
}

// NewTerraformer creates a new Terraformer with the given options.
func NewTerraformer(config *Config, log logr.Logger, paths *PathSet, clock clock.Clock) (*Terraformer, error) {
	t := &Terraformer{
		config: config,
		log:    log,
		paths:  paths,
		clock:  clock,

		StateUpdateQueue: workqueue.NewNamedRateLimitingQueue(workqueue.NewItemExponentialFailureRateLimiter(10*time.Millisecond, 5*time.Minute), "state-update"),
		// use buffered channel, to make sure we don't miss the signal
		FinalStateUpdateSucceeded: make(chan struct{}, 1),
	}

	c, err := client.New(config.RESTConfig, client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	t.client = c

	return t, nil
}

// InjectClient allows injecting a mock client for some test cases.
func (t *Terraformer) InjectClient(client client.Client) error {
	t.client = client
	return nil
}

// Run starts to terraformer execution with the given terraform command.
func (t *Terraformer) Run(command Command) error {
	if _, ok := SupportedCommands[command]; !ok {
		return fmt.Errorf("terraform command %q is not supported", command)
	}

	t.log.V(1).Info("executing terraformer with config", "config", t.config)

	return t.execute(command)
}

// execute is the main function of terraformer and puts all parts together (interacting with the terraform config and
// state resources on the kubernetes cluster, executing and watching terraform calls, delegating process signals and
// watching the state file).
func (t *Terraformer) execute(command Command) (rErr error) {
	sigCh := make(chan os.Signal, 1)
	SignalNotify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-sigCh:
			t.log.Info("interrupt received")
			cancel()
		case <-ctx.Done():
		}
	}()

	if err := t.EnsureTFDirs(); err != nil {
		return fmt.Errorf("failed to create needed directories: %w", err)
	}

	if err := t.FetchConfigAndState(ctx); err != nil {
		return err
	}

	shutdownWorker := t.StartStateUpdateWorker()
	defer shutdownWorker()

	// always try to update state ConfigMap one last time before exiting
	defer func() {
		err := t.TriggerAndWaitForFinalStateUpdate()
		if rErr == nil {
			// make sure, we don't exit with exit code 0, if we were unable to store the state
			rErr = err
		}
	}()

	// start file watcher for state file, that will continuously update state configmap
	// as soon as state file changes on disk
	shutdownFileWatcher, err := t.StartFileWatcher()
	if err != nil {
		return fmt.Errorf("failed to start state file watcher: %w", err)
	}

	// stop file watcher and wait for it to be finished
	defer shutdownFileWatcher()

	// initialize terraform plugins
	if err := t.executeTerraform(ctx, Init); err != nil {
		return fmt.Errorf("error executing terraform %s: %w", Init, err)
	}

	// execute main terraform command
	if err := t.executeTerraform(ctx, command); err != nil {
		return fmt.Errorf("error executing terraform %s: %w", command, err)
	}

	if command == Validate {
		if err := t.executeTerraform(ctx, Plan); err != nil {
			return fmt.Errorf("error executing terraform %s: %w", Plan, err)
		}
	}

	return nil
}

func (t *Terraformer) executeTerraform(ctx context.Context, command Command) error {
	log := t.stepLogger("executeTerraform")

	args := []string{string(command)}

	switch command {
	case Init:
		args = append(args, "-plugin-dir="+t.paths.ProvidersDir)
	case Plan:
		args = append(args, "-var-file="+t.paths.VarsPath, "-parallelism=4", "-detailed-exitcode", "-state="+t.paths.StatePath)
	case Apply:
		args = append(args, "-var-file="+t.paths.VarsPath, "-parallelism=4", "-auto-approve", "-state="+t.paths.StatePath)
	case Destroy:
		args = append(args, "-var-file="+t.paths.VarsPath, "-parallelism=4", "-auto-approve", "-state="+t.paths.StatePath)
	}

	args = append(args, t.paths.ConfigDir)

	log.Info("executing terraform", "command", command, "args", strings.Join(args[1:], " "))
	tfCmd := exec.Command(TerraformBinary, args...)
	// redirect all terraform output to stderr (same as logs)
	tfCmd.Stdout = Stderr
	tfCmd.Stderr = Stderr

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
		case <-ctx.Done():
			log.V(1).Info("relaying interrupt to terraform process")
			if err := tfCmd.Process.Signal(syscall.SIGINT); err != nil {
				log.Error(err, "failed to relay interrupt to terraform process")
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
