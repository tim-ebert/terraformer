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

package app

import (
	"errors"
	"flag"
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"
	logzap "sigs.k8s.io/controller-runtime/pkg/log/zap"

	terraformercmd "github.com/gardener/terraformer/pkg/cmd"
	"github.com/gardener/terraformer/pkg/terraformer"
	"github.com/gardener/terraformer/pkg/version"
)

// NewTerraformerCommand creates a new terraformer cobra command
func NewTerraformerCommand() *cobra.Command {
	var (
		zapOpts = &logzap.Options{}
		tfOpts  = terraformercmd.NewOptions()
	)

	// root command
	cmd := &cobra.Command{
		Use:   "terraformer",
		Short: "terraformer executes terraform commands inside a Kubernetes cluster",
		Long: `terraformer executes terraform commands inside a Kubernetes cluster and handles Pod lifecycle event (e.g. Signal handling).
It reads and stores terraform config and state from/to Kubernetes resources (ConfigMaps and Secrets).`,
		Version: version.Version,
		Hidden:  true,

		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			runtimelog.SetLogger(logzap.New(
				// use configuration passed via flags
				logzap.UseFlagOptions(zapOpts),
				// and overwrite some stuff
				func(o *logzap.Options) {
					if !o.Development {
						encCfg := zap.NewProductionEncoderConfig()
						// overwrite time encoding to human readable format for production logs
						encCfg.EncodeTime = zapcore.ISO8601TimeEncoder
						o.Encoder = zapcore.NewJSONEncoder(encCfg)
					}

					// don't print stacktrace for warning level logs
					o.StacktraceLevel = zapcore.ErrorLevel
				},
			))
		},

		RunE: func(cmd *cobra.Command, args []string) error {
			// root command called without any terraform command is invalid and should exit with error code
			return errors.New("no command was specified")
		},
	}
	cmd.SetUsageTemplate(cosmeticUsageTemplate)

	// setup a subcommand for every supported terraform command
	for command := range terraformer.SupportedCommands {
		addSubcommand(cmd, command, tfOpts)
	}

	// setup flags
	tfOpts.AddFlags(cmd.PersistentFlags())
	zapOpts.BindFlags(flag.CommandLine)
	cmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)

	return cmd
}

func addSubcommand(cmd *cobra.Command, command terraformer.Command, opts *terraformercmd.Options) {
	cmd.AddCommand(&cobra.Command{
		Use:   string(command),
		Short: fmt.Sprintf("execute `terraform %s`", command),
		Long:  fmt.Sprintf("terraformer %s executes the terraform %s command with the given configuration", command, command),
		Args:  cobra.NoArgs,

		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Complete(); err != nil {
				return err
			}

			// don't output usage on further errors raised during terraform execution
			cmd.SilenceUsage = true
			// further errors will be logged properly, don't duplicate
			cmd.SilenceErrors = true

			return terraformer.NewTerraformer(opts.Completed()).Run(command)
		},
	})
}

// custom usage template:
// - exclude `terraformer [flags]` if command is hidden (e.g. root command)
// - indicate [flags] after `terraformer [command]` (for subcommands)
const cosmeticUsageTemplate = `Usage:{{if and .Runnable (not .Hidden)}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command] [flags]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Available Commands:{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`
