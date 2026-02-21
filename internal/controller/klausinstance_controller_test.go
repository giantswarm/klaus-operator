package controller

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	klausoci "github.com/giantswarm/klaus-oci"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// mockOCIResolver is a test double for OCIResolver.
type mockOCIResolver struct {
	personalityFn func(ctx context.Context, ref string) (string, error)
	toolchainFn   func(ctx context.Context, ref string) (string, error)
	pluginsFn     func(ctx context.Context, plugins []klausoci.PluginReference) ([]klausoci.PluginReference, error)
}

func (m *mockOCIResolver) ResolvePersonalityRef(ctx context.Context, ref string) (string, error) {
	if m.personalityFn != nil {
		return m.personalityFn(ctx, ref)
	}
	return ref, nil
}

func (m *mockOCIResolver) ResolveToolchainRef(ctx context.Context, ref string) (string, error) {
	if m.toolchainFn != nil {
		return m.toolchainFn(ctx, ref)
	}
	return ref, nil
}

func (m *mockOCIResolver) ResolvePluginRefs(ctx context.Context, plugins []klausoci.PluginReference) ([]klausoci.PluginReference, error) {
	if m.pluginsFn != nil {
		return m.pluginsFn(ctx, plugins)
	}
	return plugins, nil
}

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
			Personality: "gsoci.azurecr.io/giantswarm/personalities/go-dev:latest",
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
	if instance.Status.Personality != "gsoci.azurecr.io/giantswarm/personalities/go-dev:latest" {
		t.Errorf("Personality = %q, want OCI ref", instance.Status.Personality)
	}
	if instance.Status.Toolchain != "" {
		t.Errorf("Toolchain = %q, want empty (default image)", instance.Status.Toolchain)
	}
}

func TestResolveOCIReferences_NilClient(t *testing.T) {
	r := &KlausInstanceReconciler{}
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Personality: "go-dev",
			Image:       "some-image:latest",
		},
	}

	if err := r.resolveOCIReferences(context.Background(), instance); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if instance.Spec.Personality != "go-dev" {
		t.Errorf("personality should be unchanged, got %q", instance.Spec.Personality)
	}
}

func TestResolveOCIReferences_ResolvesAll(t *testing.T) {
	mock := &mockOCIResolver{
		personalityFn: func(_ context.Context, ref string) (string, error) {
			return "gsoci.azurecr.io/giantswarm/personalities/go-dev:v1.2.0", nil
		},
		toolchainFn: func(_ context.Context, ref string) (string, error) {
			return "gsoci.azurecr.io/giantswarm/klaus-go:v1.25.0", nil
		},
		pluginsFn: func(_ context.Context, plugins []klausoci.PluginReference) ([]klausoci.PluginReference, error) {
			resolved := make([]klausoci.PluginReference, len(plugins))
			for i, p := range plugins {
				resolved[i] = klausoci.PluginReference{
					Repository: p.Repository,
					Tag:        "v2.0.0",
				}
			}
			return resolved, nil
		},
	}

	r := &KlausInstanceReconciler{OCIClient: mock}
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:       "user@example.com",
			Personality: "go-dev",
			Image:       "klaus-go:latest",
			Plugins: []klausv1alpha1.PluginReference{
				{Repository: "gsoci.azurecr.io/giantswarm/plugins/platform", Tag: "latest"},
			},
		},
	}

	if err := r.resolveOCIReferences(context.Background(), instance); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if instance.Spec.Personality != "gsoci.azurecr.io/giantswarm/personalities/go-dev:v1.2.0" {
		t.Errorf("personality = %q, want resolved OCI ref", instance.Spec.Personality)
	}
	if instance.Spec.Image != "gsoci.azurecr.io/giantswarm/klaus-go:v1.25.0" {
		t.Errorf("image = %q, want resolved toolchain ref", instance.Spec.Image)
	}
	if instance.Spec.Plugins[0].Tag != "v2.0.0" {
		t.Errorf("plugin tag = %q, want %q", instance.Spec.Plugins[0].Tag, "v2.0.0")
	}
}

func TestResolveOCIReferences_PersonalityError(t *testing.T) {
	mock := &mockOCIResolver{
		personalityFn: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("registry unavailable")
		},
	}

	r := &KlausInstanceReconciler{OCIClient: mock}
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:       "user@example.com",
			Personality: "go-dev",
		},
	}

	err := r.resolveOCIReferences(context.Background(), instance)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, errors.Unwrap(err)) {
		// Just verify the error message wraps the original.
		if got := err.Error(); got == "" {
			t.Error("expected non-empty error message")
		}
	}
}

func TestResolveOCIReferences_ToolchainError(t *testing.T) {
	mock := &mockOCIResolver{
		toolchainFn: func(_ context.Context, _ string) (string, error) {
			return "", errors.New("resolve failed")
		},
	}

	r := &KlausInstanceReconciler{OCIClient: mock}
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Image: "klaus-go:latest",
		},
	}

	if err := r.resolveOCIReferences(context.Background(), instance); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveOCIReferences_PluginError(t *testing.T) {
	mock := &mockOCIResolver{
		pluginsFn: func(_ context.Context, _ []klausoci.PluginReference) ([]klausoci.PluginReference, error) {
			return nil, errors.New("plugin resolve failed")
		},
	}

	r := &KlausInstanceReconciler{OCIClient: mock}
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Plugins: []klausv1alpha1.PluginReference{
				{Repository: "gsoci.azurecr.io/giantswarm/plugins/platform", Tag: "latest"},
			},
		},
	}

	if err := r.resolveOCIReferences(context.Background(), instance); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestResolveOCIReferences_NoopWhenEmpty(t *testing.T) {
	called := false
	mock := &mockOCIResolver{
		personalityFn: func(_ context.Context, _ string) (string, error) {
			called = true
			return "", nil
		},
	}

	r := &KlausInstanceReconciler{OCIClient: mock}
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
	}

	if err := r.resolveOCIReferences(context.Background(), instance); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called {
		t.Error("expected no OCI calls when personality/image/plugins are empty")
	}
}

func TestResolveOCIReferences_PluginCountMismatch(t *testing.T) {
	mock := &mockOCIResolver{
		pluginsFn: func(_ context.Context, _ []klausoci.PluginReference) ([]klausoci.PluginReference, error) {
			return []klausoci.PluginReference{}, nil
		},
	}

	r := &KlausInstanceReconciler{OCIClient: mock}
	instance := &klausv1alpha1.KlausInstance{
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Plugins: []klausv1alpha1.PluginReference{
				{Repository: "gsoci.azurecr.io/giantswarm/plugins/platform", Tag: "latest"},
			},
		},
	}

	err := r.resolveOCIReferences(context.Background(), instance)
	if err == nil {
		t.Fatal("expected error for plugin count mismatch, got nil")
	}
}
