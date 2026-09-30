package resources

import (
	"fmt"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildDeployment creates the Deployment for a KlausInstance, mirroring the
// standalone Helm chart's deployment.yaml rendering.
func BuildDeployment(instance *klausv1alpha1.KlausInstance, namespace, klausImage, gitCloneImage string, configMapData map[string]string) *appsv1.Deployment {
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

	initContainers := buildGitCloneInitContainers(instance, gitCloneImage)

	replicas := int32(1)
	if instance.Spec.Stopped {
		replicas = 0
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(replicas),
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
					InitContainers:     initContainers,
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
							Name:  AppKlaus,
							Image: klausImage,
							Ports: []corev1.ContainerPort{
								{
									Name:          HTTPPortName,
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

// buildGitCloneInitContainers returns init containers for git-cloning the
// workspace repository. Returns nil if no git repo is configured.
//
// The init container runs with ReadOnlyRootFilesystem: true for security
// hardening. A writable /tmp emptyDir is mounted so git has a scratch area
// for index.lock files, credential helpers, and pack negotiation. HOME is
// set to /tmp so git config writes (e.g., safe.directory) land there.
// GIT_CONFIG_NOSYSTEM=1 prevents reading /etc/gitconfig which may not exist.
func buildGitCloneInitContainers(instance *klausv1alpha1.KlausInstance, gitCloneImage string) []corev1.Container {
	if !NeedsGitClone(instance) {
		return nil
	}

	if gitCloneImage == "" {
		gitCloneImage = DefaultGitCloneImage
	}

	ws := instance.Spec.Workspace
	script := buildGitCloneScript(ws.GitRepo, ws.GitRef)

	env := []corev1.EnvVar{
		{Name: "HOME", Value: GitTmpMountPath},
		{Name: "GIT_CONFIG_NOSYSTEM", Value: "1"},
	}

	mounts := []corev1.VolumeMount{
		{Name: WorkspaceVolumeName, MountPath: WorkspaceMountPath},
		{Name: GitTmpVolumeName, MountPath: GitTmpMountPath},
	}
	if NeedsGitSecret(instance) {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      GitSecretVolumeName,
			MountPath: GitSecretMountPath,
			ReadOnly:  true,
		})
		env = append(env, gitCredentialEnv(GitSecretKey(instance))...)
	}

	return []corev1.Container{
		{
			Name:         "git-clone",
			Image:        gitCloneImage,
			Command:      []string{"sh", "-c"},
			Args:         []string{script},
			Env:          env,
			VolumeMounts: mounts,
			SecurityContext: &corev1.SecurityContext{
				RunAsUser:                ptr.To(int64(1000)),
				RunAsGroup:               ptr.To(int64(1000)),
				AllowPrivilegeEscalation: ptr.To(false),
				ReadOnlyRootFilesystem:   ptr.To(true),
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
			},
		},
	}
}

// gitCredentialEnv configures a per-process git credential helper through
// GIT_CONFIG_* environment variables. The helper reads the token from the
// mounted secret each time git authenticates, so the token never appears
// in a remote URL, in .git/config on the workspace PVC or in the pod spec.
func gitCredentialEnv(secretKey string) []corev1.EnvVar {
	keyPath := shellQuote(path.Join(GitSecretMountPath, secretKey))
	helper := fmt.Sprintf(`!f() { test "$1" = get || exit 0; echo username=x-access-token; echo "password=$(cat %s)"; }; f`, keyPath)
	return []corev1.EnvVar{
		{Name: "GIT_TERMINAL_PROMPT", Value: "0"},
		{Name: "GIT_CONFIG_COUNT", Value: "1"},
		{Name: "GIT_CONFIG_KEY_0", Value: "credential.helper"},
		{Name: "GIT_CONFIG_VALUE_0", Value: helper},
	}
}

// buildGitCloneScript generates the shell script for the git-clone init
// container. It handles both fresh clones and incremental updates when the
// PVC already contains a previous checkout. User-supplied values (gitRepo,
// gitRef) are single-quoted to prevent shell injection. CRD validation
// patterns provide an additional layer of defense.
//
// Authentication is not the script's concern: gitCredentialEnv supplies a
// credential helper when a git secret is configured. The update path resets
// origin to gitRepo, which also drops credentials an older operator version
// left in the persisted remote URL.
func buildGitCloneScript(gitRepo, gitRef string) string {
	quotedRepo := shellQuote(gitRepo)
	quotedWs := shellQuote(WorkspaceMountPath)

	// Fail fast on any command error. The update path uses explicit fallback
	// blocks to avoid triggering set -e when git fetch/pull fails on a
	// previously cloned workspace.
	parts := []string{"set -e"}

	// Fresh clone vs incremental update.
	parts = append(parts, fmt.Sprintf("if [ ! -d %s/.git ]; then", quotedWs))
	if gitRef != "" {
		parts = append(parts, fmt.Sprintf("  git clone --branch %s %s %s", shellQuote(gitRef), quotedRepo, quotedWs))
	} else {
		parts = append(parts, fmt.Sprintf("  git clone %s %s", quotedRepo, quotedWs))
	}
	parts = append(parts,
		"else",
		fmt.Sprintf("  cd %s", quotedWs),
		"  git remote set-url origin "+quotedRepo,
	)
	if gitRef != "" {
		quotedRef := shellQuote(gitRef)
		parts = append(parts,
			"  git fetch origin || { echo 'WARNING: git fetch failed, using existing checkout'; exit 0; }",
			fmt.Sprintf("  git checkout %s", quotedRef),
			fmt.Sprintf("  git pull origin %s || echo 'WARNING: git pull failed, using existing checkout'", quotedRef),
		)
	} else {
		parts = append(parts, "  git pull || echo 'WARNING: git pull failed, using existing checkout'")
	}
	parts = append(parts, "fi")

	return strings.Join(parts, "\n")
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
