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
	"fmt"

	"github.com/gardener/gardener/pkg/utils/test"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

var _ = Describe("main", func() {
	var (
		revertVars func()

		exitCode *int
	)

	mockExit := func(code int) {
		if exitCode != nil {
			Fail("exit called twice")
		}
		exitCode = &code
	}

	BeforeEach(func() {
		revertVars = test.WithVars(
			&checkTerraformPresent, func() error { return nil },
			&executeTerraformer, func() error { return nil },
			&exit, mockExit,
		)

		exitCode = nil
	})

	AfterEach(func() {
		revertVars()
	})

	It("should exit successfully", func() {
		main()
		Expect(exitCode).To(BeNil())
	})

	It("should panic, if terraformer is not installed", func() {
		defer test.WithVar(&checkTerraformPresent, func() error {
			return fmt.Errorf("fake")
		})()

		Expect(func() {
			main()
		}).To(Panic())
	})

	It("should exit with 1, if terraformer fails", func() {
		defer test.WithVar(&executeTerraformer, func() error {
			return fmt.Errorf("fake")
		})()

		main()
		Expect(exitCode).To(PointTo(Equal(1)))
	})

	// It("should exit with specific exit code, if terraformer fails", func() {
	// 	defer test.WithVar(&executeTerraformer, func() error {
	// 		return utils.WithExitCode{Code: 5, Underlying: fmt.Errorf("fake")}
	// 	})()
	//
	// 	main()
	// 	Expect(exitCode).To(PointTo(Equal(5)))
	// })

})
