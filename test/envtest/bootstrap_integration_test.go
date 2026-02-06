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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	bootstrapv1beta2 "github.com/kairos-io/kairos-capi/api/bootstrap/v1beta2"
	"github.com/kairos-io/kairos-capi/internal/controllers/bootstrap"
)

func TestBootstrapIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	log.SetLogger(zap.New(zap.UseDevMode(true)))
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
		ErrorIfCRDPathMissing: false, // Allow missing CAPI CRDs for now
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

	// Create manager
	mgr, err := manager.New(cfg, manager.Options{
		Scheme: scheme,
		Logger: log.Log,
		// Disable webhook server for envtest
		WebhookServer: webhook.NewServer(webhook.Options{Port: 0}),
	})
	g.Expect(err).NotTo(HaveOccurred())

	// Setup bootstrap controller
	bootstrapReconciler := &bootstrap.KairosConfigReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}
	g.Expect(bootstrapReconciler.SetupWithManager(mgr)).To(Succeed())

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
		},
	}
	t.Logf("Creating Cluster %s", cluster.Name)
	g.Expect(mgr.GetClient().Create(ctx, cluster)).To(Succeed())

	// Create Machine
	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine",
			Namespace: "test-namespace",
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: "test-cluster",
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: "test-cluster",
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: &corev1.ObjectReference{
					APIVersion: bootstrapv1beta2.GroupVersion.String(),
					Kind:       "KairosConfig",
					Name:       "test-kairos-config",
					Namespace:  "test-namespace",
				},
			},
		},
	}
	t.Logf("Creating Machine %s", machine.Name)
	g.Expect(mgr.GetClient().Create(ctx, machine)).To(Succeed())
	apiReader := mgr.GetAPIReader()
	waitForCondition(t, 10*time.Second, 1*time.Second, func() (bool, string) {
		if err := apiReader.Get(ctx, types.NamespacedName{Name: machine.Name, Namespace: machine.Namespace}, machine); err != nil {
			return false, fmt.Sprintf("get Machine: %v", err)
		}
		if machine.UID == "" {
			return false, "machine UID not set yet"
		}
		return true, fmt.Sprintf("machine UID=%s", machine.UID)
	}, func(last string) {
		t.Logf("Timed out waiting for Machine UID: %s", last)
		dumpBootstrapState(t, ctx, mgr.GetClient(), "test-namespace", "test-kairos-config")
	})
	waitForCondition(t, 10*time.Second, 1*time.Second, func() (bool, string) {
		if err := mgr.GetClient().Get(ctx, types.NamespacedName{Name: machine.Name, Namespace: machine.Namespace}, &clusterv1.Machine{}); err != nil {
			return false, fmt.Sprintf("cache get Machine: %v", err)
		}
		return true, "machine visible in cache"
	}, func(last string) {
		t.Logf("Timed out waiting for Machine in cache: %s", last)
	})

	// Create KairosConfig
	kairosConfig := &bootstrapv1beta2.KairosConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-kairos-config",
			Namespace: "test-namespace",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: clusterv1.GroupVersion.String(),
					Kind:       "Machine",
					Name:       machine.Name,
					UID:        machine.UID,
					Controller: boolPtr(true),
				},
			},
		},
		Spec: bootstrapv1beta2.KairosConfigSpec{
			Role:              "control-plane",
			Distribution:      "k0s",
			KubernetesVersion: "v1.30.0+k0s.0",
			SingleNode:        true,
			UserName:          "kairos",
			UserPassword:      "kairos",
			UserGroups:        []string{"admin"},
		},
	}
	t.Logf("Creating KairosConfig %s", kairosConfig.Name)
	g.Expect(mgr.GetClient().Create(ctx, kairosConfig)).To(Succeed())

	// Wait for bootstrap data to be generated
	waitForCondition(t, 30*time.Second, 1*time.Second, func() (bool, string) {
		_, err := bootstrapReconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test-kairos-config",
				Namespace: "test-namespace",
			},
		})
		if err != nil {
			return false, fmt.Sprintf("reconcile error: %v", err)
		}
		config := &bootstrapv1beta2.KairosConfig{}
		if err := mgr.GetClient().Get(ctx, types.NamespacedName{
			Name:      "test-kairos-config",
			Namespace: "test-namespace",
		}, config); err != nil {
			return false, fmt.Sprintf("get KairosConfig: %v", err)
		}
		if config.Status.DataSecretName == nil || *config.Status.DataSecretName == "" {
			return false, "dataSecretName not set"
		}
		return true, fmt.Sprintf("dataSecretName=%s", *config.Status.DataSecretName)
	}, func(last string) {
		t.Logf("Timed out waiting for bootstrap data: %s", last)
		dumpBootstrapState(t, ctx, mgr.GetClient(), "test-namespace", "test-kairos-config")
	})

	// Verify Secret exists
	var secretName string
	waitForCondition(t, 10*time.Second, 1*time.Second, func() (bool, string) {
		config := &bootstrapv1beta2.KairosConfig{}
		if err := mgr.GetClient().Get(ctx, types.NamespacedName{
			Name:      "test-kairos-config",
			Namespace: "test-namespace",
		}, config); err != nil {
			return false, fmt.Sprintf("get KairosConfig: %v", err)
		}
		if config.Status.DataSecretName == nil {
			return false, "dataSecretName still nil"
		}
		secretName = *config.Status.DataSecretName
		return true, fmt.Sprintf("dataSecretName=%s", secretName)
	}, func(last string) {
		t.Logf("Timed out waiting for dataSecretName: %s", last)
		dumpBootstrapState(t, ctx, mgr.GetClient(), "test-namespace", "test-kairos-config")
	})

	secret := &corev1.Secret{}
	waitForCondition(t, 10*time.Second, 1*time.Second, func() (bool, string) {
		err := mgr.GetClient().Get(ctx, types.NamespacedName{
			Name:      secretName,
			Namespace: "test-namespace",
		}, secret)
		if err != nil {
			return false, fmt.Sprintf("get Secret %s: %v", secretName, err)
		}
		return true, "secret found"
	}, func(last string) {
		t.Logf("Timed out waiting for Secret: %s", last)
		dumpBootstrapState(t, ctx, mgr.GetClient(), "test-namespace", "test-kairos-config")
	})

	// Verify Secret contains cloud-config with k0s configuration
	g.Expect(secret.Data).To(HaveKey("value"))
	cloudConfig := string(secret.Data["value"])
	g.Expect(cloudConfig).To(ContainSubstring("#cloud-config"))
	g.Expect(cloudConfig).To(ContainSubstring("k0s:"))
	g.Expect(cloudConfig).To(ContainSubstring("enabled: true"))
	g.Expect(cloudConfig).To(ContainSubstring("--single"))
}

func dumpBootstrapState(t *testing.T, ctx context.Context, c client.Client, namespace, configName string) {
	t.Helper()
	config := &bootstrapv1beta2.KairosConfig{}
	if err := c.Get(ctx, types.NamespacedName{Name: configName, Namespace: namespace}, config); err == nil {
		if b, err := json.MarshalIndent(config, "", "  "); err == nil {
			t.Logf("KairosConfig:\n%s", string(b))
		}
	} else {
		t.Logf("Failed to get KairosConfig: %v", err)
	}

	secretList := &corev1.SecretList{}
	if err := c.List(ctx, secretList, client.InNamespace(namespace)); err == nil {
		names := make([]string, 0, len(secretList.Items))
		for _, s := range secretList.Items {
			names = append(names, s.Name)
		}
		t.Logf("Secrets in %s: %v", namespace, names)
	} else {
		t.Logf("Failed to list Secrets: %v", err)
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

func boolPtr(v bool) *bool {
	return &v
}
