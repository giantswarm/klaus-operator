package resources

import (
	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// MergePersonalityIntoInstance applies the personality defaults to the instance
// spec. The instance spec is modified in place. The merge follows these rules:
//
//   - Scalar fields: instance overrides personality (non-zero instance value wins)
//   - List fields: instance values are appended to personality values
//     (plugins deduplicated by repository; MCP refs/secrets deduplicated by
//     name with instance overriding personality on conflict)
//   - Map fields: personality entries are used as base; instance entries win on
//     key conflict
//   - Pointer/boolean fields: instance overrides personality when explicitly set
//     (non-nil)
//   - Empty/zero values in instance spec do not override personality defaults
//
// Callers must pass a deep-copied instance to avoid mutating the informer cache.
func MergePersonalityIntoInstance(personality *klausv1alpha1.KlausPersonalitySpec, instance *klausv1alpha1.KlausInstanceSpec) {
	// Scalar field: instance overrides personality when non-empty.
	if instance.Image == "" {
		instance.Image = personality.Image
	}

	mergeClaudeConfig(&personality.Claude, &instance.Claude)

	// List fields: personality first, then instance appended.
	instance.Plugins = mergePlugins(personality.Plugins, instance.Plugins)
	instance.PluginDirs = mergeStringSlices(personality.PluginDirs, instance.PluginDirs)
	instance.MCPServers = mergeMCPServerRefs(personality.MCPServers, instance.MCPServers)
	instance.AddDirs = mergeStringSlices(personality.AddDirs, instance.AddDirs)

	// Map fields: personality as base, instance wins on key conflict.
	instance.Skills = mergeMaps(personality.Skills, instance.Skills)
	instance.AgentFiles = mergeMaps(personality.AgentFiles, instance.AgentFiles)
	instance.Hooks = mergeMaps(personality.Hooks, instance.Hooks)
	instance.HookScripts = mergeMaps(personality.HookScripts, instance.HookScripts)

	// Pointer fields: instance overrides personality when explicitly set.
	if instance.LoadAdditionalDirsMemory == nil {
		instance.LoadAdditionalDirsMemory = personality.LoadAdditionalDirsMemory
	}
	if instance.Resources == nil {
		instance.Resources = personality.Resources
	}
	if instance.Telemetry == nil {
		instance.Telemetry = personality.Telemetry
	}
}

// mergeClaudeConfig merges personality Claude config into instance Claude config.
func mergeClaudeConfig(personality, instance *klausv1alpha1.ClaudeConfig) {
	// Scalar fields: instance overrides when non-zero.
	if instance.Model == "" {
		instance.Model = personality.Model
	}
	if instance.MaxTurns == nil {
		instance.MaxTurns = personality.MaxTurns
	}
	if instance.PermissionMode == "" {
		instance.PermissionMode = personality.PermissionMode
	}
	if instance.SystemPrompt == "" {
		instance.SystemPrompt = personality.SystemPrompt
	}
	if instance.AppendSystemPrompt == "" {
		instance.AppendSystemPrompt = personality.AppendSystemPrompt
	}
	if instance.MCPTimeout == nil {
		instance.MCPTimeout = personality.MCPTimeout
	}
	if instance.MaxMCPOutputTokens == nil {
		instance.MaxMCPOutputTokens = personality.MaxMCPOutputTokens
	}
	if instance.StrictMCPConfig == nil {
		instance.StrictMCPConfig = personality.StrictMCPConfig
	}
	if instance.MaxBudgetUSD == nil {
		instance.MaxBudgetUSD = personality.MaxBudgetUSD
	}
	if instance.Effort == "" {
		instance.Effort = personality.Effort
	}
	if instance.FallbackModel == "" {
		instance.FallbackModel = personality.FallbackModel
	}
	if instance.JSONSchema == "" {
		instance.JSONSchema = personality.JSONSchema
	}
	if instance.SettingsFile == "" {
		instance.SettingsFile = personality.SettingsFile
	}
	if instance.SettingSources == "" {
		instance.SettingSources = personality.SettingSources
	}
	if instance.ActiveAgent == "" {
		instance.ActiveAgent = personality.ActiveAgent
	}

	// Pointer booleans: instance overrides when explicitly set (non-nil).
	if instance.PersistentMode == nil {
		instance.PersistentMode = personality.PersistentMode
	}
	if instance.IncludePartialMessages == nil {
		instance.IncludePartialMessages = personality.IncludePartialMessages
	}
	if instance.NoSessionPersistence == nil {
		instance.NoSessionPersistence = personality.NoSessionPersistence
	}

	// List fields: append instance to personality.
	instance.MCPServerSecrets = mergeMCPServerSecrets(personality.MCPServerSecrets, instance.MCPServerSecrets)
	instance.Tools = mergeStringSlices(personality.Tools, instance.Tools)
	instance.AllowedTools = mergeStringSlices(personality.AllowedTools, instance.AllowedTools)
	instance.DisallowedTools = mergeStringSlices(personality.DisallowedTools, instance.DisallowedTools)

	// Map fields: personality as base, instance wins on key conflict.
	instance.MCPServers = mergeMaps(personality.MCPServers, instance.MCPServers)
	instance.Agents = mergeMaps(personality.Agents, instance.Agents)
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

// mergeStringSlices appends instance values to personality values, preserving
// order. Duplicates are intentionally kept -- downstream consumers (e.g.
// Claude CLI flags) are tolerant of repeated entries, and deduplication here
// would add complexity without clear benefit.
func mergeStringSlices(personality, instance []string) []string {
	if len(personality) == 0 {
		return instance
	}
	if len(instance) == 0 {
		return personality
	}
	merged := make([]string, 0, len(personality)+len(instance))
	merged = append(merged, personality...)
	merged = append(merged, instance...)
	return merged
}

// mergeMCPServerRefs merges personality MCP server references with instance
// references, deduplicating by name. On duplicate name, the instance version
// overrides the personality's.
func mergeMCPServerRefs(personality, instance []klausv1alpha1.MCPServerReference) []klausv1alpha1.MCPServerReference {
	if len(personality) == 0 {
		return instance
	}
	if len(instance) == 0 {
		return personality
	}

	seen := make(map[string]bool, len(personality)+len(instance))
	var merged []klausv1alpha1.MCPServerReference

	for _, ref := range personality {
		if !seen[ref.Name] {
			seen[ref.Name] = true
			merged = append(merged, ref)
		}
	}
	for _, ref := range instance {
		if seen[ref.Name] {
			// Instance overrides personality on duplicate name.
			for i := range merged {
				if merged[i].Name == ref.Name {
					merged[i] = ref
					break
				}
			}
		} else {
			seen[ref.Name] = true
			merged = append(merged, ref)
		}
	}

	return merged
}

// mergeMCPServerSecrets merges personality secrets with instance secrets,
// deduplicating by secret name. On duplicate secret name, the instance version
// overrides the personality's.
func mergeMCPServerSecrets(personality, instance []klausv1alpha1.MCPServerSecret) []klausv1alpha1.MCPServerSecret {
	if len(personality) == 0 {
		return instance
	}
	if len(instance) == 0 {
		return personality
	}

	seen := make(map[string]bool, len(personality)+len(instance))
	var merged []klausv1alpha1.MCPServerSecret

	for _, s := range personality {
		if !seen[s.SecretName] {
			seen[s.SecretName] = true
			merged = append(merged, s)
		}
	}
	for _, s := range instance {
		if seen[s.SecretName] {
			// Instance overrides personality on duplicate secret name.
			for i := range merged {
				if merged[i].SecretName == s.SecretName {
					merged[i] = s
					break
				}
			}
		} else {
			seen[s.SecretName] = true
			merged = append(merged, s)
		}
	}

	return merged
}

// mergeMaps merges two string-keyed maps. Personality entries form the base;
// instance entries win on key conflict. Returns nil only when both inputs are
// nil/empty.
//
// When one side is empty, the other side's map is returned directly. This is
// safe because callers operate on a deep-copied instance spec.
func mergeMaps[V any](personality, instance map[string]V) map[string]V {
	if len(personality) == 0 {
		return instance
	}
	if len(instance) == 0 {
		return personality
	}

	merged := make(map[string]V, len(personality)+len(instance))
	for k, v := range personality {
		merged[k] = v
	}
	for k, v := range instance {
		merged[k] = v // instance wins
	}
	return merged
}
