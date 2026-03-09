package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// --- stop_instance tests ---

func TestHandleStopInstance_Success(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "my-agent"}

	result, err := s.handleStopInstance(authCtx("user@example.com"), req)
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
	if data["status"] != "stopping" {
		t.Errorf("status = %v, want %q", data["status"], "stopping")
	}

	// Verify the spec was patched.
	var updated klausv1alpha1.KlausInstance
	if err := c.Get(authCtx("user@example.com"), types.NamespacedName{
		Name: "my-agent", Namespace: "klaus-system",
	}, &updated); err != nil {
		t.Fatalf("failed to get updated instance: %v", err)
	}
	if !updated.Spec.Stopped {
		t.Error("expected spec.stopped to be true after stop")
	}
}

func TestHandleStopInstance_AlreadyStopped(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:   "user@example.com",
			Stopped: true,
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStateStopped,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "my-agent"}

	result, err := s.handleStopInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result for already-stopped instance")
	}

	var data map[string]any
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if data["status"] != "already_stopped" {
		t.Errorf("status = %v, want %q", data["status"], "already_stopped")
	}
}

func TestHandleStopInstance_AccessDenied(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "other@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "my-agent"}

	result, err := s.handleStopInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for access denied")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "access denied") {
		t.Errorf("error message = %q, want it to contain 'access denied'", text)
	}
}

func TestHandleStopInstance_NotFound(t *testing.T) {
	scheme := testScheme(t)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "nonexistent"}

	result, err := s.handleStopInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for not found")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "not found") {
		t.Errorf("error message = %q, want it to contain 'not found'", text)
	}
}

// --- start_instance tests ---

func TestHandleStartInstance_Success(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:   "user@example.com",
			Stopped: true,
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStateStopped,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "my-agent"}

	result, err := s.handleStartInstance(authCtx("user@example.com"), req)
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
	if data["status"] != "starting" {
		t.Errorf("status = %v, want %q", data["status"], "starting")
	}

	// Verify the spec was patched.
	var updated klausv1alpha1.KlausInstance
	if err := c.Get(authCtx("user@example.com"), types.NamespacedName{
		Name: "my-agent", Namespace: "klaus-system",
	}, &updated); err != nil {
		t.Fatalf("failed to get updated instance: %v", err)
	}
	if updated.Spec.Stopped {
		t.Error("expected spec.stopped to be false after start")
	}
}

func TestHandleStartInstance_AlreadyRunning(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "my-agent"}

	result, err := s.handleStartInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected non-error result for already-running instance")
	}

	var data map[string]any
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if data["status"] != "already_running" {
		t.Errorf("status = %v, want %q", data["status"], "already_running")
	}
}

func TestHandleStartInstance_AccessDenied(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner:   "other@example.com",
			Stopped: true,
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStateStopped,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "my-agent"}

	result, err := s.handleStartInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for access denied")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "access denied") {
		t.Errorf("error message = %q, want it to contain 'access denied'", text)
	}
}

func TestHandleStartInstance_NotFound(t *testing.T) {
	scheme := testScheme(t)

	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "nonexistent"}

	result, err := s.handleStartInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for not found")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "not found") {
		t.Errorf("error message = %q, want it to contain 'not found'", text)
	}
}
