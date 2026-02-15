package resources

import (
	"k8s.io/apimachinery/pkg/runtime"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// MergePersonalityIntoInstance applies the personality defaults to the instance
// spec. The instance spec is modified in place. The merge follows these rules:
//
//   - Scalar fields: instance overrides personality (non-zero instance value wins)
//   - List fields: instance values are appended to personality values
//     (plugins deduplicated by repository)
//   - Map fields: personality entries are used as base; instance entries win on
//     key conflict
//   - Pointer/boolean fields: instance overrides personality when explicitly set
//     (non-nil)
//   - Empty/zero values in instance spec do not override personality defaults
func MergePersonalityIntoInstance(personality *klausv1alpha1.KlausPersonalitySpec, instance *klausv1alpha1.KlausInstanceSpec) {
	mergeClaudeConfig(&personality.Claude, &instance.Claude)

	// List fields: personality first, then instance appended.
	instance.Plugins = mergePlugins(personality.Plugins, instance.Plugins)
	instance.PluginDirs = mergeStringSlices(personality.PluginDirs, instance.PluginDirs)
	instance.MCPServers = mergeMCPServerRefs(personality.MCPServers, instance.MCPServers)
	instance.AddDirs = mergeStringSlices(personality.AddDirs, instance.AddDirs)

	// Map fields: personality as base, instance wins on key conflict.
	instance.Skills = mergeSkillsMap(personality.Skills, instance.Skills)
	instance.AgentFiles = mergeAgentFilesMap(personality.AgentFiles, instance.AgentFiles)
	instance.Hooks = mergeRawExtensionMap(personality.Hooks, instance.Hooks)
	instance.HookScripts = mergeStringMap(personality.HookScripts, instance.HookScripts)

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
	instance.MCPServers = mergeRawExtensionMap(personality.MCPServers, instance.MCPServers)
	instance.Agents = mergeRawExtensionMap(personality.Agents, instance.Agents)
}

// mergePlugins merges personality plugins with instance plugins. Instance
// plugins are appended to personality plugins, deduplicated by repository.
func mergePlugins(personality, instance []klausv1alpha1.PluginReference) []klausv1alpha1.PluginReference {
	if len(personality) == 0 {
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

// mergeStringSlices appends instance values to personality values, preserving order.
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

// mergeMCPServerRefs appends instance MCP server references to personality
// references, deduplicating by name.
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
		if !seen[ref.Name] {
			seen[ref.Name] = true
			merged = append(merged, ref)
		}
	}

	return merged
}

// mergeMCPServerSecrets appends instance secrets to personality secrets,
// deduplicating by secret name.
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
		if !seen[s.SecretName] {
			seen[s.SecretName] = true
			merged = append(merged, s)
		}
	}

	return merged
}

// mergeSkillsMap merges personality skills with instance skills. Instance
// entries win on key conflict.
func mergeSkillsMap(personality, instance map[string]klausv1alpha1.SkillConfig) map[string]klausv1alpha1.SkillConfig {
	if len(personality) == 0 {
		return instance
	}
	if len(instance) == 0 {
		return personality
	}

	merged := make(map[string]klausv1alpha1.SkillConfig, len(personality)+len(instance))
	for k, v := range personality {
		merged[k] = v
	}
	for k, v := range instance {
		merged[k] = v // instance wins
	}
	return merged
}

// mergeAgentFilesMap merges personality agent files with instance agent files.
// Instance entries win on key conflict.
func mergeAgentFilesMap(personality, instance map[string]klausv1alpha1.AgentFileConfig) map[string]klausv1alpha1.AgentFileConfig {
	if len(personality) == 0 {
		return instance
	}
	if len(instance) == 0 {
		return personality
	}

	merged := make(map[string]klausv1alpha1.AgentFileConfig, len(personality)+len(instance))
	for k, v := range personality {
		merged[k] = v
	}
	for k, v := range instance {
		merged[k] = v // instance wins
	}
	return merged
}

// mergeRawExtensionMap merges personality entries with instance entries.
// Instance entries win on key conflict.
func mergeRawExtensionMap(personality, instance map[string]runtime.RawExtension) map[string]runtime.RawExtension {
	if len(personality) == 0 {
		return instance
	}
	if len(instance) == 0 {
		return personality
	}

	merged := make(map[string]runtime.RawExtension, len(personality)+len(instance))
	for k, v := range personality {
		merged[k] = v
	}
	for k, v := range instance {
		merged[k] = v // instance wins
	}
	return merged
}

// mergeStringMap merges personality entries with instance entries. Instance
// entries win on key conflict.
func mergeStringMap(personality, instance map[string]string) map[string]string {
	if len(personality) == 0 {
		return instance
	}
	if len(instance) == 0 {
		return personality
	}

	merged := make(map[string]string, len(personality)+len(instance))
	for k, v := range personality {
		merged[k] = v
	}
	for k, v := range instance {
		merged[k] = v // instance wins
	}
	return merged
}
