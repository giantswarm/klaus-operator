package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	// Validate the spec.
	if err := resources.ValidateSpec(&instance); err != nil {
		return r.updateStatusError(ctx, &instance, "ValidationError", err)
	}

	// Determine the target namespace.
	namespace := resources.UserNamespace(instance.Spec.Owner)

	logger.Info("reconciling KlausInstance",
		"instance", instance.Name,
		"owner", instance.Spec.Owner,
		"namespace", namespace,
	)

	// 1. Ensure namespace exists.
	if err := r.ensureNamespace(ctx, &instance, namespace); err != nil {
		return r.updateStatusError(ctx, &instance, "NamespaceError", err)
	}

	// 2. Copy the Anthropic API key Secret.
	found, err := r.copyAPIKeySecret(ctx, &instance, namespace)
	if err != nil {
		return r.updateStatusError(ctx, &instance, "SecretError", err)
	}
	if !found {
		logger.Info("Anthropic API key secret not found, requeuing")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
	}

	// 3. Create/update ConfigMap.
	cm, err := resources.BuildConfigMap(&instance, namespace)
	if err != nil {
		setCondition(&instance, ConditionConfigReady, metav1.ConditionFalse, "BuildError", err.Error())
		return r.updateStatusError(ctx, &instance, "ConfigMapError", err)
	}
	if err := r.reconcileConfigMap(ctx, &instance, cm); err != nil {
		setCondition(&instance, ConditionConfigReady, metav1.ConditionFalse, "ReconcileError", err.Error())
		return r.updateStatusError(ctx, &instance, "ConfigMapError", err)
	}
	setCondition(&instance, ConditionConfigReady, metav1.ConditionTrue, "Reconciled", "ConfigMap reconciled")

	// 4. Create/update PVC (if workspace configured).
	if err := r.reconcilePVC(ctx, &instance, namespace); err != nil {
		return r.updateStatusError(ctx, &instance, "PVCError", err)
	}

	// 5. Ensure ServiceAccount.
	if err := r.ensureServiceAccount(ctx, &instance, namespace); err != nil {
		return r.updateStatusError(ctx, &instance, "ServiceAccountError", err)
	}

	// 6. Create/update Deployment.
	dep := resources.BuildDeployment(&instance, namespace, r.KlausImage, cm.Data)
	if err := r.reconcileDeployment(ctx, &instance, dep); err != nil {
		setCondition(&instance, ConditionDeploymentReady, metav1.ConditionFalse, "ReconcileError", err.Error())
		return r.updateStatusError(ctx, &instance, "DeploymentError", err)
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

	// 7. Create/update Service.
	svc := resources.BuildService(&instance, namespace)
	if err := r.reconcileService(ctx, &instance, svc); err != nil {
		return r.updateStatusError(ctx, &instance, "ServiceError", err)
	}

	// 8. Create/update MCPServer CRD in muster namespace.
	if err := r.reconcileMCPServer(ctx, &instance, namespace); err != nil {
		// MCPServer creation failure is not fatal -- log and continue.
		logger.Error(err, "failed to reconcile MCPServer CRD")
		r.Recorder.Event(&instance, corev1.EventTypeWarning, "MCPServerError", err.Error())
		setCondition(&instance, ConditionMCPServerReady, metav1.ConditionFalse, "ReconcileError", err.Error())
	} else {
		setCondition(&instance, ConditionMCPServerReady, metav1.ConditionTrue, "Reconciled", "MCPServer CRD reconciled")
	}

	// 9. Update status.
	if deploymentReady {
		return r.updateStatusRunning(ctx, &instance, namespace)
	}
	return r.updateStatusPending(ctx, &instance, namespace)
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

func (r *KlausInstanceReconciler) reconcileDeployment(ctx context.Context, instance *klausv1alpha1.KlausInstance, desired *appsv1.Deployment) error {
	existing := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: desired.Name, Namespace: desired.Namespace}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
		return nil
	})
	if op == controllerutil.OperationResultCreated {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "CreatingDeployment", "Created Deployment "+desired.Name)
	}
	return err
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

	var errs []error
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

func (r *KlausInstanceReconciler) populateCommonStatus(instance *klausv1alpha1.KlausInstance, namespace string) {
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
}

func (r *KlausInstanceReconciler) updateStatusRunning(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) (ctrl.Result, error) {
	instance.Status.State = klausv1alpha1.InstanceStateRunning
	r.populateCommonStatus(instance, namespace)
	setCondition(instance, ConditionReady, metav1.ConditionTrue, "Reconciled", "All resources reconciled successfully")

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *KlausInstanceReconciler) updateStatusPending(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) (ctrl.Result, error) {
	instance.Status.State = klausv1alpha1.InstanceStatePending
	r.populateCommonStatus(instance, namespace)
	setCondition(instance, ConditionReady, metav1.ConditionFalse, "Progressing", "Waiting for Deployment to become available")

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}
	// Requeue to check deployment readiness again.
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

// SetupWithManager sets up the controller with the Manager.
// We use label-based watches with Watches() instead of Owns() because child
// resources live in different namespaces (user namespaces) from the parent
// KlausInstance (operator namespace). Owns() relies on owner references which
// cannot cross namespace boundaries.
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
		Named("klausinstance").
		Complete(r)
}
