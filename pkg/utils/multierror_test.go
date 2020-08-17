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

package utils_test

import (
	"fmt"

	"github.com/hashicorp/go-multierror"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/gardener/terraformer/pkg/utils"
)

var _ = Describe("Multierror", func() {
	var (
		allErrs    *multierror.Error
		err1, err2 error
	)

	BeforeEach(func() {
		err1 = fmt.Errorf("error 1")
		err2 = fmt.Errorf("error 2")
	})

	Describe("#NewErrorFormatFuncWithPrefix", func() {
		BeforeEach(func() {
			allErrs = &multierror.Error{
				ErrorFormat: utils.NewErrorFormatFuncWithPrefix("prefix"),
			}
		})

		It("should format a multierror correctly if it contains 1 error", func() {
			allErrs.Errors = []error{err1}
			Expect(allErrs.Error()).To(Equal("prefix: 1 error occurred: error 1"))
		})
		It("should format a multierror correctly if it contains multiple errors", func() {
			allErrs.Errors = []error{err1, err2}
			Expect(allErrs.Error()).To(Equal("prefix: 2 errors occurred: [error 1, error 2]"))
		})
	})
})
