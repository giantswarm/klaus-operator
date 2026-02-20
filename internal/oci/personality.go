package oci

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// PersonalitySpec is the parsed content of a personality.yaml file from an OCI
// personality artifact. Fields here drive merge semantics in the instance controller.
type PersonalitySpec struct {
	// Description is a human-readable description of this personality.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Image is the optional container image for instances using this personality.
	// When set, overrides the operator's default klausImage.
	Image string `yaml:"image,omitempty" json:"image,omitempty"`

	// Plugins defines OCI image references for plugins provided by this personality.
	Plugins []PersonalityPlugin `yaml:"plugins,omitempty" json:"plugins,omitempty"`

	// SystemPrompt overrides the default system prompt for instances.
	SystemPrompt string `yaml:"systemPrompt,omitempty" json:"systemPrompt,omitempty"`

	// AppendSystemPrompt appends text to the system prompt for instances.
	AppendSystemPrompt string `yaml:"appendSystemPrompt,omitempty" json:"appendSystemPrompt,omitempty"`

	// Soul is the content of SOUL.md, mounted as a ConfigMap entry into the
	// instance container at /etc/klaus/SOUL.md (informational; not yet wired
	// into environment variables but available for future use).
	Soul string `yaml:"soul,omitempty" json:"soul,omitempty"`
}

// PersonalityPlugin defines an OCI plugin reference within a personality artifact.
type PersonalityPlugin struct {
	// Repository is the OCI image repository.
	Repository string `yaml:"repository" json:"repository"`

	// Tag is the image tag. Mutually exclusive with Digest.
	// +optional
	Tag string `yaml:"tag,omitempty" json:"tag,omitempty"`

	// Digest is the image digest (sha256:...). Mutually exclusive with Tag.
	// +optional
	Digest string `yaml:"digest,omitempty" json:"digest,omitempty"`
}

// ParsePersonalitySpec parses personality.yaml bytes into a PersonalitySpec.
func ParsePersonalitySpec(data []byte) (*PersonalitySpec, error) {
	var spec PersonalitySpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parsing personality.yaml: %w", err)
	}
	return &spec, nil
}
