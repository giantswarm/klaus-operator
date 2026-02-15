package resources

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildEnvVars creates the full list of environment variables for a Klaus
// instance container, mirroring the Helm chart's deployment.yaml env rendering.
func BuildEnvVars(instance *klausv1alpha1.KlausInstance, configMapName, secretName string) []corev1.EnvVar {
	var envs []corev1.EnvVar

	// PORT is always set.
	envs = append(envs, corev1.EnvVar{
		Name:  "PORT",
		Value: strconv.Itoa(KlausPort),
	})

	// Anthropic API key from Secret.
	envs = append(envs, corev1.EnvVar{
		Name: "ANTHROPIC_API_KEY",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
				Key:                  "api-key",
			},
		},
	})

	// Claude model.
	if instance.Spec.Claude.Model != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_MODEL",
			Value: instance.Spec.Claude.Model,
		})
	}

	// Max turns.
	if instance.Spec.Claude.MaxTurns != nil && *instance.Spec.Claude.MaxTurns > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_MAX_TURNS",
			Value: strconv.Itoa(*instance.Spec.Claude.MaxTurns),
		})
	}

	// Permission mode.
	if instance.Spec.Claude.PermissionMode != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_PERMISSION_MODE",
			Value: instance.Spec.Claude.PermissionMode,
		})
	}

	// System prompt (from ConfigMap).
	if instance.Spec.Claude.SystemPrompt != "" {
		envs = append(envs, envFromConfigMap("CLAUDE_SYSTEM_PROMPT", configMapName, "system-prompt"))
	}

	// Append system prompt (from ConfigMap).
	if instance.Spec.Claude.AppendSystemPrompt != "" {
		envs = append(envs, envFromConfigMap("CLAUDE_APPEND_SYSTEM_PROMPT", configMapName, "append-system-prompt"))
	}

	// MCP config path.
	if HasMCPConfig(instance) {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_MCP_CONFIG",
			Value: MCPConfigPath,
		})
	}

	// Strict MCP config.
	if instance.Spec.Claude.StrictMCPConfig == nil || *instance.Spec.Claude.StrictMCPConfig {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_STRICT_MCP_CONFIG",
			Value: "true",
		})
	}

	// MCP timeout.
	if instance.Spec.Claude.MCPTimeout != nil && *instance.Spec.Claude.MCPTimeout > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "MCP_TIMEOUT",
			Value: strconv.Itoa(*instance.Spec.Claude.MCPTimeout),
		})
	}

	// Max MCP output tokens.
	if instance.Spec.Claude.MaxMCPOutputTokens != nil && *instance.Spec.Claude.MaxMCPOutputTokens > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "MAX_MCP_OUTPUT_TOKENS",
			Value: strconv.Itoa(*instance.Spec.Claude.MaxMCPOutputTokens),
		})
	}

	// MCP server secrets (dynamic env vars from Kubernetes Secrets).
	for _, mcpSecret := range instance.Spec.Claude.MCPServerSecrets {
		for envVar, secretKey := range mcpSecret.Env {
			envs = append(envs, corev1.EnvVar{
				Name: envVar,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: mcpSecret.SecretName},
						Key:                  secretKey,
					},
				},
			})
		}
	}

	// Max budget.
	if instance.Spec.Claude.MaxBudgetUSD != nil && *instance.Spec.Claude.MaxBudgetUSD > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_MAX_BUDGET_USD",
			Value: fmt.Sprintf("%.2f", *instance.Spec.Claude.MaxBudgetUSD),
		})
	}

	// Effort.
	if instance.Spec.Claude.Effort != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_EFFORT",
			Value: instance.Spec.Claude.Effort,
		})
	}

	// Fallback model.
	if instance.Spec.Claude.FallbackModel != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_FALLBACK_MODEL",
			Value: instance.Spec.Claude.FallbackModel,
		})
	}

	// JSON schema (from ConfigMap).
	if instance.Spec.Claude.JSONSchema != "" {
		envs = append(envs, envFromConfigMap("CLAUDE_JSON_SCHEMA", configMapName, "json-schema"))
	}

	// Settings file.
	if HasHooks(instance) {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_SETTINGS_FILE",
			Value: SettingsFilePath,
		})
	} else if instance.Spec.Claude.SettingsFile != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_SETTINGS_FILE",
			Value: instance.Spec.Claude.SettingsFile,
		})
	}

	// Setting sources.
	if instance.Spec.Claude.SettingSources != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_SETTING_SOURCES",
			Value: instance.Spec.Claude.SettingSources,
		})
	}

	// Tool control.
	if len(instance.Spec.Claude.Tools) > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_TOOLS",
			Value: strings.Join(instance.Spec.Claude.Tools, ","),
		})
	}
	if len(instance.Spec.Claude.AllowedTools) > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_ALLOWED_TOOLS",
			Value: strings.Join(instance.Spec.Claude.AllowedTools, ","),
		})
	}
	if len(instance.Spec.Claude.DisallowedTools) > 0 {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_DISALLOWED_TOOLS",
			Value: strings.Join(instance.Spec.Claude.DisallowedTools, ","),
		})
	}

	// Plugin dirs.
	pluginDirs := buildPluginDirs(instance)
	if pluginDirs != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_PLUGIN_DIRS",
			Value: pluginDirs,
		})
	}

	// Add dirs.
	addDirs := buildAddDirs(instance)
	if addDirs != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_ADD_DIRS",
			Value: addDirs,
		})
	}

	// Additional directories memory loading.
	hasAddDirs := len(instance.Spec.AddDirs) > 0 || HasInlineExtensions(instance)
	loadMemory := instance.Spec.LoadAdditionalDirsMemory == nil || *instance.Spec.LoadAdditionalDirsMemory
	if hasAddDirs && loadMemory {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD",
			Value: "true",
		})
	}

	// Agents (from ConfigMap).
	if len(instance.Spec.Claude.Agents) > 0 {
		envs = append(envs, envFromConfigMap("CLAUDE_AGENTS", configMapName, "agents"))
	}

	// Active agent.
	if instance.Spec.Claude.ActiveAgent != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_ACTIVE_AGENT",
			Value: instance.Spec.Claude.ActiveAgent,
		})
	}

	// Persistent mode.
	if instance.Spec.Claude.PersistentMode != nil && *instance.Spec.Claude.PersistentMode {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_PERSISTENT_MODE",
			Value: "true",
		})
	}

	// Include partial messages.
	if instance.Spec.Claude.IncludePartialMessages != nil && *instance.Spec.Claude.IncludePartialMessages {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_INCLUDE_PARTIAL_MESSAGES",
			Value: "true",
		})
	}

	// No session persistence.
	if instance.Spec.Claude.NoSessionPersistence != nil && *instance.Spec.Claude.NoSessionPersistence {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_NO_SESSION_PERSISTENCE",
			Value: "true",
		})
	}

	// Owner subject for JWT-based access control.
	if instance.Spec.Owner != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "KLAUS_OWNER_SUBJECT",
			Value: instance.Spec.Owner,
		})
	}

	// Telemetry.
	envs = append(envs, buildTelemetryEnvVars(instance)...)

	return envs
}

func envFromConfigMap(envName, configMapName, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: envName,
		ValueFrom: &corev1.EnvVarSource{
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: configMapName},
				Key:                  key,
			},
		},
	}
}

func buildPluginDirs(instance *klausv1alpha1.KlausInstance) string {
	var dirs []string
	// User-provided plugin directories first.
	dirs = append(dirs, instance.Spec.PluginDirs...)
	// Then OCI plugin mount paths.
	for _, plugin := range instance.Spec.Plugins {
		dirs = append(dirs, PluginMountPath(plugin))
	}
	return strings.Join(dirs, ",")
}

func buildAddDirs(instance *klausv1alpha1.KlausInstance) string {
	var dirs []string
	dirs = append(dirs, instance.Spec.AddDirs...)
	if HasInlineExtensions(instance) {
		dirs = append(dirs, ExtensionsBasePath)
	}
	return strings.Join(dirs, ",")
}

func buildTelemetryEnvVars(instance *klausv1alpha1.KlausInstance) []corev1.EnvVar {
	tel := instance.Spec.Telemetry
	if tel == nil || tel.Enabled == nil || !*tel.Enabled {
		return nil
	}

	var envs []corev1.EnvVar

	envs = append(envs, corev1.EnvVar{
		Name:  "CLAUDE_CODE_ENABLE_TELEMETRY",
		Value: "1",
	})

	if tel.MetricsExporter != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "OTEL_METRICS_EXPORTER",
			Value: tel.MetricsExporter,
		})
	}
	if tel.LogsExporter != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "OTEL_LOGS_EXPORTER",
			Value: tel.LogsExporter,
		})
	}
	if tel.OTLP != nil {
		if tel.OTLP.Protocol != "" {
			envs = append(envs, corev1.EnvVar{
				Name:  "OTEL_EXPORTER_OTLP_PROTOCOL",
				Value: tel.OTLP.Protocol,
			})
		}
		if tel.OTLP.Endpoint != "" {
			envs = append(envs, corev1.EnvVar{
				Name:  "OTEL_EXPORTER_OTLP_ENDPOINT",
				Value: tel.OTLP.Endpoint,
			})
		}
		if tel.OTLP.Headers != "" {
			envs = append(envs, corev1.EnvVar{
				Name:  "OTEL_EXPORTER_OTLP_HEADERS",
				Value: tel.OTLP.Headers,
			})
		}
	}
	if tel.MetricExportIntervalMs != nil {
		envs = append(envs, corev1.EnvVar{
			Name:  "OTEL_METRIC_EXPORT_INTERVAL",
			Value: strconv.Itoa(*tel.MetricExportIntervalMs),
		})
	}
	if tel.LogsExportIntervalMs != nil {
		envs = append(envs, corev1.EnvVar{
			Name:  "OTEL_BLRP_SCHEDULE_DELAY",
			Value: strconv.Itoa(*tel.LogsExportIntervalMs),
		})
	}

	// Privacy controls.
	if tel.LogUserPrompts != nil && *tel.LogUserPrompts {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_CODE_LOG_USER_PROMPTS",
			Value: "true",
		})
	}
	if tel.LogToolDetails != nil && *tel.LogToolDetails {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_CODE_LOG_TOOL_DETAILS",
			Value: "true",
		})
	}

	// Cardinality controls.
	if tel.IncludeSessionID != nil && !*tel.IncludeSessionID {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_CODE_INCLUDE_SESSION_ID",
			Value: "false",
		})
	}
	if tel.IncludeVersion != nil && *tel.IncludeVersion {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_CODE_INCLUDE_VERSION",
			Value: "true",
		})
	}
	if tel.IncludeAccountUUID != nil && !*tel.IncludeAccountUUID {
		envs = append(envs, corev1.EnvVar{
			Name:  "CLAUDE_CODE_INCLUDE_ACCOUNT_UUID",
			Value: "false",
		})
	}
	if tel.ResourceAttributes != "" {
		envs = append(envs, corev1.EnvVar{
			Name:  "OTEL_RESOURCE_ATTRIBUTES",
			Value: tel.ResourceAttributes,
		})
	}

	return envs
}
