package controller

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// Condition types for KlausInstance.
const (
	// ConditionReady indicates the instance is fully reconciled and running.
	ConditionReady = "Ready"

	// ConditionConfigReady indicates the ConfigMap has been created/updated.
	ConditionConfigReady = "ConfigReady"

	// ConditionDeploymentReady indicates the Deployment has been created/updated.
	ConditionDeploymentReady = "DeploymentReady"

	// ConditionMCPServerReady indicates the MCPServer CRD has been created in muster.
	ConditionMCPServerReady = "MCPServerReady"
)

// setCondition updates or appends a condition on the instance status.
func setCondition(instance *klausv1alpha1.KlausInstance, condType string, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&instance.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: instance.Generation,
		Reason:             reason,
		Message:            message,
	})
}
