/*
Copyright 2024 The Kairos CAPI Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
implied. See the License for the specific language governing
permissions and limitations under the License.
*/

package envtest

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	bootstrapv1beta2 "github.com/wrkode/kairos-capi/api/bootstrap/v1beta2"
	controlplanev1beta2 "github.com/wrkode/kairos-capi/api/controlplane/v1beta2"
	"github.com/wrkode/kairos-capi/internal/controllers/bootstrap"
	"github.com/wrkode/kairos-capi/internal/controllers/controlplane"
)

func TestControlPlaneIntegration(t *testing.T) {
	g := NewWithT(t)

	// Setup envtest environment
	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			"../../config/crd/bases",
		},
		ErrorIfCRDPathMissing: false,
	}

	cfg, err := testEnv.Start()
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(cfg).NotTo(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	// Create scheme
	scheme := runtime.NewScheme()
	g.Expect(corev1.AddToScheme(scheme)).To(Succeed())
	g.Expect(clusterv1.AddToScheme(scheme)).To(Succeed())
	g.Expect(bootstrapv1beta2.AddToScheme(scheme)).To(Succeed())
	g.Expect(controlplanev1beta2.AddToScheme(scheme)).To(Succeed())

	// Create manager
	mgr, err := manager.New(cfg, manager.Options{
		Scheme: scheme,
		Logger: log.Log,
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Setup controllers
	bootstrapReconciler := &bootstrap.KairosConfigReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	g.Expect(bootstrapReconciler.SetupWithManager(mgr)).To(Succeed())

	controlPlaneReconciler := &controlplane.KairosControlPlaneReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	g.Expect(controlPlaneReconciler.SetupWithManager(mgr)).To(Succeed())

	// Start manager
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		g.Expect(mgr.Start(ctx)).To(Succeed())
	}()

	// Wait for manager to be ready
	g.Eventually(func() bool {
		return mgr.GetCache().WaitForCacheSync(ctx)
	}, 10*time.Second).Should(BeTrue())

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}
	g.Expect(mgr.GetClient().Create(ctx, ns)).To(Succeed())

	// Create Cluster
	cluster := &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-namespace",
		},
		Spec: clusterv1.ClusterSpec{
			InfrastructureRef: &corev1.ObjectReference{
				APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
				Kind:       "DockerCluster",
				Name:       "test-cluster",
			},
			ControlPlaneRef: &corev1.ObjectReference{
				APIVersion: controlplanev1beta2.GroupVersion.String(),
				Kind:       "KairosControlPlane",
				Name:       "test-kcp",
				Namespace:  "test-namespace",
			},
		},
	}
	g.Expect(mgr.GetClient().Create(ctx, cluster)).To(Succeed())

	// Create KairosConfigTemplate
	configTemplate := &bootstrapv1beta2.KairosConfigTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-config-template",
			Namespace: "test-namespace",
		},
		Spec: bootstrapv1beta2.KairosConfigTemplateSpec{
			Template: bootstrapv1beta2.KairosConfigTemplateResource{
				Spec: bootstrapv1beta2.KairosConfigSpec{
					Role:              "control-plane",
					Distribution:      "k0s",
					KubernetesVersion: "v1.30.0+k0s.0",
					UserName:          "kairos",
					UserPassword:      "kairos",
					UserGroups:        []string{"admin"},
				},
			},
		},
	}
	g.Expect(mgr.GetClient().Create(ctx, configTemplate)).To(Succeed())

	// Create KairosControlPlane with replicas=1 (single-node)
	replicas := int32(1)
	kcp := &controlplanev1beta2.KairosControlPlane{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kcp",
			Namespace: "test-namespace",
		},
		Spec: controlplanev1beta2.KairosControlPlaneSpec{
			Replicas: &replicas,
			Version:  "v1.30.0+k0s.0",
			MachineTemplate: controlplanev1beta2.KairosControlPlaneMachineTemplate{
				InfrastructureRef: corev1.ObjectReference{
					APIVersion: "infrastructure.cluster.x-k8s.io/v1beta1",
					Kind:       "DockerMachineTemplate",
					Name:       "test-infra-template",
					Namespace:  "test-namespace",
				},
			},
			KairosConfigTemplate: controlplanev1beta2.KairosConfigTemplateReference{
				Name: "test-config-template",
			},
		},
	}
	g.Expect(mgr.GetClient().Create(ctx, kcp)).To(Succeed())

	// Wait for Machine to be created
	var machineName string
	g.Eventually(func() bool {
		machines := &clusterv1.MachineList{}
		if err := mgr.GetClient().List(ctx, machines, client.InNamespace("test-namespace")); err != nil {
			return false
		}
		if len(machines.Items) > 0 {
			machineName = machines.Items[0].Name
			return true
		}
		return false
	}, 30*time.Second, 1*time.Second).Should(BeTrue())

	// Verify Machine has correct labels
	machine := &clusterv1.Machine{}
	g.Eventually(func() bool {
		return mgr.GetClient().Get(ctx, types.NamespacedName{
			Name:      machineName,
			Namespace: "test-namespace",
		}, machine) == nil
	}, 10*time.Second).Should(BeTrue())

	g.Expect(machine.Labels).To(HaveKey(clusterv1.ClusterNameLabel))
	g.Expect(machine.Labels).To(HaveKey(clusterv1.MachineControlPlaneLabel))

	// Verify KairosConfig was created with SingleNode=true
	var kairosConfigName string
	if machine.Spec.Bootstrap.ConfigRef != nil {
		kairosConfigName = machine.Spec.Bootstrap.ConfigRef.Name
	}
	g.Expect(kairosConfigName).NotTo(BeEmpty())

	kairosConfig := &bootstrapv1beta2.KairosConfig{}
	g.Eventually(func() bool {
		return mgr.GetClient().Get(ctx, types.NamespacedName{
			Name:      kairosConfigName,
			Namespace: "test-namespace",
		}, kairosConfig) == nil
	}, 10*time.Second).Should(BeTrue())

	g.Expect(kairosConfig.Spec.SingleNode).To(BeTrue())
	g.Expect(kairosConfig.Spec.Role).To(Equal("control-plane"))
}

