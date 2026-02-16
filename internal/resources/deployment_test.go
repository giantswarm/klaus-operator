package resources

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestBuildDeployment_Basic(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Claude: klausv1alpha1.ClaudeConfig{
				Model:          "claude-sonnet-4-20250514",
				PermissionMode: klausv1alpha1.PermissionModeBypass,
			},
		},
	}

	configData := map[string]string{"system-prompt": "test prompt"}
	dep := BuildDeployment(instance, "klaus-user-test", "gsoci.azurecr.io/giantswarm/klaus:v1.0.0", configData)

	if dep.Name != "test-instance" {
		t.Errorf("Name = %q, want %q", dep.Name, "test-instance")
	}
	if dep.Namespace != "klaus-user-test" {
		t.Errorf("Namespace = %q, want %q", dep.Namespace, "klaus-user-test")
	}
	if *dep.Spec.Replicas != 1 {
		t.Errorf("Replicas = %d, want 1", *dep.Spec.Replicas)
	}

	// Verify selector labels match pod template labels.
	selectorLabels := dep.Spec.Selector.MatchLabels
	podLabels := dep.Spec.Template.Labels
	for k, v := range selectorLabels {
		if podLabels[k] != v {
			t.Errorf("selector label %q=%q not found in pod template labels", k, v)
		}
	}

	// Verify container image.
	containers := dep.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	if containers[0].Image != "gsoci.azurecr.io/giantswarm/klaus:v1.0.0" {
		t.Errorf("Image = %q, want %q", containers[0].Image, "gsoci.azurecr.io/giantswarm/klaus:v1.0.0")
	}

	// Verify configmap checksum annotation is set.
	if _, ok := dep.Spec.Template.Annotations["checksum/config"]; !ok {
		t.Error("expected checksum/config annotation on pod template")
	}

	// Verify security context.
	podSec := dep.Spec.Template.Spec.SecurityContext
	if podSec == nil {
		t.Fatal("expected pod security context")
	}
	if *podSec.RunAsUser != 1000 {
		t.Errorf("RunAsUser = %d, want 1000", *podSec.RunAsUser)
	}

	// Verify probes.
	if containers[0].LivenessProbe == nil {
		t.Error("expected liveness probe")
	}
	if containers[0].ReadinessProbe == nil {
		t.Error("expected readiness probe")
	}
}

func TestBuildDeployment_WithPlugins(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Plugins: []klausv1alpha1.PluginReference{
				{Repository: "registry.io/plugins/gs-base", Tag: "v1.0.0"},
			},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", nil)

	// Verify plugin volume exists.
	foundVolume := false
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "plugin-gs-base" {
			foundVolume = true
			if v.Image == nil {
				t.Error("expected image volume source for plugin")
			}
		}
	}
	if !foundVolume {
		t.Error("expected plugin-gs-base volume")
	}

	// Verify plugin volume mount exists.
	foundMount := false
	for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == "plugin-gs-base" {
			foundMount = true
		}
	}
	if !foundMount {
		t.Error("expected plugin-gs-base volume mount")
	}
}

func TestBuildDeployment_WithImagePullSecrets(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:            "user@example.com",
			ImagePullSecrets: []string{"my-registry-creds"},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", nil)

	pullSecrets := dep.Spec.Template.Spec.ImagePullSecrets
	if len(pullSecrets) != 1 {
		t.Fatalf("expected 1 pull secret, got %d", len(pullSecrets))
	}
	if pullSecrets[0].Name != "my-registry-creds" {
		t.Errorf("pull secret name = %q, want %q", pullSecrets[0].Name, "my-registry-creds")
	}
}

func TestBuildDeployment_WithWorkspace(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:     "user@example.com",
			Workspace: &klausv1alpha1.WorkspaceConfig{},
		},
	}

	dep := BuildDeployment(instance, "klaus-user-test", "klaus:latest", nil)

	// Verify workspace volume.
	foundVolume := false
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == WorkspaceVolumeName {
			foundVolume = true
			if v.PersistentVolumeClaim == nil {
				t.Error("expected PVC volume source for workspace")
			}
		}
	}
	if !foundVolume {
		t.Error("expected workspace volume")
	}

	// Verify workspace mount.
	foundMount := false
	for _, m := range dep.Spec.Template.Spec.Containers[0].VolumeMounts {
		if m.Name == WorkspaceVolumeName && m.MountPath == WorkspaceMountPath {
			foundMount = true
		}
	}
	if !foundMount {
		t.Error("expected workspace volume mount at " + WorkspaceMountPath)
	}
}

func TestBuildDeployment_WithCustomImage(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Image: "gsoci.azurecr.io/giantswarm/klaus-go:1.25",
		},
	}

	// The reconciler passes the resolved image to BuildDeployment.
	resolvedImage := instance.Spec.Image
	dep := BuildDeployment(instance, "klaus-user-test", resolvedImage, nil)

	containers := dep.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("expected 1 container, got %d", len(containers))
	}
	if containers[0].Image != "gsoci.azurecr.io/giantswarm/klaus-go:1.25" {
		t.Errorf("Image = %q, want %q", containers[0].Image, "gsoci.azurecr.io/giantswarm/klaus-go:1.25")
	}
}

func TestBuildDeployment_SelectorLabelsMatchPodLabels(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "my-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "owner@example.com",
			Claude: klausv1alpha1.ClaudeConfig{
				PersistentMode: ptr.To(true),
			},
		},
	}

	dep := BuildDeployment(instance, "ns", "img:latest", nil)

	selectorLabels := SelectorLabels(instance)
	for k, v := range dep.Spec.Selector.MatchLabels {
		if selectorLabels[k] != v {
			t.Errorf("Deployment selector label %q=%q doesn't match SelectorLabels()", k, v)
		}
	}
	for k, v := range dep.Spec.Template.Labels {
		// Pod labels are a superset of selector labels.
		if sv, ok := selectorLabels[k]; ok && sv != v {
			t.Errorf("Pod template label %q=%q doesn't match SelectorLabels() value %q", k, v, sv)
		}
	}
}
