package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestPopulateCommonStatus_Toolchain(t *testing.T) {
	const defaultImage = "gsoci.azurecr.io/giantswarm/klaus:latest"

	tests := []struct {
		name          string
		resolvedImage string
		wantToolchain string
	}{
		{
			name:          "default image clears toolchain",
			resolvedImage: defaultImage,
			wantToolchain: "",
		},
		{
			name:          "custom image sets toolchain",
			resolvedImage: "gsoci.azurecr.io/giantswarm/klaus-go:1.25",
			wantToolchain: "gsoci.azurecr.io/giantswarm/klaus-go:1.25",
		},
		{
			name:          "empty resolved image sets toolchain",
			resolvedImage: "",
			wantToolchain: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &KlausInstanceReconciler{KlausImage: defaultImage}
			instance := &klausv1alpha1.KlausInstance{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec: klausv1alpha1.KlausInstanceSpec{
					Owner: "user@example.com",
				},
			}

			r.populateCommonStatus(instance, "klaus-user-test", tt.resolvedImage)

			if instance.Status.Toolchain != tt.wantToolchain {
				t.Errorf("Toolchain = %q, want %q", instance.Status.Toolchain, tt.wantToolchain)
			}
		})
	}
}

func TestPopulateCommonStatus_ToolchainClearedOnRevert(t *testing.T) {
	const defaultImage = "gsoci.azurecr.io/giantswarm/klaus:latest"

	r := &KlausInstanceReconciler{KlausImage: defaultImage}
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
	}

	// First call: custom image sets toolchain.
	r.populateCommonStatus(instance, "klaus-user-test", "gsoci.azurecr.io/giantswarm/klaus-go:1.25")
	if instance.Status.Toolchain != "gsoci.azurecr.io/giantswarm/klaus-go:1.25" {
		t.Fatalf("expected toolchain to be set, got %q", instance.Status.Toolchain)
	}

	// Second call: reverting to default clears toolchain.
	r.populateCommonStatus(instance, "klaus-user-test", defaultImage)
	if instance.Status.Toolchain != "" {
		t.Errorf("expected toolchain to be cleared after reverting to default, got %q", instance.Status.Toolchain)
	}
}

func TestPopulateCommonStatus_BasicFields(t *testing.T) {
	r := &KlausInstanceReconciler{KlausImage: "default:latest"}
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "my-instance",
			Generation: 3,
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Plugins: []klausv1alpha1.PluginReference{
				{Repository: "registry.io/plugin-a", Tag: "v1"},
				{Repository: "registry.io/plugin-b", Tag: "v2"},
			},
			MCPServers: []klausv1alpha1.MCPServerReference{
				{Name: "server-a"},
			},
			Claude: klausv1alpha1.ClaudeConfig{
				PersistentMode: ptr.To(true),
				MCPServers: map[string]runtime.RawExtension{
					"inline-server": {},
				},
			},
			PersonalityRef: &klausv1alpha1.PersonalityReference{Name: "my-personality"},
		},
	}

	r.populateCommonStatus(instance, "klaus-user-test", "default:latest")

	if instance.Status.PluginCount != 2 {
		t.Errorf("PluginCount = %d, want 2", instance.Status.PluginCount)
	}
	if instance.Status.MCPServerCount != 2 {
		t.Errorf("MCPServerCount = %d, want 2 (1 ref + 1 inline)", instance.Status.MCPServerCount)
	}
	if instance.Status.ObservedGeneration != 3 {
		t.Errorf("ObservedGeneration = %d, want 3", instance.Status.ObservedGeneration)
	}
	if instance.Status.Mode != klausv1alpha1.InstanceModePersistent {
		t.Errorf("Mode = %q, want %q", instance.Status.Mode, klausv1alpha1.InstanceModePersistent)
	}
	if instance.Status.Personality != "my-personality" {
		t.Errorf("Personality = %q, want %q", instance.Status.Personality, "my-personality")
	}
	if instance.Status.Toolchain != "" {
		t.Errorf("Toolchain = %q, want empty (default image)", instance.Status.Toolchain)
	}
}
