package resources

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildPVC creates the PersistentVolumeClaim for a KlausInstance workspace.
// Returns nil if workspace is not configured.
func BuildPVC(instance *klausv1alpha1.KlausInstance, namespace string) *corev1.PersistentVolumeClaim {
	if instance.Spec.Workspace == nil {
		return nil
	}

	labels := InstanceLabels(instance)

	// Default size is 5Gi.
	size := resource.MustParse("5Gi")
	if instance.Spec.Workspace.Size != nil {
		size = *instance.Spec.Workspace.Size
	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PVCName(instance),
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: size,
				},
			},
		},
	}

	if instance.Spec.Workspace.StorageClass != "" {
		pvc.Spec.StorageClassName = &instance.Spec.Workspace.StorageClass
	}

	return pvc
}
