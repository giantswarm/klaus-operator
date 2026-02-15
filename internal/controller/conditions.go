package controller

import (
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
	now := metav1.Now()
	condition := metav1.Condition{
		Type:               condType,
		Status:             status,
		ObservedGeneration: instance.Generation,
		LastTransitionTime: now,
		Reason:             reason,
		Message:            message,
	}

	// Find and update existing condition, or append.
	for i, existing := range instance.Status.Conditions {
		if existing.Type == condType {
			// Only update LastTransitionTime if the status changed.
			if existing.Status == status {
				condition.LastTransitionTime = existing.LastTransitionTime
			}
			instance.Status.Conditions[i] = condition
			return
		}
	}

	instance.Status.Conditions = append(instance.Status.Conditions, condition)
}
