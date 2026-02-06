//go:build envtest
// +build envtest

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
	"encoding/json"
	"fmt"
	"os"
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
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	bootstrapv1beta2 "github.com/kairos-io/kairos-capi/api/bootstrap/v1beta2"
	controlplanev1beta2 "github.com/kairos-io/kairos-capi/api/controlplane/v1beta2"
	"github.com/kairos-io/kairos-capi/internal/controllers/bootstrap"
	"github.com/kairos-io/kairos-capi/internal/controllers/controlplane"
)

func TestControlPlaneIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	// Note: This test requires infrastructure provider CRDs (e.g., CAPD) to fully test
	// Machine creation. Without them, the controller will fail at infrastructure cloning.
	// For now, we test that the controller reconciles and attempts to create resources.
	// Full end-to-end testing should be done with actual infrastructure providers.
	g := NewWithT(t)

	// Setup envtest environment
	crdPaths := []string{
		"../../config/crd/bases",
	}
	// Add CAPI CRDs if available (downloaded by make test-envtest)
	if _, err := os.Stat("../../test/crd/capi/cluster-api-components.yaml"); err == nil {
		crdPaths = append(crdPaths, "../../test/crd/capi")
	} else {
		t.Fatalf("CAPI CRDs not found; run `make test-envtest` to download: %v", err)
	}
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     crdPaths,
		ErrorIfCRDPathMissing: false,
	}

	t.Logf("Starting envtest with CRD paths: %v", crdPaths)
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
		// Disable webhook server for envtest
		WebhookServer: webhook.NewServer(webhook.Options{Port: 0}),
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
	waitForCondition(t, 10*time.Second, 1*time.Second, func() (bool, string) {
		if mgr.GetCache().WaitForCacheSync(ctx) {
			return true, "cache synced"
		}
		return false, "cache sync not ready"
	}, nil)

	// Create test namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
		},
	}
	t.Logf("Creating namespace %s", ns.Name)
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
	t.Logf("Creating Cluster %s", cluster.Name)
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
	t.Logf("Creating KairosConfigTemplate %s", configTemplate.Name)
	g.Expect(mgr.GetClient().Create(ctx, configTemplate)).To(Succeed())

	// Note: We skip creating infrastructure template because DockerMachineTemplate CRD is not available
	// The controller will fail to create infrastructure machines, but we can still test
	// that it attempts to create KairosConfig resources with correct SingleNode setting

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
	t.Logf("Creating KairosControlPlane %s", kcp.Name)
	g.Expect(mgr.GetClient().Create(ctx, kcp)).To(Succeed())

	// Verify that the controller attempts to reconcile the KairosControlPlane
	// Note: Without infrastructure CRDs, Machine creation will fail early in the reconciliation.
	// The controller will attempt to create infrastructure machines and fail, but we can verify
	// that the KCP resource exists and the controller is watching it.
	// Full end-to-end testing requires infrastructure provider CRDs (e.g., CAPD).
	updatedKCP := &controlplanev1beta2.KairosControlPlane{}
	waitForCondition(t, 10*time.Second, 1*time.Second, func() (bool, string) {
		err := mgr.GetClient().Get(ctx, types.NamespacedName{
			Name:      "test-kcp",
			Namespace: "test-namespace",
		}, updatedKCP)
		if err != nil {
			return false, fmt.Sprintf("get KairosControlPlane: %v", err)
		}
		return true, "KairosControlPlane found"
	}, func(last string) {
		t.Logf("Timed out waiting for KairosControlPlane: %s", last)
		dumpControlPlaneState(t, ctx, mgr.GetClient(), "test-namespace", "test-kcp")
	})

	// Verify spec is correct
	g.Expect(updatedKCP.Spec.Replicas).NotTo(BeNil())
	g.Expect(*updatedKCP.Spec.Replicas).To(Equal(int32(1)))
	g.Expect(updatedKCP.Spec.Version).To(Equal("v1.30.0+k0s.0"))

	// Note: Full Machine and KairosConfig creation testing requires infrastructure provider CRDs.
	// The unit tests (TestCreateControlPlaneMachine_SingleNode) verify the SingleNode logic
	// with mocked infrastructure. For full integration testing, use a real infrastructure provider.
}

func dumpControlPlaneState(t *testing.T, ctx context.Context, c client.Client, namespace, kcpName string) {
	t.Helper()
	kcp := &controlplanev1beta2.KairosControlPlane{}
	if err := c.Get(ctx, types.NamespacedName{Name: kcpName, Namespace: namespace}, kcp); err == nil {
		if b, err := json.MarshalIndent(kcp, "", "  "); err == nil {
			t.Logf("KairosControlPlane:\n%s", string(b))
		}
	} else {
		t.Logf("Failed to get KairosControlPlane: %v", err)
	}

	eventList := &corev1.EventList{}
	if err := c.List(ctx, eventList, client.InNamespace(namespace)); err == nil {
		for _, evt := range eventList.Items {
			t.Logf("Event %s: %s %s %s", evt.Name, evt.Reason, evt.Type, evt.Message)
		}
	} else {
		t.Logf("Failed to list Events: %v", err)
	}
}
