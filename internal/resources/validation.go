package resources

import (
	"fmt"
	"strings"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// ValidateSpec performs validation checks on the KlausInstance spec,
// enforcing mutual-exclusivity rules and constraint checks that the
// Helm chart enforces via fail.
func ValidateSpec(instance *klausv1alpha1.KlausInstance) error {
	if err := validateHooksExclusivity(instance); err != nil {
		return err
	}
	if err := validatePlugins(instance); err != nil {
		return err
	}
	return nil
}

// validateHooksExclusivity ensures that inline hooks and settingsFile are
// mutually exclusive -- you cannot specify both because they both control
// settings.json.
func validateHooksExclusivity(instance *klausv1alpha1.KlausInstance) error {
	if len(instance.Spec.Hooks) > 0 && instance.Spec.Claude.SettingsFile != "" {
		return fmt.Errorf("spec.hooks and spec.claude.settingsFile are mutually exclusive: " +
			"hooks are rendered to settings.json, but settingsFile points to a custom path")
	}
	return nil
}

// validatePlugins validates plugin references on a KlausInstance.
func validatePlugins(instance *klausv1alpha1.KlausInstance) error {
	return ValidatePluginRefs(instance.Spec.Plugins)
}

// ValidatePluginRefs validates a slice of plugin references: each plugin must
// have exactly one of tag or digest (not both, not neither), digests must use
// the sha256: prefix, and plugin short names must be unique.
func ValidatePluginRefs(plugins []klausv1alpha1.PluginReference) error {
	seen := make(map[string]string) // short name -> repository

	for i, plugin := range plugins {
		// Tag XOR digest.
		hasTag := plugin.Tag != ""
		hasDigest := plugin.Digest != ""
		if !hasTag && !hasDigest {
			return fmt.Errorf("spec.plugins[%d] (%s): must specify either tag or digest",
				i, plugin.Repository)
		}
		if hasTag && hasDigest {
			return fmt.Errorf("spec.plugins[%d] (%s): tag and digest are mutually exclusive",
				i, plugin.Repository)
		}

		// Digest format validation.
		if hasDigest && !strings.HasPrefix(plugin.Digest, "sha256:") {
			return fmt.Errorf("spec.plugins[%d] (%s): digest must start with 'sha256:'",
				i, plugin.Repository)
		}

		// Short name uniqueness.
		shortName := ShortPluginName(plugin.Repository)
		if existing, ok := seen[shortName]; ok {
			return fmt.Errorf("spec.plugins[%d] (%s): short name %q conflicts with %s "+
				"(plugin short names must be unique as they determine volume names and mount paths)",
				i, plugin.Repository, shortName, existing)
		}
		seen[shortName] = plugin.Repository
	}

	return nil
}
