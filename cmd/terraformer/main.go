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

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	runtimelog "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/gardener/terraformer/cmd/terraformer/app"
	"github.com/gardener/terraformer/pkg/utils"
)

func main() {
	if err := exec.Command("which", "terraform").Run(); err != nil {
		panic("terraform is not installed or not executable. cannot start terraformer.")
	}

	if err := app.NewTerraformerCommand().Execute(); err != nil {
		if log := runtimelog.Log; log.Enabled() {
			log.Error(err, "error running terraformer")
		} else {
			fmt.Println(err)
		}

		// set exit code from terraform
		withExitCode := &utils.WithExitCode{}
		if errors.As(err, withExitCode) {
			if exitCode := withExitCode.ExitCode(); exitCode > 0 {
				os.Exit(exitCode)
			}
		}
		os.Exit(1)
	}
}
