package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KlausMCPServerSpec defines a shared MCP server configuration. Fields map
// directly to the MCP server config format used in .mcp.json, except for
// SecretRefs which are used for pod-level credential injection only.
type KlausMCPServerSpec struct {
	// Type is the transport type (e.g. "streamable-http", "sse", "stdio").
	Type string `json:"type"`

	// URL for HTTP-based MCP servers.
	// +optional
	URL string `json:"url,omitempty"`

	// Command for stdio-based MCP servers.
	// +optional
	Command string `json:"command,omitempty"`

	// Args for stdio-based MCP servers.
	// +optional
	Args []string `json:"args,omitempty"`

	// Env contains static environment variables for the MCP server process.
	// Values support ${VAR} expansion -- the referenced variables are resolved
	// from SecretRefs at pod startup.
	// These are included in the .mcp.json config.
	// +optional
	Env map[string]string `json:"env,omitempty"`

	// Headers contains HTTP headers for HTTP-based MCP servers.
	// Values support ${VAR} expansion -- the referenced variables are resolved
	// from SecretRefs at pod startup.
	// These are included in the .mcp.json config.
	// +optional
	Headers map[string]string `json:"headers,omitempty"`

	// SecretRefs defines Kubernetes Secret references for credential injection.
	// Each entry maps environment variable names to Secret keys, enabling
	// ${VAR} expansion in the MCP config. Secrets must exist in the operator
	// namespace; they are copied to instance user namespaces at reconcile time.
	// +optional
	SecretRefs []MCPServerSecret `json:"secretRefs,omitempty"`
}

// KlausMCPServerStatus defines the observed state of a KlausMCPServer.
type KlausMCPServerStatus struct {
	// Conditions represent the latest available observations of the server's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// InstanceCount is the number of KlausInstance resources referencing this server.
	// +optional
	InstanceCount int `json:"instanceCount,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Instances",type=integer,JSONPath=`.status.instanceCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:shortName=kmcp

// KlausMCPServer defines a shared MCP server configuration that can be
// referenced by KlausInstance resources. The operator resolves these
// references and assembles the MCP config with Secret injection.
type KlausMCPServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KlausMCPServerSpec   `json:"spec,omitempty"`
	Status KlausMCPServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KlausMCPServerList contains a list of KlausMCPServer.
type KlausMCPServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KlausMCPServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KlausMCPServer{}, &KlausMCPServerList{})
}
