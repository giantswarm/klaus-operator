package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
	"github.com/giantswarm/klaus-operator/internal/resources"
)

const finalizerName = "klaus.giantswarm.io/finalizer"

// mcpServerGVK is the GroupVersionKind for the MCPServer CRD managed by muster.
var mcpServerGVK = schema.GroupVersionKind{
	Group:   "muster.giantswarm.io",
	Version: "v1alpha1",
	Kind:    "MCPServer",
}

// KlausInstanceReconciler reconciles a KlausInstance object.
type KlausInstanceReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	Recorder           record.EventRecorder
	KlausImage         string
	GitCloneImage      string
	AnthropicKeySecret string
	AnthropicKeyNs     string
	OperatorNamespace  string
}

// +kubebuilder:rbac:groups=klaus.giantswarm.io,resources=klausinstances,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=klaus.giantswarm.io,resources=klausinstances/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=klaus.giantswarm.io,resources=klausinstances/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=muster.giantswarm.io,resources=mcpservers,verbs=get;list;watch;create;update;patch;delete

// Reconcile handles a KlausInstance event.
func (r *KlausInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the KlausInstance.
	var instance klausv1alpha1.KlausInstance
	if err := r.Get(ctx, req.NamespacedName, &instance); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Handle deletion.
	if !instance.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, &instance)
	}

	// Ensure finalizer. Return early so the next reconcile starts with a
	// consistent object that includes the finalizer.
	if !controllerutil.ContainsFinalizer(&instance, finalizerName) {
		controllerutil.AddFinalizer(&instance, finalizerName)
		return ctrl.Result{}, r.Update(ctx, &instance)
	}

	// Update status to Pending.
	if instance.Status.State == "" {
		instance.Status.State = klausv1alpha1.InstanceStatePending
		if err := r.Status().Update(ctx, &instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Resolve personalityRef and merge personality defaults into instance spec.
	// We work on a deep copy so the original object is not mutated in the cache.
	merged := instance.DeepCopy()
	if err := r.resolvePersonality(ctx, merged); err != nil {
		return r.updateStatusError(ctx, &instance, "PersonalityError", err)
	}

	// Detect inline MCP server configs that will be overridden by resolved
	// KlausMCPServer references and emit informational events.
	for _, ref := range merged.Spec.MCPServers {
		if _, exists := merged.Spec.Claude.MCPServers[ref.Name]; exists {
			r.Recorder.Event(&instance, corev1.EventTypeNormal, "MCPServerOverride",
				fmt.Sprintf("KlausMCPServer %q overrides inline MCP server config with the same name", ref.Name))
		}
	}

	// Resolve KlausMCPServer references and merge their configs and secrets
	// into the merged spec. This must happen after personality merge so that
	// personality-level MCP server refs are included.
	if err := r.resolveMCPServers(ctx, merged); err != nil {
		return r.updateStatusError(ctx, &instance, "MCPServerRefError", err)
	}

	// Validate the merged spec.
	if err := resources.ValidateSpec(merged); err != nil {
		return r.updateStatusError(ctx, &instance, "ValidationError", err)
	}

	// Determine the target namespace.
	namespace := resources.UserNamespace(merged.Spec.Owner)

	logger.Info("reconciling KlausInstance",
		"instance", merged.Name,
		"owner", merged.Spec.Owner,
		"namespace", namespace,
	)

	// 1. Ensure namespace exists.
	if err := r.ensureNamespace(ctx, merged, namespace); err != nil {
		return r.updateStatusError(ctx, &instance, "NamespaceError", err)
	}

	// 2. Copy the Anthropic API key Secret.
	found, err := r.copyAPIKeySecret(ctx, merged, namespace)
	if err != nil {
		return r.updateStatusError(ctx, &instance, "SecretError", err)
	}
	if !found {
		logger.Info("Anthropic API key secret not found, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// 3. Copy git credential Secret (if workspace.gitSecretRef configured).
	gitSecretOp, err := r.copyGitSecret(ctx, merged, namespace)
	if err != nil {
		return r.updateStatusError(ctx, &instance, "GitSecretError", err)
	}
	if gitSecretOp == controllerutil.OperationResultCreated || gitSecretOp == controllerutil.OperationResultUpdated {
		r.Recorder.Event(&instance, corev1.EventTypeNormal, "GitSecretReady",
			"Git credential secret copied to user namespace")
	}

	// 4. Create/update ConfigMap.
	cm, err := resources.BuildConfigMap(merged, namespace)
	if err != nil {
		setCondition(&instance, ConditionConfigReady, metav1.ConditionFalse, "BuildError", err.Error())
		return r.updateStatusError(ctx, &instance, "ConfigMapError", err)
	}
	if err := r.reconcileConfigMap(ctx, &instance, cm); err != nil {
		setCondition(&instance, ConditionConfigReady, metav1.ConditionFalse, "ReconcileError", err.Error())
		return r.updateStatusError(ctx, &instance, "ConfigMapError", err)
	}
	setCondition(&instance, ConditionConfigReady, metav1.ConditionTrue, "Reconciled", "ConfigMap reconciled")

	// 5. Create/update PVC (if workspace configured).
	if err := r.reconcilePVC(ctx, merged, namespace); err != nil {
		return r.updateStatusError(ctx, &instance, "PVCError", err)
	}

	// 6. Ensure ServiceAccount.
	if err := r.ensureServiceAccount(ctx, merged, namespace); err != nil {
		return r.updateStatusError(ctx, &instance, "ServiceAccountError", err)
	}

	// 7. Create/update Deployment.
	// Resolve the container image: instance > personality > operator default.
	resolvedImage := r.KlausImage
	if merged.Spec.Image != "" {
		resolvedImage = merged.Spec.Image
	}
	dep := resources.BuildDeployment(merged, namespace, resolvedImage, r.GitCloneImage, cm.Data)
	depOp, err := r.reconcileDeployment(ctx, &instance, dep)
	if err != nil {
		setCondition(&instance, ConditionDeploymentReady, metav1.ConditionFalse, "ReconcileError", err.Error())
		return r.updateStatusError(ctx, &instance, "DeploymentError", err)
	}
	if resources.NeedsGitClone(merged) && (depOp == controllerutil.OperationResultCreated || depOp == controllerutil.OperationResultUpdated) {
		r.Recorder.Event(&instance, corev1.EventTypeNormal, "WorkspaceClone",
			fmt.Sprintf("Workspace git clone configured for %s", merged.Spec.Workspace.GitRepo))
	}

	// Check Deployment readiness before declaring Running.
	var currentDep appsv1.Deployment
	if err := r.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: dep.Namespace}, &currentDep); err != nil {
		return r.updateStatusError(ctx, &instance, "DeploymentError", err)
	}
	deploymentReady := currentDep.Status.AvailableReplicas > 0
	if deploymentReady {
		setCondition(&instance, ConditionDeploymentReady, metav1.ConditionTrue, "Available", "Deployment has available replicas")
	} else {
		setCondition(&instance, ConditionDeploymentReady, metav1.ConditionFalse, "Progressing", "Deployment is rolling out")
	}

	// 8. Create/update Service.
	svc := resources.BuildService(merged, namespace)
	if err := r.reconcileService(ctx, &instance, svc); err != nil {
		return r.updateStatusError(ctx, &instance, "ServiceError", err)
	}

	// 9. Create/update MCPServer CRD in muster namespace.
	if err := r.reconcileMCPServer(ctx, merged, namespace); err != nil {
		// MCPServer creation failure is not fatal -- log and continue.
		logger.Error(err, "failed to reconcile MCPServer CRD")
		r.Recorder.Event(&instance, corev1.EventTypeWarning, "MCPServerError", err.Error())
		setCondition(&instance, ConditionMCPServerReady, metav1.ConditionFalse, "ReconcileError", err.Error())
	} else {
		setCondition(&instance, ConditionMCPServerReady, metav1.ConditionTrue, "Reconciled", "MCPServer CRD reconciled")
	}

	// 10. Update status. Use the merged spec for status computation (plugin
	// counts, mode) so the status reflects the effective configuration.
	if deploymentReady {
		return r.updateStatusRunning(ctx, &instance, namespace, resolvedImage)
	}
	return r.updateStatusPending(ctx, &instance, namespace, resolvedImage)
}

func (r *KlausInstanceReconciler) ensureNamespace(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) error {
	desired := resources.BuildNamespace(instance)
	existing := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		if existing.Labels == nil {
			existing.Labels = make(map[string]string)
		}
		for k, v := range desired.Labels {
			existing.Labels[k] = v
		}
		return nil
	})
	return err
}

// copyAPIKeySecret copies the shared Anthropic API key Secret into the
// instance namespace. Returns (true, nil) on success, (false, nil) if the
// source secret does not exist yet, or (false, err) on failure.
func (r *KlausInstanceReconciler) copyAPIKeySecret(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) (bool, error) {
	// Read the shared org secret.
	srcSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      r.AnthropicKeySecret,
		Namespace: r.AnthropicKeyNs,
	}, srcSecret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("fetching Anthropic API key secret: %w", err)
	}

	apiKey, ok := srcSecret.Data["api-key"]
	if !ok {
		return false, fmt.Errorf("Anthropic API key secret missing 'api-key' field")
	}

	// Create or update the secret in the instance namespace.
	desired := resources.BuildAPIKeySecret(instance, namespace, apiKey)
	existing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: namespace}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Data = desired.Data
		existing.Labels = desired.Labels
		return nil
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *KlausInstanceReconciler) reconcileConfigMap(ctx context.Context, instance *klausv1alpha1.KlausInstance, desired *corev1.ConfigMap) error {
	existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Data = desired.Data
		existing.Labels = desired.Labels
		return nil
	})
	if op == controllerutil.OperationResultCreated {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "CreatingConfigMap", "Created ConfigMap "+desired.Name)
	}
	return err
}

func (r *KlausInstanceReconciler) reconcilePVC(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) error {
	pvc := resources.BuildPVC(instance, namespace)
	if pvc == nil {
		return nil
	}

	existing := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: namespace}, existing)
	if apierrors.IsNotFound(err) {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "CreatingPVC", "Creating PVC "+pvc.Name)
		return r.Create(ctx, pvc)
	}
	// PVCs cannot be updated (immutable spec), so we just return.
	return err
}

func (r *KlausInstanceReconciler) ensureServiceAccount(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) error {
	existing := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: instance.Name, Namespace: namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Labels = resources.InstanceLabels(instance)
		return nil
	})
	return err
}

func (r *KlausInstanceReconciler) reconcileDeployment(ctx context.Context, instance *klausv1alpha1.KlausInstance, desired *appsv1.Deployment) (controllerutil.OperationResult, error) {
	existing := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
		return nil
	})
	if op == controllerutil.OperationResultCreated {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "CreatingDeployment", "Created Deployment "+desired.Name)
	}
	return op, err
}

func (r *KlausInstanceReconciler) reconcileService(ctx context.Context, instance *klausv1alpha1.KlausInstance, desired *corev1.Service) error {
	existing := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		// Preserve ClusterIP on update.
		clusterIP := existing.Spec.ClusterIP
		existing.Spec = desired.Spec
		existing.Spec.ClusterIP = clusterIP
		existing.Labels = desired.Labels
		return nil
	})
	if op == controllerutil.OperationResultCreated {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "CreatingService", "Created Service "+desired.Name)
	}
	return err
}

func (r *KlausInstanceReconciler) reconcileMCPServer(ctx context.Context, instance *klausv1alpha1.KlausInstance, instanceNamespace string) error {
	desired := resources.BuildMCPServerCRD(instance, instanceNamespace)

	musterNamespace := resources.MusterNamespace(instance)
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(mcpServerGVK)
	existing.SetName("klaus-" + instance.Name)
	existing.SetNamespace(musterNamespace)

	err := r.Get(ctx, types.NamespacedName{
		Name:      "klaus-" + instance.Name,
		Namespace: musterNamespace,
	}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update the spec and labels using typed accessors to avoid panics.
	existing.Object["spec"] = desired.Object["spec"]
	existing.SetLabels(desired.GetLabels())
	return r.Update(ctx, existing)
}

func (r *KlausInstanceReconciler) reconcileDelete(ctx context.Context, instance *klausv1alpha1.KlausInstance) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling deletion", "instance", instance.Name)

	namespace := resources.UserNamespace(instance.Spec.Owner)

	// Clean up in-namespace resources. These are not garbage-collected via
	// owner references because they live in a different namespace than the
	// KlausInstance.
	inNamespaceResources := []client.Object{
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
			Name: instance.Name, Namespace: namespace,
		}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{
			Name: resources.ServiceName(instance), Namespace: namespace,
		}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{
			Name: resources.ConfigMapName(instance), Namespace: namespace,
		}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{
			Name: resources.SecretName(instance), Namespace: namespace,
		}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
			Name: instance.Name, Namespace: namespace,
		}},
	}

	// PVC only exists if workspace was configured.
	if instance.Spec.Workspace != nil {
		inNamespaceResources = append(inNamespaceResources, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: resources.PVCName(instance), Namespace: namespace,
			},
		})
	}

	// Git credential secret only exists if gitSecretRef was configured.
	if resources.NeedsGitSecret(instance) {
		inNamespaceResources = append(inNamespaceResources, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: resources.GitSecretName(instance), Namespace: namespace,
			},
		})
	}

	var errs []error

	// Clean up stale MCP secrets, respecting multi-instance ownership. This
	// only removes secrets no longer referenced by any non-deleting instance
	// for the same owner.
	if err := r.cleanupStaleMCPSecrets(ctx, instance.Spec.Owner, namespace); err != nil {
		logger.Error(err, "failed to clean up stale MCP secrets")
		errs = append(errs, err)
	}
	for _, obj := range inNamespaceResources {
		if err := r.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
			logger.Error(err, "failed to delete resource",
				"kind", fmt.Sprintf("%T", obj),
				"name", obj.GetName(),
				"namespace", obj.GetNamespace(),
			)
			errs = append(errs, err)
		}
	}

	// Clean up cross-namespace MCPServer CRD.
	musterNamespace := resources.MusterNamespace(instance)

	mcpServer := &unstructured.Unstructured{}
	mcpServer.SetGroupVersionKind(mcpServerGVK)
	mcpServer.SetName("klaus-" + instance.Name)
	mcpServer.SetNamespace(musterNamespace)

	if err := r.Delete(ctx, mcpServer); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "failed to delete MCPServer CRD")
		errs = append(errs, err)
	}

	// Only remove the finalizer once all child resources are confirmed deleted.
	if len(errs) > 0 {
		return ctrl.Result{}, fmt.Errorf("cleaning up child resources: %w", errors.Join(errs...))
	}

	// Remove finalizer.
	controllerutil.RemoveFinalizer(instance, finalizerName)
	if err := r.Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *KlausInstanceReconciler) updateStatusError(ctx context.Context, instance *klausv1alpha1.KlausInstance, reason string, err error) (ctrl.Result, error) {
	instance.Status.State = klausv1alpha1.InstanceStateError
	instance.Status.ObservedGeneration = instance.Generation
	setCondition(instance, ConditionReady, metav1.ConditionFalse, reason, err.Error())
	_ = r.Status().Update(ctx, instance)
	r.Recorder.Event(instance, corev1.EventTypeWarning, reason, err.Error())
	return ctrl.Result{}, err
}

func (r *KlausInstanceReconciler) populateCommonStatus(instance *klausv1alpha1.KlausInstance, namespace, resolvedImage string) {
	instance.Status.Endpoint = resources.ServiceEndpoint(instance, namespace)
	instance.Status.PluginCount = len(instance.Spec.Plugins)
	instance.Status.MCPServerCount = len(instance.Spec.MCPServers) + len(instance.Spec.Claude.MCPServers)
	instance.Status.ObservedGeneration = instance.Generation

	if instance.Spec.Claude.PersistentMode != nil && *instance.Spec.Claude.PersistentMode {
		instance.Status.Mode = klausv1alpha1.InstanceModePersistent
	} else {
		instance.Status.Mode = klausv1alpha1.InstanceModeSingleShot
	}

	if instance.Spec.PersonalityRef != nil {
		instance.Status.Personality = instance.Spec.PersonalityRef.Name
	} else {
		instance.Status.Personality = ""
	}

	// Report the resolved image when it differs from the operator default.
	if resolvedImage != r.KlausImage {
		instance.Status.Toolchain = resolvedImage
	} else {
		instance.Status.Toolchain = ""
	}
}

func (r *KlausInstanceReconciler) updateStatusRunning(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace, resolvedImage string) (ctrl.Result, error) {
	instance.Status.State = klausv1alpha1.InstanceStateRunning
	r.populateCommonStatus(instance, namespace, resolvedImage)
	setCondition(instance, ConditionReady, metav1.ConditionTrue, "Reconciled", "All resources reconciled successfully")

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *KlausInstanceReconciler) updateStatusPending(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace, resolvedImage string) (ctrl.Result, error) {
	instance.Status.State = klausv1alpha1.InstanceStatePending
	r.populateCommonStatus(instance, namespace, resolvedImage)
	setCondition(instance, ConditionReady, metav1.ConditionFalse, "Progressing", "Waiting for Deployment to become available")

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}
	// Requeue to check deployment readiness again.
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// resolveMCPServers fetches referenced KlausMCPServer CRDs, converts their
// configs to JSON, and merges them into the instance spec. Their secretRefs are
// also injected (deduplicated) and the source Secrets are copied to the user
// namespace so that secretKeyRef env vars can resolve at pod startup.
//
// This function also:
//   - Checks the KlausMCPServer Ready condition to fail fast with a clear
//     message when a referenced server is misconfigured or has missing secrets.
//   - Detects secret name collisions across MCP servers that would cause
//     conflicts in the user namespace.
//   - Cleans up stale MCP secrets no longer referenced by any instance.
func (r *KlausInstanceReconciler) resolveMCPServers(ctx context.Context, instance *klausv1alpha1.KlausInstance) error {
	if len(instance.Spec.MCPServers) == 0 {
		return nil
	}

	resolved := &resources.ResolvedMCPConfig{
		Servers: make(map[string]runtime.RawExtension, len(instance.Spec.MCPServers)),
	}

	namespace := resources.UserNamespace(instance.Spec.Owner)

	// Track which MCP servers own each secret name to detect collisions.
	secretOwners := make(map[string]string)

	for _, ref := range instance.Spec.MCPServers {
		var server klausv1alpha1.KlausMCPServer
		if err := r.Get(ctx, types.NamespacedName{
			Name:      ref.Name,
			Namespace: instance.Namespace,
		}, &server); err != nil {
			return fmt.Errorf("resolving MCP server %q: %w", ref.Name, err)
		}

		// Check if the MCP server is ready. If the controller has explicitly
		// marked it as not ready (e.g. missing secrets, invalid spec), fail
		// fast with a clear message instead of proceeding to copy potentially
		// missing secrets. Servers that haven't been reconciled yet (no
		// conditions) are allowed through.
		readyCond := apimeta.FindStatusCondition(server.Status.Conditions, MCPServerConditionReady)
		if readyCond != nil && readyCond.Status == metav1.ConditionFalse {
			return fmt.Errorf("MCP server %q is not ready: %s", ref.Name, readyCond.Message)
		}

		// Convert the server spec to a RawExtension for .mcp.json assembly.
		rawConfig, err := resources.ServerConfigToRawExtension(&server.Spec)
		if err != nil {
			return fmt.Errorf("marshaling MCP server %q config: %w", ref.Name, err)
		}
		resolved.Servers[ref.Name] = rawConfig

		// Collect secretRefs and detect secret name collisions. Two different
		// MCP servers referencing secrets with the same name would silently
		// overwrite each other in the user namespace.
		resolved.Secrets = append(resolved.Secrets, server.Spec.SecretRefs...)
		for _, secretRef := range server.Spec.SecretRefs {
			if prevOwner, exists := secretOwners[secretRef.SecretName]; exists && prevOwner != ref.Name {
				return fmt.Errorf(
					"secret name collision: secret %q is referenced by both MCP servers %q and %q; "+
						"use uniquely-named secrets to avoid conflicts in the user namespace",
					secretRef.SecretName, prevOwner, ref.Name,
				)
			}
			secretOwners[secretRef.SecretName] = ref.Name
		}

		// Copy referenced Secrets from operator namespace to user namespace.
		for _, secretRef := range server.Spec.SecretRefs {
			if err := r.copyMCPSecret(ctx, instance, secretRef.SecretName, namespace); err != nil {
				return fmt.Errorf("copying MCP secret %q for server %q: %w",
					secretRef.SecretName, ref.Name, err)
			}
		}
	}

	// Clean up stale MCP secrets that are no longer referenced by any
	// non-deleting instance for the same owner.
	if err := r.cleanupStaleMCPSecrets(ctx, instance.Spec.Owner, namespace); err != nil {
		return fmt.Errorf("cleaning up stale MCP secrets: %w", err)
	}

	resources.MergeResolvedMCPIntoInstance(resolved, &instance.Spec)
	return nil
}

// copyGitSecret copies the workspace git credential Secret from the operator
// namespace to the user namespace so the git-clone init container can access it.
// Returns OperationResultNone when gitSecretRef is not configured.
func (r *KlausInstanceReconciler) copyGitSecret(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) (controllerutil.OperationResult, error) {
	if !resources.NeedsGitSecret(instance) {
		return controllerutil.OperationResultNone, nil
	}

	srcName := instance.Spec.Workspace.GitSecretRef.Name
	srcSecret := &corev1.Secret{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      srcName,
		Namespace: instance.Namespace,
	}, srcSecret); err != nil {
		return controllerutil.OperationResultNone, fmt.Errorf("fetching git secret %q: %w", srcName, err)
	}

	desired := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:      resources.GitSecretName(instance),
		Namespace: namespace,
	}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, desired, func() error {
		desired.Type = srcSecret.Type
		desired.Data = srcSecret.Data
		desired.Labels = resources.InstanceLabels(instance)
		return nil
	})
	if err != nil {
		return controllerutil.OperationResultNone, fmt.Errorf("reconciling git secret copy: %w", err)
	}
	return op, nil
}

// copyMCPSecret copies a Secret from the operator namespace to the target user
// namespace, ensuring that secretKeyRef env vars on the instance pod can resolve.
// Labels are owner-scoped (not instance-specific) because multiple instances
// for the same owner may share the same MCP secret.
func (r *KlausInstanceReconciler) copyMCPSecret(ctx context.Context, instance *klausv1alpha1.KlausInstance, secretName, targetNamespace string) error {
	srcSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      secretName,
		Namespace: instance.Namespace,
	}, srcSecret)
	if err != nil {
		return fmt.Errorf("fetching source secret: %w", err)
	}

	existing := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{
		Name:      secretName,
		Namespace: targetNamespace,
	}}
	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Data = srcSecret.Data
		existing.Labels = resources.MCPSecretLabels(instance.Spec.Owner)
		return nil
	})
	return err
}

// cleanupStaleMCPSecrets removes MCP secrets from the user namespace that are
// no longer referenced by any non-deleting KlausInstance for the same owner.
// This handles the case where a KlausMCPServer's secretRefs change or an
// instance removes a reference to an MCP server.
func (r *KlausInstanceReconciler) cleanupStaleMCPSecrets(ctx context.Context, owner, namespace string) error {
	logger := log.FromContext(ctx)

	// Build a lookup of MCP server name -> secret names.
	var serverList klausv1alpha1.KlausMCPServerList
	if err := r.List(ctx, &serverList, client.InNamespace(r.OperatorNamespace)); err != nil {
		return fmt.Errorf("listing MCP servers: %w", err)
	}
	serverSecrets := make(map[string][]string, len(serverList.Items))
	for _, server := range serverList.Items {
		for _, ref := range server.Spec.SecretRefs {
			serverSecrets[server.Name] = append(serverSecrets[server.Name], ref.SecretName)
		}
	}

	// Collect the desired set of MCP secret names across all non-deleting
	// instances that share the same user namespace.
	desiredSecrets := make(map[string]bool)
	var instanceList klausv1alpha1.KlausInstanceList
	if err := r.List(ctx, &instanceList, client.InNamespace(r.OperatorNamespace)); err != nil {
		return fmt.Errorf("listing instances: %w", err)
	}
	for _, inst := range instanceList.Items {
		if resources.UserNamespace(inst.Spec.Owner) != namespace || !inst.DeletionTimestamp.IsZero() {
			continue
		}
		for _, ref := range inst.Spec.MCPServers {
			for _, secretName := range serverSecrets[ref.Name] {
				desiredSecrets[secretName] = true
			}
		}
	}

	// List existing MCP secrets in the user namespace.
	var secretList corev1.SecretList
	if err := r.List(ctx, &secretList,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"app.kubernetes.io/component":  "mcp-secret",
			"app.kubernetes.io/managed-by": "klaus-operator",
		},
	); err != nil {
		return fmt.Errorf("listing MCP secrets: %w", err)
	}

	for i := range secretList.Items {
		if !desiredSecrets[secretList.Items[i].Name] {
			logger.Info("deleting stale MCP secret",
				"secret", secretList.Items[i].Name, "namespace", namespace)
			if err := r.Delete(ctx, &secretList.Items[i]); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting stale MCP secret %q: %w", secretList.Items[i].Name, err)
			}
		}
	}
	return nil
}

// resolvePersonality fetches the referenced KlausPersonality and merges its
// spec into the instance spec. If no personalityRef is set, this is a no-op.
func (r *KlausInstanceReconciler) resolvePersonality(ctx context.Context, instance *klausv1alpha1.KlausInstance) error {
	if instance.Spec.PersonalityRef == nil {
		return nil
	}

	var personality klausv1alpha1.KlausPersonality
	if err := r.Get(ctx, types.NamespacedName{
		Name:      instance.Spec.PersonalityRef.Name,
		Namespace: instance.Namespace,
	}, &personality); err != nil {
		return fmt.Errorf("resolving personality %q: %w", instance.Spec.PersonalityRef.Name, err)
	}

	resources.MergePersonalityIntoInstance(&personality.Spec, &instance.Spec)
	return nil
}

// SetupWithManager sets up the controller with the Manager.
// We use label-based watches with Watches() instead of Owns() because child
// resources live in different namespaces (user namespaces) from the parent
// KlausInstance (operator namespace). Owns() relies on owner references which
// cannot cross namespace boundaries.
//
// We also watch KlausPersonality resources so that changes to a personality
// trigger reconciliation of all instances that reference it.
func (r *KlausInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	managedByPredicate, err := predicate.LabelSelectorPredicate(metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/managed-by": "klaus-operator",
		},
	})
	if err != nil {
		return fmt.Errorf("creating label selector predicate: %w", err)
	}

	mapToInstance := handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, obj client.Object) []reconcile.Request {
			instanceName := obj.GetLabels()["app.kubernetes.io/instance"]
			if instanceName == "" {
				return nil
			}
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{
					Name:      instanceName,
					Namespace: r.OperatorNamespace,
				},
			}}
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&klausv1alpha1.KlausInstance{}).
		Watches(&appsv1.Deployment{}, mapToInstance,
			builder.WithPredicates(managedByPredicate)).
		Watches(&corev1.Service{}, mapToInstance,
			builder.WithPredicates(managedByPredicate)).
		Watches(&corev1.ConfigMap{}, mapToInstance,
			builder.WithPredicates(managedByPredicate)).
		Watches(&klausv1alpha1.KlausPersonality{},
			handler.EnqueueRequestsFromMapFunc(EnqueueReferencingInstances(r.Client, r.OperatorNamespace)),
		).
		Watches(&klausv1alpha1.KlausMCPServer{},
			handler.EnqueueRequestsFromMapFunc(EnqueueReferencingMCPServerInstances(r.Client, r.OperatorNamespace)),
		).
		Named("klausinstance").
		Complete(r)
}
