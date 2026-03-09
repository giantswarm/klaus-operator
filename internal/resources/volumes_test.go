package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func findMount(mounts []corev1.VolumeMount, mountPath string) *corev1.VolumeMount {
	for i := range mounts {
		if mounts[i].MountPath == mountPath {
			return &mounts[i]
		}
	}
	return nil
}

func TestBuildVolumeMounts_PersonalitySOUL(t *testing.T) {
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test-instance"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:       "user@example.com",
			Personality: "gsoci.azurecr.io/giantswarm/personality:v1",
			Claude: klausv1alpha1.ClaudeConfig{
				Model:          "claude-sonnet-4-20250514",
				PermissionMode: klausv1alpha1.PermissionModeBypass,
			},
		},
	}

	mounts := BuildVolumeMounts(instance)

	// Existing personality mount should still be present.
	pm := findMount(mounts, PersonalityMountPath)
	if pm == nil {
		t.Fatalf("expected personality mount at %s", PersonalityMountPath)
	}
	if pm.Name != PersonalityVolumeName {
		t.Errorf("personality mount Name = %q, want %q", pm.Name, PersonalityVolumeName)
	}
	if pm.SubPath != "" {
		t.Errorf("personality mount SubPath = %q, want empty", pm.SubPath)
	}

	// SOUL.md SubPath mount should be present.
	sm := findMount(mounts, SOULMountPath)
	if sm == nil {
		t.Fatalf("expected SOUL mount at %s", SOULMountPath)
	}
	if sm.Name != PersonalityVolumeName {
		t.Errorf("SOUL mount Name = %q, want %q", sm.Name, PersonalityVolumeName)
	}
	if sm.SubPath != "SOUL.md" {
		t.Errorf("SOUL mount SubPath = %q, want %q", sm.SubPath, "SOUL.md")
	}
	if !sm.ReadOnly {
		t.Error("SOUL mount should be read-only")
	}
}

func TestBuildVolumeMounts_NoPersonality(t *testing.T) {
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

	mounts := BuildVolumeMounts(instance)

	if pm := findMount(mounts, PersonalityMountPath); pm != nil {
		t.Errorf("unexpected personality mount at %s when personality is empty", PersonalityMountPath)
	}
	if sm := findMount(mounts, SOULMountPath); sm != nil {
		t.Errorf("unexpected SOUL mount at %s when personality is empty", SOULMountPath)
	}
}
