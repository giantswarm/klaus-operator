package oci

import (
	"fmt"

	klausoci "github.com/giantswarm/klaus-oci"
	"sigs.k8s.io/yaml"
)

// PersonalitySpec extends the shared klausoci.PersonalitySpec with
// operator-specific fields parsed from personality.yaml. The shared fields
// (Description, Image, Plugins) come from the klaus-oci library; fields like
// SystemPrompt, AppendSystemPrompt, and Soul are operator-specific extensions.
type PersonalitySpec struct {
	// Description is a human-readable description of this personality.
	Description string `json:"description,omitempty"`

	// Image is the optional container image for instances using this personality.
	// When set, overrides the operator's default klausImage.
	Image string `json:"image,omitempty"`

	// Plugins defines OCI image references for plugins provided by this personality.
	Plugins []klausoci.PluginReference `json:"plugins,omitempty"`

	// SystemPrompt overrides the default system prompt for instances.
	SystemPrompt string `json:"systemPrompt,omitempty"`

	// AppendSystemPrompt appends text to the system prompt for instances.
	AppendSystemPrompt string `json:"appendSystemPrompt,omitempty"`

	// Soul is the content of SOUL.md, mounted as a ConfigMap entry into the
	// instance container at /etc/klaus/SOUL.md.
	Soul string `json:"soul,omitempty"`
}

// copy returns a deep copy of the PersonalitySpec, safe for caller mutation
// without corrupting cached instances.
func (s *PersonalitySpec) copy() *PersonalitySpec {
	cp := *s
	if len(s.Plugins) > 0 {
		cp.Plugins = make([]klausoci.PluginReference, len(s.Plugins))
		copy(cp.Plugins, s.Plugins)
	}
	return &cp
}

// ParsePersonalitySpec parses personality.yaml bytes into a PersonalitySpec.
func ParsePersonalitySpec(data []byte) (*PersonalitySpec, error) {
	var spec PersonalitySpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing personality.yaml: %w", err)
	}
	return &spec, nil
}
