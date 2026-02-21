package resources

import (
	"testing"

	klausoci "github.com/giantswarm/klaus-oci"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestUserNamespace(t *testing.T) {
	tests := []struct {
		name     string
		owner    string
		expected string
	}{
		{
			name:     "simple email",
			owner:    "user@example.com",
			expected: "klaus-user-user-example-com",
		},
		{
			name:     "complex email",
			owner:    "first.last@team.company.io",
			expected: "klaus-user-first-last-team-company-io",
		},
		{
			name:     "uppercase email",
			owner:    "Admin@Example.COM",
			expected: "klaus-user-admin-example-com",
		},
		{
			name:     "special characters",
			owner:    "user+tag@example.com",
			expected: "klaus-user-user-tag-example-com",
		},
		{
			name:     "long email truncation no trailing hyphen",
			owner:    "aaaa@@@@@bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbcccc",
			expected: "klaus-user-aaaa-----bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		{
			name:     "trailing hyphen after truncation is trimmed",
			owner:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa@b",
			expected: "klaus-user-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := UserNamespace(tt.owner)
			if result != tt.expected {
				t.Errorf("UserNamespace(%q) = %q, want %q", tt.owner, result, tt.expected)
			}
		})
	}
}

func TestShortName(t *testing.T) {
	tests := []struct {
		name       string
		repository string
		expected   string
	}{
		{
			name:       "long path",
			repository: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-platform",
			expected:   "gs-platform",
		},
		{
			name:       "short path",
			repository: "myregistry/myplugin",
			expected:   "myplugin",
		},
		{
			name:       "single segment",
			repository: "myplugin",
			expected:   "myplugin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := klausoci.ShortName(tt.repository)
			if result != tt.expected {
				t.Errorf("ShortName(%q) = %q, want %q", tt.repository, result, tt.expected)
			}
		})
	}
}

func TestPluginImageReference(t *testing.T) {
	tests := []struct {
		name     string
		plugin   klausv1alpha1.PluginReference
		expected string
	}{
		{
			name: "with tag",
			plugin: klausv1alpha1.PluginReference{
				Repository: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base",
				Tag:        "v1.0.0",
			},
			expected: "gsoci.azurecr.io/giantswarm/klaus-plugins/gs-base:v1.0.0",
		},
		{
			name: "with digest",
			plugin: klausv1alpha1.PluginReference{
				Repository: "gsoci.azurecr.io/giantswarm/klaus-plugins/security",
				Digest:     "sha256:abc123",
			},
			expected: "gsoci.azurecr.io/giantswarm/klaus-plugins/security@sha256:abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PluginImageReference(tt.plugin)
			if result != tt.expected {
				t.Errorf("PluginImageReference() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestConfigMapChecksum(t *testing.T) {
	data1 := map[string]string{"a": "1", "b": "2"}
	data2 := map[string]string{"b": "2", "a": "1"}
	data3 := map[string]string{"a": "1", "b": "3"}

	checksum1 := ConfigMapChecksum(data1)
	checksum2 := ConfigMapChecksum(data2)
	checksum3 := ConfigMapChecksum(data3)

	// Same data in different order should produce the same checksum.
	if checksum1 != checksum2 {
		t.Errorf("checksums should be equal for same data: %q != %q", checksum1, checksum2)
	}

	// Different data should produce different checksums.
	if checksum1 == checksum3 {
		t.Errorf("checksums should differ for different data: %q == %q", checksum1, checksum3)
	}
}

func TestHasInlineExtensions(t *testing.T) {
	tests := []struct {
		name     string
		instance *klausv1alpha1.KlausInstance
		expected bool
	}{
		{
			name: "no extensions",
			instance: &klausv1alpha1.KlausInstance{
				Spec: klausv1alpha1.KlausInstanceSpec{},
			},
			expected: false,
		},
		{
			name: "with skills",
			instance: &klausv1alpha1.KlausInstance{
				Spec: klausv1alpha1.KlausInstanceSpec{
					Skills: map[string]klausv1alpha1.SkillConfig{
						"test": {Content: "test content"},
					},
				},
			},
			expected: true,
		},
		{
			name: "with agent files",
			instance: &klausv1alpha1.KlausInstance{
				Spec: klausv1alpha1.KlausInstanceSpec{
					AgentFiles: map[string]klausv1alpha1.AgentFileConfig{
						"test": {Content: "test content"},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasInlineExtensions(tt.instance)
			if result != tt.expected {
				t.Errorf("HasInlineExtensions() = %v, want %v", result, tt.expected)
			}
		})
	}
}
