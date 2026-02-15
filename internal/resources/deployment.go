package resources

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildDeployment creates the Deployment for a KlausInstance, mirroring the
// standalone Helm chart's deployment.yaml rendering.
func BuildDeployment(instance *klausv1alpha1.KlausInstance, namespace, klausImage string, configMapData map[string]string) *appsv1.Deployment {
	labels := InstanceLabels(instance)
	cmName := ConfigMapName(instance)
	secName := SecretName(instance)

	envVars := BuildEnvVars(instance, cmName, secName)
	volumes := BuildVolumes(instance, cmName)
	volumeMounts := BuildVolumeMounts(instance)

	// Resource requirements (with defaults).
	resources := corev1.ResourceRequirements{}
	if instance.Spec.Resources != nil {
		resources = *instance.Spec.Resources
	}

	// Pod annotations.
	podAnnotations := map[string]string{}
	if configMapData != nil {
		podAnnotations["checksum/config"] = ConfigMapChecksum(configMapData)
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: SelectorLabels(instance),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: instance.Name,
					ImagePullSecrets:   buildImagePullSecrets(instance),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsUser:  ptr.To(int64(1000)),
						RunAsGroup: ptr.To(int64(1000)),
						FSGroup:    ptr.To(int64(1000)),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "klaus",
							Image: klausImage,
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: int32(KlausPort),
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Env:          envVars,
							Resources:    resources,
							VolumeMounts: volumeMounts,
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/healthz",
										Port: intstr.FromInt32(int32(KlausPort)),
									},
								},
								InitialDelaySeconds: 10,
								PeriodSeconds:       30,
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readyz",
										Port: intstr.FromInt32(int32(KlausPort)),
									},
								},
								InitialDelaySeconds: 5,
								PeriodSeconds:       10,
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								// readOnlyRootFilesystem is false because Claude CLI
								// needs write access to npm cache and git state.
								ReadOnlyRootFilesystem: ptr.To(false),
							},
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	return dep
}

// buildImagePullSecrets converts the list of pull secret names to
// LocalObjectReferences for the pod spec.
func buildImagePullSecrets(instance *klausv1alpha1.KlausInstance) []corev1.LocalObjectReference {
	if len(instance.Spec.ImagePullSecrets) == 0 {
		return nil
	}
	refs := make([]corev1.LocalObjectReference, 0, len(instance.Spec.ImagePullSecrets))
	for _, name := range instance.Spec.ImagePullSecrets {
		refs = append(refs, corev1.LocalObjectReference{Name: name})
	}
	return refs
}
