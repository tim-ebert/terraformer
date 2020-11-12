// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"context"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestObjects models a set of API objects used in tests
type TestObjects struct {
	ctx    context.Context
	client client.Client

	Namespace              string
	ConfigurationConfigMap *corev1.ConfigMap
	StateConfigMap         *corev1.ConfigMap
	VariablesSecret        *corev1.Secret
}

// PrepareTestObjects creates a default set of needed API objects for tests
func PrepareTestObjects(ctx context.Context, c client.Client) *TestObjects {
	o := &TestObjects{ctx: ctx, client: c}

	// create test namespace
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "tf-test-"}}
	Expect(o.client.Create(ctx, ns)).To(Succeed())
	Expect(ns.Name).NotTo(BeEmpty())
	o.Namespace = ns.Name

	var handle CleanupActionHandle
	handle = AddCleanupAction(func() {
		Expect(client.IgnoreNotFound(o.client.Delete(ctx, ns))).To(Succeed())
		RemoveCleanupAction(handle)
	})

	// create configuration ConfigMap
	o.ConfigurationConfigMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "tf-config", Namespace: o.Namespace},
		Data: map[string]string{
			ConfigMainKey: `resource "null_resource" "foo" {
	triggers = {
    some_var = var.SOME_VAR
  }
}`,
			ConfigVarsKey: `variable "SOME_VAR" {
	description = "Some variable"
	type        = string
}`,
		},
	}
	err := o.client.Create(ctx, o.ConfigurationConfigMap)
	Expect(err).NotTo(HaveOccurred())

	// create state ConfigMap
	o.StateConfigMap = &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "tf-state", Namespace: o.Namespace},
		Data: map[string]string{
			StateKey: `some state`,
		},
	}
	err = o.client.Create(ctx, o.StateConfigMap)
	Expect(err).NotTo(HaveOccurred())

	// create variables Secret
	o.VariablesSecret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "tf-vars", Namespace: o.Namespace},
		Data: map[string][]byte{
			VarsKey: []byte(`SOME_VAR = "fancy"`),
		},
	}
	err = o.client.Create(ctx, o.VariablesSecret)
	Expect(err).NotTo(HaveOccurred())

	return o
}

// Refresh retrieves a fresh copy of the objects from the API server, so that tests can make assertions on them.
func (o *TestObjects) Refresh() {
	Expect(o.client.Get(o.ctx, objectKeyFromObject(o.ConfigurationConfigMap), o.ConfigurationConfigMap)).To(Succeed())
	Expect(o.client.Get(o.ctx, objectKeyFromObject(o.StateConfigMap), o.StateConfigMap)).To(Succeed())
	Expect(o.client.Get(o.ctx, objectKeyFromObject(o.VariablesSecret), o.VariablesSecret)).To(Succeed())
}

func objectKeyFromObject(obj metav1.Object) client.ObjectKey {
	return client.ObjectKey{Namespace: obj.GetNamespace(), Name: obj.GetName()}
}
