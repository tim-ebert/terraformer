// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package version

var (
	// Version contains the overall codebase version. It's for detecting
	// what code a binary was built from.
	// It is injecting via -ldflags during build time.
	Version = "v0.0.0-dev"
)
