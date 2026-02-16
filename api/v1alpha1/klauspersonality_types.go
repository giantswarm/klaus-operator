package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// KlausPersonalitySpec defines a reusable template configuration that can be
// referenced by KlausInstance resources via personalityRef. The spec covers
// the same configuration surface as KlausInstanceSpec, minus fields that are
// inherently per-instance (owner, personalityRef, workspace, muster,
// imagePullSecrets).
type KlausPersonalitySpec struct {
	// Description is a human-readable description of this personality.
	// +optional
	Description string `json:"description,omitempty"`

	// Image is the container image for instances using this personality.
	// When set, overrides the operator's default klausImage.
	// This should be a composite image containing both the language toolchain
	// and the Klaus agent.
	// +optional
	Image string `json:"image,omitempty"`

	// Claude contains all Claude Code agent configuration defaults.
	// +optional
	Claude ClaudeConfig `json:"claude,omitempty"`

	// Plugins defines default OCI image references rendered as Kubernetes image volumes.
	// +optional
	Plugins []PluginReference `json:"plugins,omitempty"`

	// PluginDirs specifies default additional plugin directory paths.
	// +optional
	PluginDirs []string `json:"pluginDirs,omitempty"`

	// MCPServers references shared KlausMCPServer CRDs by name.
	// +optional
	MCPServers []MCPServerReference `json:"mcpServers,omitempty"`

	// Skills defines default inline skill configurations rendered as SKILL.md files.
	// +optional
	Skills map[string]SkillConfig `json:"skills,omitempty"`

	// AgentFiles defines default inline markdown-format subagent definitions.
	// +optional
	AgentFiles map[string]AgentFileConfig `json:"agentFiles,omitempty"`

	// Hooks defines default lifecycle hooks rendered to settings.json.
	// Mutually exclusive with Claude.SettingsFile.
	// +optional
	Hooks map[string]runtime.RawExtension `json:"hooks,omitempty"`

	// HookScripts defines default executable scripts mounted at /etc/klaus/hooks/.
	// +optional
	HookScripts map[string]string `json:"hookScripts,omitempty"`

	// AddDirs specifies default additional directories to load.
	// +optional
	AddDirs []string `json:"addDirs,omitempty"`

	// LoadAdditionalDirsMemory enables CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD.
	// +optional
	LoadAdditionalDirsMemory *bool `json:"loadAdditionalDirsMemory,omitempty"`

	// Resources specifies default compute resource requirements for instance pods.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`

	// Telemetry configures default OpenTelemetry and Prometheus metrics settings.
	// +optional
	Telemetry *TelemetryConfig `json:"telemetry,omitempty"`
}

// KlausPersonalityStatus defines the observed state of a KlausPersonality.
type KlausPersonalityStatus struct {
	// Conditions represent the latest available observations of the personality's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// InstanceCount is the number of KlausInstance resources referencing this personality.
	// +optional
	InstanceCount int `json:"instanceCount,omitempty"`

	// PluginCount is the number of plugins defined in this personality.
	// +optional
	PluginCount int `json:"pluginCount,omitempty"`

	// MCPServerCount is the number of MCP servers referenced by this personality.
	// +optional
	MCPServerCount int `json:"mcpServerCount,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Description",type=string,JSONPath=`.spec.description`
// +kubebuilder:printcolumn:name="Instances",type=integer,JSONPath=`.status.instanceCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=kp

// KlausPersonality defines a reusable template for KlausInstance configuration.
// Instances can reference a personality via personalityRef to inherit its
// defaults. Instance-specific fields override personality defaults according to
// defined merge semantics.
type KlausPersonality struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KlausPersonalitySpec   `json:"spec,omitempty"`
	Status KlausPersonalityStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KlausPersonalityList contains a list of KlausPersonality.
type KlausPersonalityList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KlausPersonality `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KlausPersonality{}, &KlausPersonalityList{})
}
