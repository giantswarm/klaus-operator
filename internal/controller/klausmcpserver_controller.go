package controller

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// Condition types for KlausMCPServer.
const (
	// MCPServerConditionReady indicates the MCP server config is valid and usable.
	MCPServerConditionReady = "Ready"

	// MCPServerConditionSecretsValid indicates all referenced Secrets exist.
	MCPServerConditionSecretsValid = "SecretsValid"
)

// KlausMCPServerReconciler reconciles a KlausMCPServer object.
type KlausMCPServerReconciler struct {
	client.Client
	Scheme            *runtime.Scheme
	Recorder          record.EventRecorder
	OperatorNamespace string
}

// +kubebuilder:rbac:groups=klaus.giantswarm.io,resources=klausmcpservers,verbs=get;list;watch
// +kubebuilder:rbac:groups=klaus.giantswarm.io,resources=klausmcpservers/status,verbs=get;update;patch

// Reconcile handles a KlausMCPServer event.
func (r *KlausMCPServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var server klausv1alpha1.KlausMCPServer
	if err := r.Get(ctx, req.NamespacedName, &server); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("reconciling KlausMCPServer", "name", server.Name)

	// Validate spec. Validation errors are permanent (the user must fix the
	// spec), so we update the status condition and return nil to avoid
	// unnecessary requeuing with backoff.
	if err := r.validateSpec(&server); err != nil {
		apimeta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
			Type:               MCPServerConditionReady,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: server.Generation,
			Reason:             "ValidationError",
			Message:            err.Error(),
		})
		r.Recorder.Event(&server, corev1.EventTypeWarning, "ValidationError", err.Error())

		server.Status.ObservedGeneration = server.Generation
		if statusErr := r.Status().Update(ctx, &server); statusErr != nil {
			logger.Error(statusErr, "failed to update status after validation error")
			return ctrl.Result{}, statusErr
		}
		return ctrl.Result{}, nil
	}

	// Validate referenced Secrets exist in the operator namespace.
	secretsValid := true
	if err := r.validateSecrets(ctx, &server); err != nil {
		secretsValid = false
		apimeta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
			Type:               MCPServerConditionSecretsValid,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: server.Generation,
			Reason:             "SecretNotFound",
			Message:            err.Error(),
		})
		r.Recorder.Event(&server, corev1.EventTypeWarning, "SecretNotFound", err.Error())
	} else {
		apimeta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
			Type:               MCPServerConditionSecretsValid,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: server.Generation,
			Reason:             "Valid",
			Message:            "All referenced Secrets exist",
		})
	}

	// Count referencing instances. A transient error here would reset the
	// count to 0 in the status, so we return the error to requeue.
	instanceCount, err := r.countReferencingInstances(ctx, server.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("counting referencing instances: %w", err)
	}

	// Update status.
	server.Status.InstanceCount = instanceCount
	server.Status.ObservedGeneration = server.Generation

	readyStatus := metav1.ConditionTrue
	readyReason := "Reconciled"
	readyMessage := "MCP server is ready"
	if !secretsValid {
		readyStatus = metav1.ConditionFalse
		readyReason = "SecretsInvalid"
		readyMessage = "One or more referenced Secrets are missing"
	}
	apimeta.SetStatusCondition(&server.Status.Conditions, metav1.Condition{
		Type:               MCPServerConditionReady,
		Status:             readyStatus,
		ObservedGeneration: server.Generation,
		Reason:             readyReason,
		Message:            readyMessage,
	})

	if err := r.Status().Update(ctx, &server); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// validateSpec performs basic validation on the KlausMCPServer spec.
func (r *KlausMCPServerReconciler) validateSpec(server *klausv1alpha1.KlausMCPServer) error {
	if server.Spec.Type == "" {
		return fmt.Errorf("spec.type is required")
	}

	switch server.Spec.Type {
	case "streamable-http", "sse", "http":
		if server.Spec.URL == "" {
			return fmt.Errorf("spec.url is required for type %q", server.Spec.Type)
		}
	case "stdio":
		if server.Spec.Command == "" {
			return fmt.Errorf("spec.command is required for type %q", server.Spec.Type)
		}
	default:
		return fmt.Errorf("unsupported server type %q (valid types: streamable-http, sse, http, stdio)", server.Spec.Type)
	}

	return nil
}

// validateSecrets checks that all referenced Secrets exist in the operator namespace.
func (r *KlausMCPServerReconciler) validateSecrets(ctx context.Context, server *klausv1alpha1.KlausMCPServer) error {
	for _, secretRef := range server.Spec.SecretRefs {
		var secret corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{
			Name:      secretRef.SecretName,
			Namespace: server.Namespace,
		}, &secret); err != nil {
			return fmt.Errorf("secret %q: %w", secretRef.SecretName, err)
		}
	}
	return nil
}

// countReferencingInstances counts KlausInstance resources that reference this
// MCP server by name.
func (r *KlausMCPServerReconciler) countReferencingInstances(ctx context.Context, serverName string) (int, error) {
	var instanceList klausv1alpha1.KlausInstanceList
	if err := r.List(ctx, &instanceList, client.InNamespace(r.OperatorNamespace)); err != nil {
		return 0, err
	}

	count := 0
	for _, inst := range instanceList.Items {
		for _, ref := range inst.Spec.MCPServers {
			if ref.Name == serverName {
				count++
				break
			}
		}
	}
	return count, nil
}

// SetupWithManager sets up the controller with the Manager.
// Watches KlausInstance changes to update instance counts and Secret changes
// to re-validate secret references (e.g., when a missing Secret is created).
func (r *KlausMCPServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&klausv1alpha1.KlausMCPServer{}).
		Watches(&klausv1alpha1.KlausInstance{},
			handler.EnqueueRequestsFromMapFunc(r.mapInstanceToMCPServers),
		).
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToMCPServers),
		).
		Named("klausmcpserver").
		Complete(r)
}

// mapInstanceToMCPServers maps a KlausInstance to the KlausMCPServers it references,
// triggering status updates (instance count) when instances are created/deleted.
func (r *KlausMCPServerReconciler) mapInstanceToMCPServers(_ context.Context, obj client.Object) []reconcile.Request {
	instance, ok := obj.(*klausv1alpha1.KlausInstance)
	if !ok {
		return nil
	}

	var requests []reconcile.Request
	for _, ref := range instance.Spec.MCPServers {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      ref.Name,
				Namespace: instance.Namespace,
			},
		})
	}
	return requests
}

// mapSecretToMCPServers maps a Secret to any KlausMCPServer resources that
// reference it via secretRefs. This ensures that when a previously-missing
// Secret is created, the MCP server's SecretsValid condition is re-evaluated.
func (r *KlausMCPServerReconciler) mapSecretToMCPServers(ctx context.Context, obj client.Object) []reconcile.Request {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return nil
	}

	// Only consider Secrets in the operator namespace.
	if secret.Namespace != r.OperatorNamespace {
		return nil
	}

	var serverList klausv1alpha1.KlausMCPServerList
	if err := r.List(ctx, &serverList, client.InNamespace(r.OperatorNamespace)); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, server := range serverList.Items {
		for _, ref := range server.Spec.SecretRefs {
			if ref.SecretName == secret.Name {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      server.Name,
						Namespace: server.Namespace,
					},
				})
				break
			}
		}
	}
	return requests
}

// EnqueueReferencingMCPServerInstances returns reconcile requests for all
// KlausInstance resources that reference the given MCP server. Called by the
// KlausInstanceReconciler's SetupWithManager to watch MCP server changes.
func EnqueueReferencingMCPServerInstances(c client.Client, operatorNamespace string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		server, ok := obj.(*klausv1alpha1.KlausMCPServer)
		if !ok {
			return nil
		}

		var instanceList klausv1alpha1.KlausInstanceList
		if err := c.List(ctx, &instanceList, client.InNamespace(operatorNamespace)); err != nil {
			return nil
		}

		var requests []reconcile.Request
		for _, inst := range instanceList.Items {
			for _, ref := range inst.Spec.MCPServers {
				if ref.Name == server.Name {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Name:      inst.Name,
							Namespace: inst.Namespace,
						},
					})
					break
				}
			}
		}
		return requests
	}
}
