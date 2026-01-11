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

package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	bootstrapv1beta2 "github.com/wrkode/kairos-capi/api/bootstrap/v1beta2"
	"github.com/wrkode/kairos-capi/internal/bootstrap"
)

// KairosConfigReconciler reconciles a KairosConfig object
type KairosConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kairosconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kairosconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kairosconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;clusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines/status;clusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups="",resources=secrets;events,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=create;get;list;update;patch;watch
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch
//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations;validatingwebhookconfigurations,verbs=get;list;patch;update
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=create;get;list;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;patch;update

// Reconcile is part of the main kubernetes reconciliation loop
func (r *KairosConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the KairosConfig instance
	kairosConfig := &bootstrapv1beta2.KairosConfig{}
	if err := r.Get(ctx, req.NamespacedName, kairosConfig); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !kairosConfig.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, log, kairosConfig)
	}

	// Add finalizer if needed
	if !controllerutil.ContainsFinalizer(kairosConfig, bootstrapv1beta2.KairosConfigFinalizer) {
		controllerutil.AddFinalizer(kairosConfig, bootstrapv1beta2.KairosConfigFinalizer)
		if err := r.Update(ctx, kairosConfig); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Check if paused
	if kairosConfig.Spec.Pause {
		log.Info("KairosConfig is paused, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	// Find the owning Machine
	machine, err := util.GetOwnerMachine(ctx, r.Client, kairosConfig.ObjectMeta)
	if err != nil {
		log.Error(err, "Failed to get owner machine")
		return ctrl.Result{}, err
	}
	if machine == nil {
		log.Info("Machine Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	// Find the owning Cluster
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		log.Error(err, "Failed to get cluster from machine metadata")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Cluster is not available yet")
		return ctrl.Result{}, nil
	}

	// Initialize patch helper
	helper, err := patch.NewHelper(kairosConfig, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always update observedGeneration
	kairosConfig.Status.ObservedGeneration = kairosConfig.Generation

	// Reconcile bootstrap data
	if err := r.reconcileBootstrapData(ctx, log, kairosConfig, machine, cluster); err != nil {
		// Mark conditions as false on error
		conditions.MarkFalse(kairosConfig, clusterv1.ReadyCondition, bootstrapv1beta2.BootstrapDataSecretGenerationFailedReason, clusterv1.ConditionSeverityWarning, err.Error())
		conditions.MarkFalse(kairosConfig, bootstrapv1beta2.BootstrapReadyCondition, bootstrapv1beta2.BootstrapDataSecretGenerationFailedReason, clusterv1.ConditionSeverityWarning, err.Error())
		conditions.MarkFalse(kairosConfig, bootstrapv1beta2.DataSecretAvailableCondition, bootstrapv1beta2.BootstrapDataSecretGenerationFailedReason, clusterv1.ConditionSeverityWarning, err.Error())

		kairosConfig.Status.FailureReason = bootstrapv1beta2.BootstrapDataSecretGenerationFailedReason
		kairosConfig.Status.FailureMessage = err.Error()
		kairosConfig.Status.Ready = false

		return ctrl.Result{}, helper.Patch(ctx, kairosConfig)
	}

	// Mark conditions as true on success
	conditions.MarkTrue(kairosConfig, clusterv1.ReadyCondition)
	conditions.MarkTrue(kairosConfig, bootstrapv1beta2.BootstrapReadyCondition)
	conditions.MarkTrue(kairosConfig, bootstrapv1beta2.DataSecretAvailableCondition)

	// Clear failure fields
	kairosConfig.Status.FailureReason = ""
	kairosConfig.Status.FailureMessage = ""

	// Update status
	return ctrl.Result{}, helper.Patch(ctx, kairosConfig)
}

func (r *KairosConfigReconciler) reconcileBootstrapData(ctx context.Context, log logr.Logger, kairosConfig *bootstrapv1beta2.KairosConfig, machine *clusterv1.Machine, cluster *clusterv1.Cluster) error {
	// If dataSecretName is already set, verify the secret exists
	if kairosConfig.Status.DataSecretName != nil {
		secret := &corev1.Secret{}
		secretKey := types.NamespacedName{
			Name:      *kairosConfig.Status.DataSecretName,
			Namespace: kairosConfig.Namespace,
		}
		if err := r.Get(ctx, secretKey, secret); err != nil {
			if apierrors.IsNotFound(err) {
				// Secret was deleted, regenerate
				log.Info("Bootstrap secret was deleted, regenerating", "secret", *kairosConfig.Status.DataSecretName)
				kairosConfig.Status.DataSecretName = nil
			} else {
				return fmt.Errorf("failed to get bootstrap secret: %w", err)
			}
		} else {
			// Secret exists, verify it's ready
			log.Info("Bootstrap data already generated", "secret", *kairosConfig.Status.DataSecretName)
			kairosConfig.Status.Ready = true
			// Ensure initialization.dataSecretCreated is set
			if kairosConfig.Status.Initialization == nil {
				kairosConfig.Status.Initialization = &bootstrapv1beta2.KairosConfigInitialization{}
			}
			kairosConfig.Status.Initialization.DataSecretCreated = true
			return nil
		}
	}

	// Generate Kairos cloud-config
	cloudConfig, err := r.generateCloudConfig(ctx, log, kairosConfig, machine, cluster)
	if err != nil {
		return fmt.Errorf("failed to generate cloud-config: %w", err)
	}

	// Base64 encode the cloud-config for VMware infrastructure provider
	// VMware expects cloud-init data to be base64 encoded when passed through guestinfo properties
	// CAPV (Cluster API Provider vSphere) will use this encoded data directly
	cloudConfigBase64 := base64.StdEncoding.EncodeToString([]byte(cloudConfig))

	// Create Secret with bootstrap data
	randomSuffix, err := randomString(6)
	if err != nil {
		return fmt.Errorf("failed to generate random string: %w", err)
	}
	secretName := fmt.Sprintf("%s-%s", kairosConfig.Name, randomSuffix)
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: kairosConfig.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel: cluster.Name,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: kairosConfig.APIVersion,
					Kind:       kairosConfig.Kind,
					Name:       kairosConfig.Name,
					UID:        kairosConfig.UID,
					Controller: func() *bool { b := true; return &b }(),
				},
			},
		},
		Type: clusterv1.ClusterSecretType,
		Data: map[string][]byte{
			"value": []byte(cloudConfigBase64),
		},
	}

	if err := r.Create(ctx, secret); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Secret already exists, use it
			if err := r.Get(ctx, types.NamespacedName{Name: secretName, Namespace: kairosConfig.Namespace}, secret); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// Update status with dataSecretName
	kairosConfig.Status.DataSecretName = &secretName
	kairosConfig.Status.Ready = true

	// Set initialization.dataSecretCreated as required by Cluster API contract
	// This field is used by the Machine controller to determine when bootstrap data is ready
	if kairosConfig.Status.Initialization == nil {
		kairosConfig.Status.Initialization = &bootstrapv1beta2.KairosConfigInitialization{}
	}
	kairosConfig.Status.Initialization.DataSecretCreated = true

	log.Info("Bootstrap data secret created", "secret", secretName)
	return nil
}

func (r *KairosConfigReconciler) generateCloudConfig(ctx context.Context, log logr.Logger, kairosConfig *bootstrapv1beta2.KairosConfig, machine *clusterv1.Machine, cluster *clusterv1.Cluster) (string, error) {
	// Determine role
	role := kairosConfig.Spec.Role
	if role == "" {
		// Infer from machine labels
		if util.IsControlPlaneMachine(machine) {
			role = "control-plane"
		} else {
			role = "worker"
		}
	}

	// Determine distribution
	distribution := kairosConfig.Spec.Distribution
	if distribution == "" {
		distribution = "k0s"
	}

	// Get cluster information
	serverAddress := kairosConfig.Spec.ServerAddress
	if serverAddress == "" && cluster.Spec.ControlPlaneEndpoint.IsValid() {
		serverAddress = fmt.Sprintf("https://%s:%d", cluster.Spec.ControlPlaneEndpoint.Host, cluster.Spec.ControlPlaneEndpoint.Port)
	}

	// Generate cloud-config based on distribution
	switch distribution {
	case "k0s":
		return r.generateK0sCloudConfig(ctx, log, kairosConfig, machine, cluster, role, serverAddress)
	case "k3s":
		return "", fmt.Errorf("k3s distribution not yet implemented")
	default:
		return "", fmt.Errorf("unsupported distribution: %s", distribution)
	}
}

func (r *KairosConfigReconciler) generateK0sCloudConfig(ctx context.Context, log logr.Logger, kairosConfig *bootstrapv1beta2.KairosConfig, machine *clusterv1.Machine, cluster *clusterv1.Cluster, role, serverAddress string) (string, error) {
	// Determine single-node mode
	// Single-node is determined by:
	// 1. Explicit flag in KairosConfig.spec.singleNode
	// 2. Or if this is a control-plane and we can check the owning KairosControlPlane
	singleNode := kairosConfig.Spec.SingleNode
	if !singleNode && role == "control-plane" && machine != nil {
		// Try to find the owning KairosControlPlane to check replicas
		ownerRef := metav1.GetControllerOf(machine)
		if ownerRef != nil && ownerRef.Kind == "KairosControlPlane" {
			// For now, we rely on the SingleNode flag in spec
			// In the future, we could fetch the KCP and check spec.replicas == 1
			log.V(4).Info("Control plane node, single-node mode determined from spec", "singleNode", singleNode)
		}
	}

	// Get worker token if needed (for worker nodes)
	// Precedence: WorkerTokenSecretRef > WorkerToken > TokenSecretRef > Token
	// TODO: Add validating webhook to enforce worker token requirement at API level
	var workerToken string
	if role == "worker" {
		// Try WorkerTokenSecretRef first (most secure)
		if kairosConfig.Spec.WorkerTokenSecretRef != nil {
			secretKey := types.NamespacedName{
				Namespace: kairosConfig.Namespace,
				Name:      kairosConfig.Spec.WorkerTokenSecretRef.Name,
			}
			// Use specified namespace or fall back to KairosConfig namespace
			if kairosConfig.Spec.WorkerTokenSecretRef.Namespace != "" {
				secretKey.Namespace = kairosConfig.Spec.WorkerTokenSecretRef.Namespace
			}

			secret := &corev1.Secret{}
			if err := r.Get(ctx, secretKey, secret); err != nil {
				return "", fmt.Errorf("failed to get worker token secret %s/%s: %w", secretKey.Namespace, secretKey.Name, err)
			}

			// Use specified key or default to "token"
			key := kairosConfig.Spec.WorkerTokenSecretRef.Key
			if key == "" {
				key = "token"
			}

			if tokenData, ok := secret.Data[key]; ok {
				workerToken = string(tokenData)
			} else {
				return "", fmt.Errorf("worker token secret %s/%s does not contain key '%s'", secretKey.Namespace, secretKey.Name, key)
			}
		} else if kairosConfig.Spec.WorkerToken != "" {
			// Fall back to inline WorkerToken
			workerToken = kairosConfig.Spec.WorkerToken
		} else if kairosConfig.Spec.TokenSecretRef != nil {
			// Fall back to legacy TokenSecretRef
			secret := &corev1.Secret{}
			secretKey := types.NamespacedName{
				Namespace: cluster.Namespace,
				Name:      kairosConfig.Spec.TokenSecretRef.Name,
			}
			if err := r.Get(ctx, secretKey, secret); err != nil {
				return "", fmt.Errorf("failed to get token secret: %w", err)
			}
			// Try common token keys
			if tokenData, ok := secret.Data["token"]; ok {
				workerToken = string(tokenData)
			} else if tokenData, ok := secret.Data["value"]; ok {
				workerToken = string(tokenData)
			} else {
				return "", fmt.Errorf("token secret does not contain 'token' or 'value' key")
			}
		} else if kairosConfig.Spec.Token != "" {
			// Fall back to legacy Token
			workerToken = kairosConfig.Spec.Token
		}

		// Validate worker token is present
		if workerToken == "" {
			return "", fmt.Errorf("worker token is required for worker nodes: either WorkerTokenSecretRef, WorkerToken, TokenSecretRef, or Token must be set")
		}
	}

	// Set defaults for user configuration
	userName := kairosConfig.Spec.UserName
	if userName == "" {
		userName = "kairos"
	}
	userPassword := kairosConfig.Spec.UserPassword
	if userPassword == "" {
		userPassword = "kairos"
	}
	userGroups := kairosConfig.Spec.UserGroups
	if len(userGroups) == 0 {
		userGroups = []string{"admin"}
	}

	// Set hostname prefix (default to "metal-" if not specified)
	hostnamePrefix := kairosConfig.Spec.HostnamePrefix
	if hostnamePrefix == "" {
		hostnamePrefix = "metal-"
	}

	// Set install configuration (with defaults)
	var installConfig *bootstrap.InstallConfig
	if kairosConfig.Spec.Install != nil {
		installConfig = &bootstrap.InstallConfig{
			Auto:   true,   // Default to true
			Device: "auto", // Default to "auto"
			Reboot: true,   // Default to true
		}
		if kairosConfig.Spec.Install.Auto != nil {
			installConfig.Auto = *kairosConfig.Spec.Install.Auto
		}
		if kairosConfig.Spec.Install.Device != "" {
			installConfig.Device = kairosConfig.Spec.Install.Device
		}
		if kairosConfig.Spec.Install.Reboot != nil {
			installConfig.Reboot = *kairosConfig.Spec.Install.Reboot
		}
	}

	// Build template data
	templateData := bootstrap.TemplateData{
		Role:           role,
		SingleNode:     singleNode,
		UserName:       userName,
		UserPassword:   userPassword,
		UserGroups:     userGroups,
		GitHubUser:     kairosConfig.Spec.GitHubUser,
		SSHPublicKey:   kairosConfig.Spec.SSHPublicKey,
		WorkerToken:    workerToken,
		Manifests:      kairosConfig.Spec.Manifests,
		HostnamePrefix: hostnamePrefix,
		Install:        installConfig,
	}

	// Render template
	return bootstrap.RenderK0sCloudConfig(templateData)
}

func (r *KairosConfigReconciler) reconcileDelete(ctx context.Context, log logr.Logger, kairosConfig *bootstrapv1beta2.KairosConfig) (ctrl.Result, error) {
	// Remove finalizer
	controllerutil.RemoveFinalizer(kairosConfig, bootstrapv1beta2.KairosConfigFinalizer)
	return ctrl.Result{}, r.Update(ctx, kairosConfig)
}

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

// randomString generates a random lowercase alphanumeric string of the given length
// This ensures the string is RFC 1123 compliant for Kubernetes resource names
func randomString(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		randomByte := make([]byte, 1)
		if _, err := rand.Read(randomByte); err != nil {
			return "", err
		}
		b[i] = charset[randomByte[0]%byte(len(charset))]
	}
	return string(b), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KairosConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&bootstrapv1beta2.KairosConfig{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.machineToKairosConfig),
		).
		Complete(r)
}

// machineToKairosConfig maps a Machine to its KairosConfig
func (r *KairosConfigReconciler) machineToKairosConfig(ctx context.Context, o client.Object) []reconcile.Request {
	machine, ok := o.(*clusterv1.Machine)
	if !ok {
		return nil
	}

	// Check if Machine has a bootstrap config reference
	if machine.Spec.Bootstrap.ConfigRef == nil {
		return nil
	}

	// Check if it's a KairosConfig
	if machine.Spec.Bootstrap.ConfigRef.GroupVersionKind().Group != bootstrapv1beta2.GroupVersion.Group {
		return nil
	}
	if machine.Spec.Bootstrap.ConfigRef.Kind != "KairosConfig" {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      machine.Spec.Bootstrap.ConfigRef.Name,
				Namespace: machine.Namespace,
			},
		},
	}
}
