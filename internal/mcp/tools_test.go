package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

func TestHandleGetInstance_ToolchainIncluded(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := klausv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-instance",
			Namespace:         "klaus-system",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
			Claude: klausv1alpha1.ClaudeConfig{
				Model: "claude-sonnet-4-20250514",
			},
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State:     klausv1alpha1.InstanceStateRunning,
			Toolchain: "gsoci.azurecr.io/giantswarm/klaus-go:1.25",
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance).
		Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	ctx := context.WithValue(context.Background(), authTokenKey,
		"Bearer "+buildTestJWT(`{"email":"user@example.com"}`))

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "test-instance"}

	result, err := s.handleGetInstance(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	var data map[string]any
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if data["toolchain"] != "gsoci.azurecr.io/giantswarm/klaus-go:1.25" {
		t.Errorf("toolchain = %v, want %q", data["toolchain"], "gsoci.azurecr.io/giantswarm/klaus-go:1.25")
	}
}

func TestHandleGetInstance_ToolchainOmittedWhenEmpty(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := klausv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-instance",
			Namespace:         "klaus-system",
			CreationTimestamp: metav1.NewTime(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)),
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStateRunning,
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance).
		Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	ctx := context.WithValue(context.Background(), authTokenKey,
		"Bearer "+buildTestJWT(`{"email":"user@example.com"}`))

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "test-instance"}

	result, err := s.handleGetInstance(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	var data map[string]any
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if _, exists := data["toolchain"]; exists {
		t.Errorf("toolchain key should be omitted when empty, got %v", data["toolchain"])
	}
}
