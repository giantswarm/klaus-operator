package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildNamespace creates the user namespace for a KlausInstance.
func BuildNamespace(instance *klausv1alpha1.KlausInstance) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: UserNamespace(instance.Spec.Owner),
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "klaus-operator",
				"klaus.giantswarm.io/owner":    sanitizeLabelValue(instance.Spec.Owner),
			},
		},
	}
}
