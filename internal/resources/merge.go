package resources

import (
	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
	"github.com/giantswarm/klaus-operator/internal/oci"
)

// MergeOCIPersonalityIntoInstance applies the OCI personality defaults to the
// instance spec. The instance spec is modified in place. The merge follows
// these rules:
//
//   - Scalar fields: instance overrides personality (non-zero instance value wins)
//   - List fields: personality plugins are prepended to instance plugins
//     (deduplicated by repository; instance version wins on conflict)
//   - Pointer/boolean fields: instance overrides personality when explicitly set (non-nil)
//   - Empty/zero values in instance spec do not override personality defaults
//
// Callers must pass a deep-copied instance to avoid mutating the informer cache.
func MergeOCIPersonalityIntoInstance(personality *oci.PersonalitySpec, instance *klausv1alpha1.KlausInstanceSpec) {
	if personality == nil {
		return
	}

	// Image: instance overrides personality when non-empty.
	if instance.Image == "" {
		instance.Image = personality.Image
	}

	// System prompts: instance overrides personality when non-empty.
	if instance.Claude.SystemPrompt == "" {
		instance.Claude.SystemPrompt = personality.SystemPrompt
	}
	if instance.Claude.AppendSystemPrompt == "" {
		instance.Claude.AppendSystemPrompt = personality.AppendSystemPrompt
	}

	// Plugins: personality first, then instance appended (dedup by repository).
	personalityPlugins := make([]klausv1alpha1.PluginReference, 0, len(personality.Plugins))
	for _, p := range personality.Plugins {
		personalityPlugins = append(personalityPlugins, klausv1alpha1.PluginReference{
			Repository: p.Repository,
			Tag:        p.Tag,
			Digest:     p.Digest,
		})
	}
	instance.Plugins = mergePlugins(personalityPlugins, instance.Plugins)
}

// mergePlugins merges personality plugins with instance plugins. Instance
// plugins are appended to personality plugins, deduplicated by repository.
// On duplicate repository, the instance version overrides the personality's.
func mergePlugins(personality, instance []klausv1alpha1.PluginReference) []klausv1alpha1.PluginReference {
	if len(personality) == 0 {
		// Safe: caller operates on a deep copy, so returning the input
		// slice does not alias the informer cache.
		return instance
	}
	if len(instance) == 0 {
		return personality
	}

	seen := make(map[string]bool, len(personality)+len(instance))
	var merged []klausv1alpha1.PluginReference

	// Personality plugins first.
	for _, p := range personality {
		if !seen[p.Repository] {
			seen[p.Repository] = true
			merged = append(merged, p)
		}
	}

	// Instance plugins appended (override personality version on duplicate repo).
	for _, p := range instance {
		if seen[p.Repository] {
			// Replace the personality plugin with the instance override.
			for i := range merged {
				if merged[i].Repository == p.Repository {
					merged[i] = p
					break
				}
			}
		} else {
			seen[p.Repository] = true
			merged = append(merged, p)
		}
	}

	return merged
}
