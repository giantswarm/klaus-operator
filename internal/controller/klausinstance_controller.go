package controller

import (
	"context"
	"fmt"

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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
	"github.com/giantswarm/klaus-operator/internal/resources"
)

const finalizerName = "klaus.giantswarm.io/finalizer"

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

	// Ensure finalizer.
	if !controllerutil.ContainsFinalizer(&instance, finalizerName) {
		controllerutil.AddFinalizer(&instance, finalizerName)
		if err := r.Update(ctx, &instance); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Update status to Pending.
	if instance.Status.State == "" {
		instance.Status.State = klausv1alpha1.InstanceStatePending
		if err := r.Status().Update(ctx, &instance); err != nil {
			return ctrl.Result{}, err
		}
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
	apiKey, err := r.copyAPIKeySecret(ctx, &instance, namespace)
	if err != nil {
		return r.updateStatusError(ctx, &instance, "SecretError", err)
	}
	if apiKey == nil {
		logger.Info("Anthropic API key secret not found, requeuing")
		return ctrl.Result{RequeueAfter: 30_000_000_000}, nil // 30s
	}

	// 3. Create/update ConfigMap.
	cm, err := resources.BuildConfigMap(&instance, namespace)
	if err != nil {
		return r.updateStatusError(ctx, &instance, "ConfigMapError", err)
	}
	if err := r.reconcileConfigMap(ctx, &instance, cm); err != nil {
		return r.updateStatusError(ctx, &instance, "ConfigMapError", err)
	}

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
		return r.updateStatusError(ctx, &instance, "DeploymentError", err)
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
	}

	// 9. Update status.
	return r.updateStatusRunning(ctx, &instance, namespace)
}

func (r *KlausInstanceReconciler) ensureNamespace(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) error {
	ns := resources.BuildNamespace(instance)
	existing := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: namespace}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, ns)
	}
	return err
}

func (r *KlausInstanceReconciler) copyAPIKeySecret(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) ([]byte, error) {
	// Read the shared org secret.
	srcSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      r.AnthropicKeySecret,
		Namespace: r.AnthropicKeyNs,
	}, srcSecret)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("fetching Anthropic API key secret: %w", err)
	}

	apiKey, ok := srcSecret.Data["api-key"]
	if !ok {
		return nil, fmt.Errorf("Anthropic API key secret missing 'api-key' field")
	}

	// Create or update the secret in the instance namespace.
	destSecret := resources.BuildAPIKeySecret(instance, namespace, apiKey)
	existing := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{Name: destSecret.Name, Namespace: namespace}, existing)
	if apierrors.IsNotFound(err) {
		return apiKey, r.Create(ctx, destSecret)
	}
	if err != nil {
		return nil, err
	}

	// Update if data changed.
	existing.Data = destSecret.Data
	return apiKey, r.Update(ctx, existing)
}

func (r *KlausInstanceReconciler) reconcileConfigMap(ctx context.Context, instance *klausv1alpha1.KlausInstance, desired *corev1.ConfigMap) error {
	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "CreatingConfigMap", "Creating ConfigMap "+desired.Name)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	existing.Data = desired.Data
	existing.Labels = desired.Labels
	return r.Update(ctx, existing)
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
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: namespace,
			Labels:    resources.InstanceLabels(instance),
		},
	}

	existing := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: sa.Name, Namespace: namespace}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, sa)
	}
	return err
}

func (r *KlausInstanceReconciler) reconcileDeployment(ctx context.Context, instance *klausv1alpha1.KlausInstance, desired *appsv1.Deployment) error {
	existing := &appsv1.Deployment{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "CreatingDeployment", "Creating Deployment "+desired.Name)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update the spec.
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	return r.Update(ctx, existing)
}

func (r *KlausInstanceReconciler) reconcileService(ctx context.Context, instance *klausv1alpha1.KlausInstance, desired *corev1.Service) error {
	existing := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: desired.Name, Namespace: desired.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		r.Recorder.Event(instance, corev1.EventTypeNormal, "CreatingService", "Creating Service "+desired.Name)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Preserve ClusterIP on update.
	desired.Spec.ClusterIP = existing.Spec.ClusterIP
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	return r.Update(ctx, existing)
}

func (r *KlausInstanceReconciler) reconcileMCPServer(ctx context.Context, instance *klausv1alpha1.KlausInstance, instanceNamespace string) error {
	desired := resources.BuildMCPServerCRD(instance, instanceNamespace)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "muster.giantswarm.io",
		Version: "v1alpha1",
		Kind:    "MCPServer",
	})

	musterNamespace := "muster"
	if instance.Spec.Muster != nil && instance.Spec.Muster.Namespace != "" {
		musterNamespace = instance.Spec.Muster.Namespace
	}

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

	// Update the spec.
	existing.Object["spec"] = desired.Object["spec"]
	existing.Object["metadata"].(map[string]any)["labels"] = desired.Object["metadata"].(map[string]any)["labels"]
	return r.Update(ctx, existing)
}

func (r *KlausInstanceReconciler) reconcileDelete(ctx context.Context, instance *klausv1alpha1.KlausInstance) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling deletion", "instance", instance.Name)

	// Clean up cross-namespace MCPServer CRD.
	musterNamespace := "muster"
	if instance.Spec.Muster != nil && instance.Spec.Muster.Namespace != "" {
		musterNamespace = instance.Spec.Muster.Namespace
	}

	mcpServer := &unstructured.Unstructured{}
	mcpServer.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "muster.giantswarm.io",
		Version: "v1alpha1",
		Kind:    "MCPServer",
	})
	mcpServer.SetName("klaus-" + instance.Name)
	mcpServer.SetNamespace(musterNamespace)

	if err := r.Delete(ctx, mcpServer); err != nil && !apierrors.IsNotFound(err) {
		logger.Error(err, "failed to delete MCPServer CRD")
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
	_ = r.Status().Update(ctx, instance)
	r.Recorder.Event(instance, corev1.EventTypeWarning, reason, err.Error())
	return ctrl.Result{}, err
}

func (r *KlausInstanceReconciler) updateStatusRunning(ctx context.Context, instance *klausv1alpha1.KlausInstance, namespace string) (ctrl.Result, error) {
	instance.Status.State = klausv1alpha1.InstanceStateRunning
	instance.Status.Endpoint = resources.ServiceEndpoint(instance, namespace)
	instance.Status.PluginCount = len(instance.Spec.Plugins)
	instance.Status.MCPServerCount = len(instance.Spec.MCPServers) + len(instance.Spec.Claude.MCPServers)
	instance.Status.ObservedGeneration = instance.Generation

	if instance.Spec.Claude.PersistentMode != nil && *instance.Spec.Claude.PersistentMode {
		instance.Status.Mode = "persistent"
	} else {
		instance.Status.Mode = "single-shot"
	}

	if instance.Spec.PersonalityRef != nil {
		instance.Status.Personality = instance.Spec.PersonalityRef.Name
	}

	if err := r.Status().Update(ctx, instance); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KlausInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&klausv1alpha1.KlausInstance{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Named("klausinstance").
		Complete(r)
}
