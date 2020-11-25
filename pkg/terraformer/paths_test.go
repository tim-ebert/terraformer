// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package terraformer_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/gardener/terraformer/pkg/terraformer"
)

var _ = Describe("Terraformer Paths", func() {
	Describe("#DefaultPaths", func() {
		It("should return the correct defaults", func() {
			Expect(terraformer.DefaultPaths()).To(Equal(&terraformer.PathSet{
				ConfigDir:    "/tf",
				VarsDir:      "/tfvars",
				StateDir:     "/tfstate",
				ProvidersDir: "/terraform-providers",
				VarsPath:     "/tfvars/terraform.tfvars",
				StatePath:    "/tfstate/terraform.tfstate",
			}))
		})
	})

	Describe("#WithBaseDir", func() {
		It("should not mutate ", func() {
			paths := &terraformer.PathSet{ProvidersDir: "/foo-providers"}
			Expect(paths.WithBaseDir("/root").ProvidersDir).To(Equal(paths.ProvidersDir))
		})
	})
})
