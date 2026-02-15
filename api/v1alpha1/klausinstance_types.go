package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// KlausInstanceSpec defines the desired state of a KlausInstance.
type KlausInstanceSpec struct {
	// PersonalityRef references a KlausPersonality template to use as defaults.
	// +optional
	PersonalityRef *PersonalityReference `json:"personalityRef,omitempty"`

	// Owner is the user identity (email) that owns this instance.
	// Used for access control and namespace isolation.
	Owner string `json:"owner"`

	// Claude contains all Claude Code agent configuration.
	// +optional
	Claude ClaudeConfig `json:"claude,omitempty"`

	// Plugins defines OCI image references rendered as Kubernetes image volumes.
	// +optional
	Plugins []PluginReference `json:"plugins,omitempty"`

	// MCPServers references shared KlausMCPServer CRDs by name.
	// +optional
	MCPServers []MCPServerReference `json:"mcpServers,omitempty"`

	// Skills defines inline skill configurations rendered as SKILL.md files.
	// +optional
	Skills map[string]SkillConfig `json:"skills,omitempty"`

	// AgentFiles defines inline markdown-format subagent definitions.
	// +optional
	AgentFiles map[string]AgentFileConfig `json:"agentFiles,omitempty"`

	// Hooks defines lifecycle hooks rendered to settings.json.
	// Mutually exclusive with Claude.SettingsFile.
	// +optional
	Hooks map[string]runtime.RawExtension `json:"hooks,omitempty"`

	// HookScripts defines executable scripts mounted at /etc/klaus/hooks/.
	// +optional
	HookScripts map[string]string `json:"hookScripts,omitempty"`

	// AddDirs specifies additional directories to load.
	// +optional
	AddDirs []string `json:"addDirs,omitempty"`

	// LoadAdditionalDirsMemory enables CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD.
	// +optional
	LoadAdditionalDirsMemory *bool `json:"loadAdditionalDirsMemory,omitempty"`

	// Workspace configures persistent storage for the instance.
	// +optional
	Workspace *WorkspaceConfig `json:"workspace,omitempty"`

	// Resources specifies compute resource requirements for the instance pod.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Telemetry configures OpenTelemetry and Prometheus metrics.
	// +optional
	Telemetry *TelemetryConfig `json:"telemetry,omitempty"`

	// Muster configures MCPServer CRD registration in the muster namespace.
	// +optional
	Muster *MusterConfig `json:"muster,omitempty"`
}

// PersonalityReference references a KlausPersonality by name.
type PersonalityReference struct {
	// Name is the name of the KlausPersonality resource.
	Name string `json:"name"`
}

// ClaudeConfig contains all Claude Code agent configuration options.
// This mirrors the Helm chart's claude.* values.
type ClaudeConfig struct {
	// Model specifies the Claude model to use.
	// +optional
	Model string `json:"model,omitempty"`

	// MaxTurns limits the number of agentic turns. 0 means unlimited.
	// +optional
	MaxTurns *int `json:"maxTurns,omitempty"`

	// PermissionMode controls tool permission handling.
	// +kubebuilder:default=bypassPermissions
	// +optional
	PermissionMode string `json:"permissionMode,omitempty"`

	// SystemPrompt overrides the default system prompt.
	// +optional
	SystemPrompt string `json:"systemPrompt,omitempty"`

	// AppendSystemPrompt appends text to the default system prompt.
	// +optional
	AppendSystemPrompt string `json:"appendSystemPrompt,omitempty"`

	// MCPServers defines inline MCP server configuration (free-form map rendered to .mcp.json).
	// +optional
	MCPServers map[string]runtime.RawExtension `json:"mcpServers,omitempty"`

	// MCPServerSecrets defines secret references for ${VAR} expansion in MCP config.
	// +optional
	MCPServerSecrets []MCPServerSecret `json:"mcpServerSecrets,omitempty"`

	// MCPTimeout sets the MCP_TIMEOUT env var (milliseconds).
	// +optional
	MCPTimeout *int `json:"mcpTimeout,omitempty"`

	// MaxMCPOutputTokens sets MAX_MCP_OUTPUT_TOKENS.
	// +optional
	MaxMCPOutputTokens *int `json:"maxMcpOutputTokens,omitempty"`

	// StrictMCPConfig prevents loading MCP configs from user/project/local sources.
	// +kubebuilder:default=true
	// +optional
	StrictMCPConfig *bool `json:"strictMcpConfig,omitempty"`

	// MaxBudgetUSD sets the maximum spend per session in USD.
	// +optional
	MaxBudgetUSD *float64 `json:"maxBudgetUSD,omitempty"`

	// Effort controls thinking effort level (low, medium, high).
	// +optional
	Effort string `json:"effort,omitempty"`

	// FallbackModel specifies a fallback model if the primary is unavailable.
	// +optional
	FallbackModel string `json:"fallbackModel,omitempty"`

	// JSONSchema defines structured output schema.
	// +optional
	JSONSchema string `json:"jsonSchema,omitempty"`

	// SettingsFile path to a custom settings file. Mutually exclusive with Hooks.
	// +optional
	SettingsFile string `json:"settingsFile,omitempty"`

	// SettingSources controls which settings sources are loaded.
	// +optional
	SettingSources string `json:"settingSources,omitempty"`

	// Tools specifies tools to enable.
	// +optional
	Tools []string `json:"tools,omitempty"`

	// AllowedTools restricts which tools can be used.
	// +optional
	AllowedTools []string `json:"allowedTools,omitempty"`

	// DisallowedTools prevents specific tools from being used.
	// +optional
	DisallowedTools []string `json:"disallowedTools,omitempty"`

	// Agents defines JSON-format subagent configurations.
	// +optional
	Agents map[string]runtime.RawExtension `json:"agents,omitempty"`

	// ActiveAgent selects the top-level agent.
	// +optional
	ActiveAgent string `json:"activeAgent,omitempty"`

	// PersistentMode enables bidirectional stream-json mode instead of single-shot.
	// +optional
	PersistentMode *bool `json:"persistentMode,omitempty"`

	// IncludePartialMessages enables streaming partial messages.
	// +optional
	IncludePartialMessages *bool `json:"includePartialMessages,omitempty"`

	// NoSessionPersistence disables session persistence.
	// +optional
	NoSessionPersistence *bool `json:"noSessionPersistence,omitempty"`
}

// MCPServerSecret defines a Kubernetes Secret reference for MCP server credential injection.
type MCPServerSecret struct {
	// SecretName is the name of the Kubernetes Secret.
	SecretName string `json:"secretName"`

	// Env maps environment variable names to Secret keys.
	Env map[string]string `json:"env"`
}

// PluginReference defines an OCI image reference for a Klaus plugin.
type PluginReference struct {
	// Repository is the OCI image repository.
	Repository string `json:"repository"`

	// Tag is the image tag. Mutually exclusive with Digest.
	// +optional
	Tag string `json:"tag,omitempty"`

	// Digest is the image digest (sha256:...). Mutually exclusive with Tag.
	// +optional
	Digest string `json:"digest,omitempty"`
}

// MCPServerReference references a KlausMCPServer CRD by name.
type MCPServerReference struct {
	// Name is the name of the KlausMCPServer resource.
	Name string `json:"name"`
}

// SkillConfig defines an inline skill rendered as SKILL.md with YAML frontmatter.
type SkillConfig struct {
	// Description is the skill description in frontmatter.
	// +optional
	Description string `json:"description,omitempty"`

	// Content is the body text of the SKILL.md file.
	Content string `json:"content"`

	// DisableModelInvocation prevents the model from invoking this skill.
	// +optional
	DisableModelInvocation *bool `json:"disableModelInvocation,omitempty"`

	// UserInvocable allows users to invoke this skill via slash commands.
	// +optional
	UserInvocable *bool `json:"userInvocable,omitempty"`

	// AllowedTools restricts which tools this skill can use.
	// +optional
	AllowedTools string `json:"allowedTools,omitempty"`

	// Model overrides the model for this skill.
	// +optional
	Model string `json:"model,omitempty"`

	// Context provides additional context configuration.
	// +optional
	Context *runtime.RawExtension `json:"context,omitempty"`

	// Agent assigns this skill to a specific agent.
	// +optional
	Agent string `json:"agent,omitempty"`

	// ArgumentHint provides input hints for the skill.
	// +optional
	ArgumentHint string `json:"argumentHint,omitempty"`
}

// AgentFileConfig defines an inline markdown-format subagent definition.
type AgentFileConfig struct {
	// Content is the raw markdown content of the agent file.
	Content string `json:"content"`
}

// WorkspaceConfig configures persistent storage for the instance.
type WorkspaceConfig struct {
	// StorageClass is the storage class for the PVC.
	// +optional
	StorageClass string `json:"storageClass,omitempty"`

	// Size is the requested storage size.
	// +kubebuilder:default="5Gi"
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`

	// GitRepo is a git repository URL to clone into the workspace.
	// +optional
	GitRepo string `json:"gitRepo,omitempty"`

	// GitRef is the git ref to checkout.
	// +optional
	GitRef string `json:"gitRef,omitempty"`
}

// TelemetryConfig configures OpenTelemetry and metrics for the instance.
type TelemetryConfig struct {
	// Enabled enables telemetry collection.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// MetricsExporter specifies the metrics exporter (e.g. "otlp").
	// +optional
	MetricsExporter string `json:"metricsExporter,omitempty"`

	// LogsExporter specifies the logs exporter (e.g. "otlp").
	// +optional
	LogsExporter string `json:"logsExporter,omitempty"`

	// OTLP contains OTLP exporter configuration.
	// +optional
	OTLP *OTLPConfig `json:"otlp,omitempty"`

	// MetricExportIntervalMs sets the metric export interval in milliseconds.
	// +optional
	MetricExportIntervalMs *int `json:"metricExportIntervalMs,omitempty"`

	// LogsExportIntervalMs sets the logs export interval in milliseconds.
	// +optional
	LogsExportIntervalMs *int `json:"logsExportIntervalMs,omitempty"`

	// LogUserPrompts enables logging of user prompts.
	// +optional
	LogUserPrompts *bool `json:"logUserPrompts,omitempty"`

	// LogToolDetails enables logging of tool details.
	// +optional
	LogToolDetails *bool `json:"logToolDetails,omitempty"`

	// IncludeSessionID includes session ID in telemetry.
	// +optional
	IncludeSessionID *bool `json:"includeSessionId,omitempty"`

	// IncludeVersion includes version in telemetry.
	// +optional
	IncludeVersion *bool `json:"includeVersion,omitempty"`

	// IncludeAccountUUID includes account UUID in telemetry.
	// +optional
	IncludeAccountUUID *bool `json:"includeAccountUuid,omitempty"`

	// ResourceAttributes sets OTEL_RESOURCE_ATTRIBUTES.
	// +optional
	ResourceAttributes string `json:"resourceAttributes,omitempty"`
}

// OTLPConfig contains OTLP exporter settings.
type OTLPConfig struct {
	// Protocol is the OTLP protocol (e.g. "grpc", "http/protobuf").
	// +optional
	Protocol string `json:"protocol,omitempty"`

	// Endpoint is the OTLP endpoint URL.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Headers are additional OTLP headers.
	// +optional
	Headers string `json:"headers,omitempty"`
}

// MusterConfig configures MCPServer CRD registration.
type MusterConfig struct {
	// Namespace is the namespace where the MCPServer CRD will be created.
	// +kubebuilder:default=muster
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// ToolPrefix is prepended to tool names in the MCPServer registration.
	// +optional
	ToolPrefix string `json:"toolPrefix,omitempty"`
}

// InstanceState represents the lifecycle state of a KlausInstance.
// +kubebuilder:validation:Enum=Pending;Running;Error;Stopped
type InstanceState string

const (
	InstanceStatePending InstanceState = "Pending"
	InstanceStateRunning InstanceState = "Running"
	InstanceStateError   InstanceState = "Error"
	InstanceStateStopped InstanceState = "Stopped"
)

// KlausInstanceStatus defines the observed state of a KlausInstance.
type KlausInstanceStatus struct {
	// State is the current lifecycle state.
	// +optional
	State InstanceState `json:"state,omitempty"`

	// Endpoint is the internal service URL for the instance.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Mode indicates the process mode (single-shot or persistent).
	// +optional
	Mode string `json:"mode,omitempty"`

	// LastActivity is the timestamp of the last activity.
	// +optional
	LastActivity *metav1.Time `json:"lastActivity,omitempty"`

	// Personality is the name of the resolved KlausPersonality.
	// +optional
	Personality string `json:"personality,omitempty"`

	// PluginCount is the number of plugins loaded.
	// +optional
	PluginCount int `json:"pluginCount,omitempty"`

	// MCPServerCount is the number of MCP servers configured.
	// +optional
	MCPServerCount int `json:"mcpServerCount,omitempty"`

	// Conditions represent the latest available observations of the instance's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`
// +kubebuilder:printcolumn:name="Owner",type=string,JSONPath=`.spec.owner`
// +kubebuilder:printcolumn:name="Personality",type=string,JSONPath=`.status.personality`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// KlausInstance is the Schema for the klausinstances API.
// It represents a running Klaus agent instance with its configuration.
type KlausInstance struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KlausInstanceSpec   `json:"spec,omitempty"`
	Status KlausInstanceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KlausInstanceList contains a list of KlausInstance.
type KlausInstanceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KlausInstance `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KlausInstance{}, &KlausInstanceList{})
}
