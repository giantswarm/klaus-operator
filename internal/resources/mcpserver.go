package resources

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// BuildMCPServerCRD creates an unstructured MCPServer CRD for registering a
// Klaus instance in muster. We use an unstructured object to avoid importing
// muster's types.
func BuildMCPServerCRD(instance *klausv1alpha1.KlausInstance, instanceNamespace string) *unstructured.Unstructured {
	musterNamespace := MusterNamespace(instance)
	toolPrefix := ""
	if instance.Spec.Muster != nil {
		toolPrefix = instance.Spec.Muster.ToolPrefix
	}

	endpoint := ServiceEndpoint(instance, instanceNamespace)

	spec := map[string]any{
		"type": "streamable-http",
		"url":  endpoint + "/mcp",
		"auth": map[string]any{
			"forwardToken": true,
		},
	}
	if toolPrefix != "" {
		spec["toolPrefix"] = toolPrefix
	}

	mcpServer := &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "muster.giantswarm.io/v1alpha1",
			"kind":       "MCPServer",
			"metadata": map[string]any{
				"name":      "klaus-" + instance.Name,
				"namespace": musterNamespace,
				"labels": map[string]any{
					LabelManagedBy:               AppKlausOperator,
					"app.kubernetes.io/instance": instance.Name,
					LabelOwner:                   sanitizeLabelValue(instance.Spec.Owner),
				},
			},
			"spec": spec,
		},
	}

	return mcpServer
}

// BuildOperatorMCPServerCRD creates an MCPServer CRD for the operator itself.
func BuildOperatorMCPServerCRD(operatorServiceURL, musterNamespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]any{
			"apiVersion": "muster.giantswarm.io/v1alpha1",
			"kind":       "MCPServer",
			"metadata": map[string]any{
				"name":      AppKlausOperator,
				"namespace": musterNamespace,
				"labels": map[string]any{
					LabelManagedBy: AppKlausOperator,
					LabelAppName:   AppKlausOperator,
				},
			},
			"spec": map[string]any{
				"type": "streamable-http",
				"url":  operatorServiceURL + "/mcp",
				"auth": map[string]any{
					"forwardToken": true,
				},
			},
		},
	}
}
