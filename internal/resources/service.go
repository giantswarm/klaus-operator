package resources

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildService creates the ClusterIP Service for a KlausInstance.
func BuildService(instance *klausv1alpha1.KlausInstance, namespace string) *corev1.Service {
	labels := InstanceLabels(instance)

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ServiceName(instance),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"app.kubernetes.io/name":     "klaus",
				"app.kubernetes.io/instance": instance.Name,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       int32(KlausPort),
					TargetPort: intstr.FromString("http"),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// ServiceEndpoint returns the internal service URL for a KlausInstance.
func ServiceEndpoint(instance *klausv1alpha1.KlausInstance, namespace string) string {
	return "http://" + ServiceName(instance) + "." + namespace + ".svc.cluster.local:" + strconv.Itoa(KlausPort)
}
