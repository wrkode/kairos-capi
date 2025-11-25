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

package controlplane

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	controlplanev1beta2 "github.com/wrkode/kairos-capi/api/controlplane/v1beta2"
	"github.com/wrkode/kairos-capi/internal/infrastructure"
)

// KairosControlPlaneReconciler reconciles a KairosControlPlane object
type KairosControlPlaneReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=kairoscontrolplanes,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=kairoscontrolplanes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=kairoscontrolplanes/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;machines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters/status;machines/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=bootstrap.cluster.x-k8s.io,resources=kairosconfigs;kairosconfigtemplates,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop
func (r *KairosControlPlaneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the KairosControlPlane instance
	kcp := &controlplanev1beta2.KairosControlPlane{}
	if err := r.Get(ctx, req.NamespacedName, kcp); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion
	if !kcp.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, log, kcp)
	}

	// Add finalizer if needed
	if !controllerutil.ContainsFinalizer(kcp, controlplanev1beta2.KairosControlPlaneFinalizer) {
		controllerutil.AddFinalizer(kcp, controlplanev1beta2.KairosControlPlaneFinalizer)
		if err := r.Update(ctx, kcp); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Find the owning Cluster
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, kcp.ObjectMeta)
	if err != nil {
		// If the error is due to missing cluster-name label or Cluster not found,
		// try to find the Cluster by searching for Clusters that reference this KairosControlPlane
		errMsg := err.Error()
		if errMsg == "no \"cluster.x-k8s.io/cluster-name\" label present" || 
		   apierrors.IsNotFound(err) ||
		   (errMsg != "" && (errMsg == "failed to get Cluster/kairos-cluster: Cluster.cluster.x-k8s.io \"kairos-cluster\" not found" || 
		   errMsg == "Cluster.cluster.x-k8s.io \"kairos-cluster\" not found")) {
			log.Info("cluster-name label missing or Cluster not found via metadata, searching for Cluster that references this control plane", "error", errMsg)
			cluster, err = r.findClusterForControlPlane(ctx, kcp)
			if err != nil {
				log.Error(err, "Failed to find cluster for control plane")
				return ctrl.Result{}, err
			}
			if cluster != nil {
				// Set the label on the KairosControlPlane
				if kcp.Labels == nil {
					kcp.Labels = make(map[string]string)
				}
				kcp.Labels[clusterv1.ClusterNameLabel] = cluster.Name
				if err := r.Update(ctx, kcp); err != nil {
					log.Error(err, "Failed to update KairosControlPlane with cluster-name label")
					return ctrl.Result{}, err
				}
				log.Info("Set cluster-name label on KairosControlPlane", "cluster", cluster.Name)
				// Return to trigger a new reconcile with the label set
				return ctrl.Result{Requeue: true}, nil
			}
		} else {
			log.Error(err, "Failed to get cluster from metadata")
			return ctrl.Result{}, err
		}
	}
	if cluster == nil {
		log.Info("Cluster is not available yet")
		return ctrl.Result{}, nil
	}

	// Initialize patch helper
	helper, err := patch.NewHelper(kcp, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always update observedGeneration
	kcp.Status.ObservedGeneration = kcp.Generation

	// Reconcile control plane machines
	if err := r.reconcileMachines(ctx, log, kcp, cluster); err != nil {
		conditions.MarkFalse(kcp, clusterv1.ReadyCondition, controlplanev1beta2.ControlPlaneInitializationFailedReason, clusterv1.ConditionSeverityWarning, err.Error())
		conditions.MarkFalse(kcp, controlplanev1beta2.AvailableCondition, controlplanev1beta2.ControlPlaneInitializationFailedReason, clusterv1.ConditionSeverityWarning, err.Error())
		kcp.Status.FailureReason = controlplanev1beta2.ControlPlaneInitializationFailedReason
		kcp.Status.FailureMessage = err.Error()
		return ctrl.Result{}, helper.Patch(ctx, kcp)
	}

	// Update status
	if err := r.updateStatus(ctx, log, kcp, cluster); err != nil {
		return ctrl.Result{}, err
	}

	// Update conditions based on status
	if kcp.Status.Initialized {
		conditions.MarkTrue(kcp, clusterv1.ReadyCondition)
		conditions.MarkTrue(kcp, controlplanev1beta2.AvailableCondition)
		if kcp.Status.ReadyReplicas > 0 {
			conditions.MarkTrue(kcp, clusterv1.ReadyCondition)
		} else {
			conditions.MarkFalse(kcp, clusterv1.ReadyCondition, controlplanev1beta2.WaitingForMachinesReadyReason, clusterv1.ConditionSeverityInfo, "Waiting for control plane machines to be ready")
		}
	} else {
		conditions.MarkFalse(kcp, clusterv1.ReadyCondition, controlplanev1beta2.WaitingForMachinesReason, clusterv1.ConditionSeverityInfo, "Waiting for control plane initialization")
		conditions.MarkFalse(kcp, controlplanev1beta2.AvailableCondition, controlplanev1beta2.WaitingForMachinesReason, clusterv1.ConditionSeverityInfo, "Waiting for control plane initialization")
	}

	// Clear failure fields if successful
	if kcp.Status.ReadyReplicas > 0 {
		kcp.Status.FailureReason = ""
		kcp.Status.FailureMessage = ""
	}

	return ctrl.Result{}, helper.Patch(ctx, kcp)
}

// findClusterForControlPlane searches for a Cluster that references this KairosControlPlane
func (r *KairosControlPlaneReconciler) findClusterForControlPlane(ctx context.Context, kcp *controlplanev1beta2.KairosControlPlane) (*clusterv1.Cluster, error) {
	// List all Clusters in the same namespace
	clusters := &clusterv1.ClusterList{}
	if err := r.List(ctx, clusters, client.InNamespace(kcp.Namespace)); err != nil {
		return nil, fmt.Errorf("failed to list clusters: %w", err)
	}

	// Find the Cluster that references this KairosControlPlane
	for i := range clusters.Items {
		cluster := &clusters.Items[i]
		if cluster.Spec.ControlPlaneRef != nil &&
			cluster.Spec.ControlPlaneRef.Kind == "KairosControlPlane" &&
			cluster.Spec.ControlPlaneRef.Name == kcp.Name {
			// Check namespace - it might be empty (defaults to cluster namespace)
			refNamespace := cluster.Spec.ControlPlaneRef.Namespace
			if refNamespace == "" || refNamespace == kcp.Namespace {
				// Check API version/group matches
				refAPIVersion := cluster.Spec.ControlPlaneRef.APIVersion
				if refAPIVersion == "" || refAPIVersion == controlplanev1beta2.GroupVersion.String() {
					return cluster, nil
				}
			}
		}
	}

	return nil, nil
}

func (r *KairosControlPlaneReconciler) reconcileMachines(ctx context.Context, log logr.Logger, kcp *controlplanev1beta2.KairosControlPlane, cluster *clusterv1.Cluster) error {
	// Get desired replica count
	desiredReplicas := int32(1)
	if kcp.Spec.Replicas != nil {
		desiredReplicas = *kcp.Spec.Replicas
	}

	// List existing control plane machines
	machines, err := r.getControlPlaneMachines(ctx, kcp, cluster)
	if err != nil {
		return fmt.Errorf("failed to list control plane machines: %w", err)
	}

	currentReplicas := int32(len(machines))

	log.Info("Reconciling control plane machines", "desired", desiredReplicas, "current", currentReplicas)

	// Create machines if needed
	if currentReplicas < desiredReplicas {
		toCreate := desiredReplicas - currentReplicas
		for i := int32(0); i < toCreate; i++ {
			if err := r.createControlPlaneMachine(ctx, log, kcp, cluster, currentReplicas+i); err != nil {
				return fmt.Errorf("failed to create control plane machine: %w", err)
			}
		}
	}

	// Delete machines if needed (for MVP, we only support single control plane)
	if currentReplicas > desiredReplicas && desiredReplicas == 1 {
		// For MVP, delete excess machines
		toDelete := currentReplicas - desiredReplicas
		for i := int32(0); i < toDelete; i++ {
			machine := machines[i]
			if err := r.Delete(ctx, machine); err != nil {
				return fmt.Errorf("failed to delete control plane machine: %w", err)
			}
		}
	}

	return nil
}

func (r *KairosControlPlaneReconciler) createControlPlaneMachine(ctx context.Context, log logr.Logger, kcp *controlplanev1beta2.KairosControlPlane, cluster *clusterv1.Cluster, index int32) error {
	machineName := fmt.Sprintf("%s-%d", kcp.Name, index)

	// Create KairosConfig
	kairosConfig := &bootstrapv1beta2.KairosConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%d", kcp.Name, index),
			Namespace: kcp.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(kcp, controlplanev1beta2.GroupVersion.WithKind("KairosControlPlane")),
			},
		},
		Spec: bootstrapv1beta2.KairosConfigSpec{
			Role:              "control-plane",
			Distribution:      "k0s",
			KubernetesVersion: kcp.Spec.Version,
		},
	}

	// Determine single-node mode from replicas
	replicas := int32(1)
	if kcp.Spec.Replicas != nil {
		replicas = *kcp.Spec.Replicas
	}
	kairosConfig.Spec.SingleNode = (replicas == 1)
	log.Info("Setting SingleNode flag", "singleNode", kairosConfig.Spec.SingleNode, "replicas", replicas)

	// If there's a template, merge its spec
	if kcp.Spec.KairosConfigTemplate.Name != "" {
		template := &bootstrapv1beta2.KairosConfigTemplate{}
		templateKey := types.NamespacedName{
			Namespace: kcp.Namespace,
			Name:      kcp.Spec.KairosConfigTemplate.Name,
		}
		if err := r.Get(ctx, templateKey, template); err != nil {
			return fmt.Errorf("failed to get KairosConfigTemplate: %w", err)
		}
		// Merge template spec
		kairosConfig.Spec = template.Spec.Template.Spec
		kairosConfig.Spec.Role = "control-plane"
		kairosConfig.Spec.KubernetesVersion = kcp.Spec.Version
		// Override SingleNode based on replicas (replicas takes precedence)
		kairosConfig.Spec.SingleNode = (replicas == 1)
	}

	if err := r.Create(ctx, kairosConfig); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
	}

	// Create infrastructure machine (clone from template)
	infraMachine, err := r.createInfrastructureMachine(ctx, log, kcp, cluster, machineName)
	if err != nil {
		return fmt.Errorf("failed to create infrastructure machine: %w", err)
	}

	// Create Machine
	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName,
			Namespace: kcp.Namespace,
			Labels: map[string]string{
				clusterv1.ClusterNameLabel:         cluster.Name,
				clusterv1.MachineControlPlaneLabel: "",
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(kcp, controlplanev1beta2.GroupVersion.WithKind("KairosControlPlane")),
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName: cluster.Name,
			Version:     &kcp.Spec.Version,
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: &corev1.ObjectReference{
					APIVersion: bootstrapv1beta2.GroupVersion.String(),
					Kind:       "KairosConfig",
					Name:       kairosConfig.Name,
					Namespace:  kairosConfig.Namespace,
				},
			},
			InfrastructureRef: corev1.ObjectReference{
				APIVersion: infraMachine.GetObjectKind().GroupVersionKind().GroupVersion().String(),
				Kind:       infraMachine.GetObjectKind().GroupVersionKind().Kind,
				Name:       infraMachine.GetName(),
				Namespace:  infraMachine.GetNamespace(),
			},
		},
	}

	return r.Create(ctx, machine)
}

func (r *KairosControlPlaneReconciler) createInfrastructureMachine(ctx context.Context, log logr.Logger, kcp *controlplanev1beta2.KairosControlPlane, cluster *clusterv1.Cluster, machineName string) (client.Object, error) {
	infraRef := kcp.Spec.MachineTemplate.InfrastructureRef

	// Prepare labels and annotations
	labels := map[string]string{
		clusterv1.ClusterNameLabel:         cluster.Name,
		clusterv1.MachineControlPlaneLabel: "",
	}
	// Merge with template metadata labels
	if kcp.Spec.MachineTemplate.Metadata.Labels != nil {
		for k, v := range kcp.Spec.MachineTemplate.Metadata.Labels {
			labels[k] = v
		}
	}

	annotations := map[string]string{}
	// Merge with template metadata annotations
	if kcp.Spec.MachineTemplate.Metadata.Annotations != nil {
		for k, v := range kcp.Spec.MachineTemplate.Metadata.Annotations {
			annotations[k] = v
		}
	}

	// Clone infrastructure machine using the helper
	infraMachine, err := infrastructure.CloneInfrastructureMachine(
		ctx,
		r.Client,
		r.Scheme,
		infraRef,
		machineName,
		kcp.Namespace,
		labels,
		annotations,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to clone infrastructure machine: %w", err)
	}

	// Set owner reference
	if err := controllerutil.SetControllerReference(kcp, infraMachine, r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to set controller reference: %w", err)
	}

	// Create the infrastructure machine
	if err := r.Create(ctx, infraMachine); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, fmt.Errorf("failed to create infrastructure machine: %w", err)
		}
		// Machine already exists, get it
		if err := r.Get(ctx, types.NamespacedName{Name: machineName, Namespace: kcp.Namespace}, infraMachine); err != nil {
			return nil, fmt.Errorf("failed to get existing infrastructure machine: %w", err)
		}
	}

	log.Info("Created infrastructure machine", "kind", infraRef.Kind, "name", machineName)
	return infraMachine, nil
}

func (r *KairosControlPlaneReconciler) getControlPlaneMachines(ctx context.Context, kcp *controlplanev1beta2.KairosControlPlane, cluster *clusterv1.Cluster) ([]*clusterv1.Machine, error) {
	selector := labels.SelectorFromSet(map[string]string{
		clusterv1.ClusterNameLabel:         cluster.Name,
		clusterv1.MachineControlPlaneLabel: "",
	})

	machineList := &clusterv1.MachineList{}
	if err := r.List(ctx, machineList, client.InNamespace(kcp.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, err
	}

	machines := make([]*clusterv1.Machine, 0, len(machineList.Items))
	for i := range machineList.Items {
		machine := &machineList.Items[i]
		// Check if this machine is owned by this KCP
		ownerRef := metav1.GetControllerOf(machine)
		if ownerRef != nil && ownerRef.Kind == "KairosControlPlane" && ownerRef.Name == kcp.Name {
			machines = append(machines, machine)
		}
	}

	return machines, nil
}

func (r *KairosControlPlaneReconciler) updateStatus(ctx context.Context, log logr.Logger, kcp *controlplanev1beta2.KairosControlPlane, cluster *clusterv1.Cluster) error {
	machines, err := r.getControlPlaneMachines(ctx, kcp, cluster)
	if err != nil {
		return err
	}

	kcp.Status.Replicas = int32(len(machines))

	readyReplicas := int32(0)
	updatedReplicas := int32(0)
	unavailableReplicas := int32(0)

	for _, machine := range machines {
		// Check if machine is ready
		if machine.Status.NodeRef != nil {
			readyReplicas++
		}

		// Check if machine is updated (matches desired version)
		if machine.Spec.Version != nil && *machine.Spec.Version == kcp.Spec.Version {
			updatedReplicas++
		}

		// Check if machine is unavailable
		if machine.Status.Phase != string(clusterv1.MachinePhaseRunning) {
			unavailableReplicas++
		}
	}

	kcp.Status.ReadyReplicas = readyReplicas
	kcp.Status.UpdatedReplicas = updatedReplicas
	kcp.Status.UnavailableReplicas = unavailableReplicas

	// Mark as initialized if we have at least one ready replica
	if readyReplicas > 0 && !kcp.Status.Initialized {
		kcp.Status.Initialized = true
		log.Info("Control plane initialized")
	}

	return nil
}

func (r *KairosControlPlaneReconciler) reconcileDelete(ctx context.Context, log logr.Logger, kcp *controlplanev1beta2.KairosControlPlane) (ctrl.Result, error) {
	// Remove finalizer
	controllerutil.RemoveFinalizer(kcp, controlplanev1beta2.KairosControlPlaneFinalizer)
	return ctrl.Result{}, r.Update(ctx, kcp)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KairosControlPlaneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&controlplanev1beta2.KairosControlPlane{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(r.machineToKairosControlPlane),
		).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(r.clusterToKairosControlPlane),
		).
		Complete(r)
}

// machineToKairosControlPlane maps a Machine to its KairosControlPlane
func (r *KairosControlPlaneReconciler) machineToKairosControlPlane(ctx context.Context, o client.Object) []reconcile.Request {
	machine, ok := o.(*clusterv1.Machine)
	if !ok {
		return nil
	}

	// Check if it's a control plane machine
	if !util.IsControlPlaneMachine(machine) {
		return nil
	}

	// Find the owning KairosControlPlane
	ownerRef := metav1.GetControllerOf(machine)
	if ownerRef == nil {
		return nil
	}

	if ownerRef.Kind != "KairosControlPlane" || ownerRef.APIVersion != controlplanev1beta2.GroupVersion.String() {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      ownerRef.Name,
				Namespace: machine.Namespace,
			},
		},
	}
}

// clusterToKairosControlPlane maps a Cluster to its KairosControlPlane
func (r *KairosControlPlaneReconciler) clusterToKairosControlPlane(ctx context.Context, o client.Object) []reconcile.Request {
	cluster, ok := o.(*clusterv1.Cluster)
	if !ok {
		return nil
	}

	if cluster.Spec.ControlPlaneRef == nil {
		return nil
	}

	if cluster.Spec.ControlPlaneRef.Kind != "KairosControlPlane" || cluster.Spec.ControlPlaneRef.APIVersion != controlplanev1beta2.GroupVersion.String() {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      cluster.Spec.ControlPlaneRef.Name,
				Namespace: cluster.Namespace,
			},
		},
	}
}
