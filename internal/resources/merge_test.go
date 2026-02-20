package resources

import (
	"testing"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
	"github.com/giantswarm/klaus-operator/internal/oci"
)

func TestMergeOCIPersonalityIntoInstance_NilPersonality(t *testing.T) {
	instance := &klausv1alpha1.KlausInstanceSpec{
		Claude: klausv1alpha1.ClaudeConfig{
			Model: "instance-model",
		},
	}
	MergeOCIPersonalityIntoInstance(nil, instance)
	if instance.Claude.Model != "instance-model" {
		t.Errorf("nil personality should not change instance spec, got model %q", instance.Claude.Model)
	}
}

func TestMergeOCIPersonalityIntoInstance_ImageInherited(t *testing.T) {
	t.Run("empty instance inherits personality image", func(t *testing.T) {
		personality := &oci.PersonalitySpec{
			Image: "gsoci.azurecr.io/giantswarm/klaus-go:1.25",
		}
		instance := &klausv1alpha1.KlausInstanceSpec{}

		MergeOCIPersonalityIntoInstance(personality, instance)

		if instance.Image != "gsoci.azurecr.io/giantswarm/klaus-go:1.25" {
			t.Errorf("expected image from personality, got %q", instance.Image)
		}
	})

	t.Run("instance image overrides personality image", func(t *testing.T) {
		personality := &oci.PersonalitySpec{
			Image: "gsoci.azurecr.io/giantswarm/klaus-go:1.25",
		}
		instance := &klausv1alpha1.KlausInstanceSpec{
			Image: "gsoci.azurecr.io/giantswarm/klaus-python:3.13",
		}

		MergeOCIPersonalityIntoInstance(personality, instance)

		if instance.Image != "gsoci.azurecr.io/giantswarm/klaus-python:3.13" {
			t.Errorf("expected instance image override, got %q", instance.Image)
		}
	})

	t.Run("empty personality does not clear instance image", func(t *testing.T) {
		personality := &oci.PersonalitySpec{}
		instance := &klausv1alpha1.KlausInstanceSpec{
			Image: "gsoci.azurecr.io/giantswarm/klaus-rust:1.85",
		}

		MergeOCIPersonalityIntoInstance(personality, instance)

		if instance.Image != "gsoci.azurecr.io/giantswarm/klaus-rust:1.85" {
			t.Errorf("expected instance image preserved, got %q", instance.Image)
		}
	})
}

func TestMergeOCIPersonalityIntoInstance_SystemPromptInherited(t *testing.T) {
	t.Run("instance inherits system prompt when empty", func(t *testing.T) {
		personality := &oci.PersonalitySpec{
			SystemPrompt:       "You are a Go expert.",
			AppendSystemPrompt: "Always follow conventions.",
		}
		instance := &klausv1alpha1.KlausInstanceSpec{}

		MergeOCIPersonalityIntoInstance(personality, instance)

		if instance.Claude.SystemPrompt != "You are a Go expert." {
			t.Errorf("expected system prompt from personality, got %q", instance.Claude.SystemPrompt)
		}
		if instance.Claude.AppendSystemPrompt != "Always follow conventions." {
			t.Errorf("expected append system prompt from personality, got %q", instance.Claude.AppendSystemPrompt)
		}
	})

	t.Run("instance system prompt overrides personality", func(t *testing.T) {
		personality := &oci.PersonalitySpec{
			SystemPrompt: "Personality prompt",
		}
		instance := &klausv1alpha1.KlausInstanceSpec{
			Claude: klausv1alpha1.ClaudeConfig{
				SystemPrompt: "Instance prompt",
			},
		}

		MergeOCIPersonalityIntoInstance(personality, instance)

		if instance.Claude.SystemPrompt != "Instance prompt" {
			t.Errorf("expected instance system prompt override, got %q", instance.Claude.SystemPrompt)
		}
	})
}

func TestMergeOCIPersonalityIntoInstance_PluginsMergedAndDeduplicated(t *testing.T) {
	personality := &oci.PersonalitySpec{
		Plugins: []oci.PersonalityPlugin{
			{Repository: "gsoci.azurecr.io/giantswarm/gs-platform", Tag: "v1.0.0"},
			{Repository: "gsoci.azurecr.io/giantswarm/security", Tag: "v0.3.0"},
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{
		Plugins: []klausv1alpha1.PluginReference{
			// Override the security plugin version.
			{Repository: "gsoci.azurecr.io/giantswarm/security", Tag: "v0.4.0"},
			// Add a new plugin.
			{Repository: "gsoci.azurecr.io/giantswarm/team-tools", Tag: "v1.0.0"},
		},
	}

	MergeOCIPersonalityIntoInstance(personality, instance)

	if len(instance.Plugins) != 3 {
		t.Fatalf("expected 3 plugins after merge, got %d", len(instance.Plugins))
	}

	// First: gs-platform from personality.
	if instance.Plugins[0].Repository != "gsoci.azurecr.io/giantswarm/gs-platform" || instance.Plugins[0].Tag != "v1.0.0" {
		t.Errorf("plugin 0 should be gs-platform:v1.0.0, got %s:%s", instance.Plugins[0].Repository, instance.Plugins[0].Tag)
	}
	// Second: security overridden by instance.
	if instance.Plugins[1].Repository != "gsoci.azurecr.io/giantswarm/security" || instance.Plugins[1].Tag != "v0.4.0" {
		t.Errorf("plugin 1 should be security:v0.4.0 (instance override), got %s:%s", instance.Plugins[1].Repository, instance.Plugins[1].Tag)
	}
	// Third: team-tools from instance.
	if instance.Plugins[2].Repository != "gsoci.azurecr.io/giantswarm/team-tools" || instance.Plugins[2].Tag != "v1.0.0" {
		t.Errorf("plugin 2 should be team-tools:v1.0.0, got %s:%s", instance.Plugins[2].Repository, instance.Plugins[2].Tag)
	}
}

func TestMergeOCIPersonalityIntoInstance_EmptyPersonality(t *testing.T) {
	personality := &oci.PersonalitySpec{}

	instance := &klausv1alpha1.KlausInstanceSpec{
		Claude: klausv1alpha1.ClaudeConfig{
			Model: "instance-model",
		},
		Plugins: []klausv1alpha1.PluginReference{
			{Repository: "repo/plugin", Tag: "v1"},
		},
	}

	MergeOCIPersonalityIntoInstance(personality, instance)

	if instance.Claude.Model != "instance-model" {
		t.Error("empty personality should not change instance model")
	}
	if len(instance.Plugins) != 1 {
		t.Error("empty personality should not change instance plugins")
	}
}

func TestMergeOCIPersonalityIntoInstance_EmptyInstance(t *testing.T) {
	personality := &oci.PersonalitySpec{
		Image:        "gsoci.azurecr.io/giantswarm/klaus-go:latest",
		SystemPrompt: "You are an expert.",
		Plugins: []oci.PersonalityPlugin{
			{Repository: "repo/plugin", Tag: "v1"},
		},
	}

	instance := &klausv1alpha1.KlausInstanceSpec{}

	MergeOCIPersonalityIntoInstance(personality, instance)

	if instance.Image != "gsoci.azurecr.io/giantswarm/klaus-go:latest" {
		t.Errorf("expected image from personality, got %q", instance.Image)
	}
	if instance.Claude.SystemPrompt != "You are an expert." {
		t.Errorf("expected system prompt from personality, got %q", instance.Claude.SystemPrompt)
	}
	if len(instance.Plugins) != 1 {
		t.Errorf("expected 1 plugin from personality, got %d", len(instance.Plugins))
	}
}

func TestMergePlugins_EmptyInputs(t *testing.T) {
	t.Run("both empty", func(t *testing.T) {
		result := mergePlugins(nil, nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("personality only", func(t *testing.T) {
		plugins := []klausv1alpha1.PluginReference{{Repository: "a", Tag: "v1"}}
		result := mergePlugins(plugins, nil)
		if len(result) != 1 || result[0].Repository != "a" {
			t.Errorf("expected personality plugins, got %v", result)
		}
	})

	t.Run("instance only", func(t *testing.T) {
		plugins := []klausv1alpha1.PluginReference{{Repository: "b", Tag: "v2"}}
		result := mergePlugins(nil, plugins)
		if len(result) != 1 || result[0].Repository != "b" {
			t.Errorf("expected instance plugins, got %v", result)
		}
	})
}
