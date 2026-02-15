package controller

import (
	"context"
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
	"github.com/giantswarm/klaus-operator/internal/resources"
)

// Condition types for KlausPersonality.
const (
	// PersonalityConditionReady indicates the personality has been validated and is usable.
	PersonalityConditionReady = "Ready"

	// PersonalityConditionValid indicates the personality spec passes validation.
	PersonalityConditionValid = "Valid"
)

// KlausPersonalityReconciler reconciles a KlausPersonality object.
type KlausPersonalityReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=klaus.giantswarm.io,resources=klauspersonalities,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=klaus.giantswarm.io,resources=klauspersonalities/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=klaus.giantswarm.io,resources=klauspersonalities/finalizers,verbs=update

// Reconcile handles a KlausPersonality event.
func (r *KlausPersonalityReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the KlausPersonality.
	var personality klausv1alpha1.KlausPersonality
	if err := r.Get(ctx, req.NamespacedName, &personality); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling KlausPersonality", "name", personality.Name)

	// Validate the personality spec.
	if err := r.validatePersonality(&personality); err != nil {
		apimeta.SetStatusCondition(&personality.Status.Conditions, metav1.Condition{
			Type:               PersonalityConditionValid,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: personality.Generation,
			Reason:             "ValidationError",
			Message:            err.Error(),
		})
		apimeta.SetStatusCondition(&personality.Status.Conditions, metav1.Condition{
			Type:               PersonalityConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: personality.Generation,
			Reason:             "ValidationError",
			Message:            err.Error(),
		})
		r.Recorder.Event(&personality, "Warning", "ValidationError", err.Error())

		personality.Status.ObservedGeneration = personality.Generation
		_ = r.Status().Update(ctx, &personality)
		return ctrl.Result{}, err
	}

	apimeta.SetStatusCondition(&personality.Status.Conditions, metav1.Condition{
		Type:               PersonalityConditionValid,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: personality.Generation,
		Reason:             "Valid",
		Message:            "Personality spec is valid",
	})

	// Count referencing instances.
	instanceCount, err := r.countReferencingInstances(ctx, personality.Name)
	if err != nil {
		logger.Error(err, "failed to count referencing instances")
		// Non-fatal: continue with stale count.
	}

	// Update status.
	personality.Status.InstanceCount = instanceCount
	personality.Status.PluginCount = len(personality.Spec.Plugins)
	personality.Status.MCPServerCount = len(personality.Spec.MCPServers) + len(personality.Spec.Claude.MCPServers)
	personality.Status.ObservedGeneration = personality.Generation

	apimeta.SetStatusCondition(&personality.Status.Conditions, metav1.Condition{
		Type:               PersonalityConditionReady,
		Status:             metav1.ConditionTrue,
		ObservedGeneration: personality.Generation,
		Reason:             "Reconciled",
		Message:            "Personality is ready",
	})

	if err := r.Status().Update(ctx, &personality); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// validatePersonality performs validation checks on the KlausPersonality spec.
func (r *KlausPersonalityReconciler) validatePersonality(personality *klausv1alpha1.KlausPersonality) error {
	// Check hooks vs settingsFile mutual exclusivity.
	if len(personality.Spec.Hooks) > 0 && personality.Spec.Claude.SettingsFile != "" {
		return fmt.Errorf("spec.hooks and spec.claude.settingsFile are mutually exclusive: " +
			"hooks are rendered to settings.json, but settingsFile points to a custom path")
	}

	// Validate plugins.
	if err := validatePersonalityPlugins(personality.Spec.Plugins); err != nil {
		return err
	}

	return nil
}

// validatePersonalityPlugins checks plugin references in a personality spec.
func validatePersonalityPlugins(plugins []klausv1alpha1.PluginReference) error {
	seen := make(map[string]string)
	for i, plugin := range plugins {
		hasTag := plugin.Tag != ""
		hasDigest := plugin.Digest != ""
		if !hasTag && !hasDigest {
			return fmt.Errorf("spec.plugins[%d] (%s): must specify either tag or digest",
				i, plugin.Repository)
		}
		if hasTag && hasDigest {
			return fmt.Errorf("spec.plugins[%d] (%s): tag and digest are mutually exclusive",
				i, plugin.Repository)
		}

		shortName := resources.ShortPluginName(plugin.Repository)
		if existing, ok := seen[shortName]; ok {
			return fmt.Errorf("spec.plugins[%d] (%s): short name %q conflicts with %s",
				i, plugin.Repository, shortName, existing)
		}
		seen[shortName] = plugin.Repository
	}
	return nil
}

// countReferencingInstances counts KlausInstance resources in the operator
// namespace that reference this personality.
func (r *KlausPersonalityReconciler) countReferencingInstances(ctx context.Context, personalityName string) (int, error) {
	var instanceList klausv1alpha1.KlausInstanceList
	if err := r.List(ctx, &instanceList, client.InNamespace(r.OperatorNamespace)); err != nil {
		return 0, err
	}

	count := 0
	for _, inst := range instanceList.Items {
		if inst.Spec.PersonalityRef != nil && inst.Spec.PersonalityRef.Name == personalityName {
			count++
		}
	}
	return count, nil
}

// SetupWithManager sets up the controller with the Manager.
// When a KlausPersonality changes, we also enqueue all KlausInstance resources
// that reference it to trigger re-reconciliation with the updated defaults.
func (r *KlausPersonalityReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&klausv1alpha1.KlausPersonality{}).
		Watches(&klausv1alpha1.KlausInstance{},
			handler.EnqueueRequestsFromMapFunc(r.mapInstanceToPersonality),
		).
		Named("klauspersonality").
		Complete(r)
}

// mapInstanceToPersonality maps a KlausInstance to the KlausPersonality it
// references. This triggers personality status updates (instance count) when
// instances are created/deleted.
func (r *KlausPersonalityReconciler) mapInstanceToPersonality(_ context.Context, obj client.Object) []reconcile.Request {
	instance, ok := obj.(*klausv1alpha1.KlausInstance)
	if !ok || instance.Spec.PersonalityRef == nil {
		return nil
	}

	return []reconcile.Request{{
		NamespacedName: types.NamespacedName{
			Name:      instance.Spec.PersonalityRef.Name,
			Namespace: instance.Namespace,
		},
	}}
}

// EnqueueReferencingInstances returns reconcile requests for all KlausInstance
// resources that reference the given personality. Called by the
// KlausInstanceReconciler's SetupWithManager to watch personality changes.
func EnqueueReferencingInstances(c client.Client, operatorNamespace string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		personality, ok := obj.(*klausv1alpha1.KlausPersonality)
		if !ok {
			return nil
		}

		var instanceList klausv1alpha1.KlausInstanceList
		if err := c.List(ctx, &instanceList, client.InNamespace(operatorNamespace)); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, inst := range instanceList.Items {
			if inst.Spec.PersonalityRef != nil && inst.Spec.PersonalityRef.Name == personality.Name {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      inst.Name,
						Namespace: inst.Namespace,
					},
				})
			}
		}
		return requests
	}
}
