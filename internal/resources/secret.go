package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildAPIKeySecret creates a Secret in the instance namespace containing the
// Anthropic API key, copied from the shared org secret.
func BuildAPIKeySecret(instance *klausv1alpha1.KlausInstance, namespace string, apiKey []byte) *corev1.Secret {
	labels := InstanceLabels(instance)

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      SecretName(instance),
			Namespace: namespace,
			Labels:    labels,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"api-key": apiKey,
		},
	}
}
