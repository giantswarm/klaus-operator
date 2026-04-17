package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// simulateController polls until the named instance exists then transitions
// it to Running with the given endpoint. Stops when ctx is cancelled.
func simulateController(ctx context.Context, c client.Client, namespace, name, endpoint string) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var inst klausv1alpha1.KlausInstance
			if err := c.Get(ctx, types.NamespacedName{
				Name: name, Namespace: namespace,
			}, &inst); err != nil {
				continue
			}
			if inst.Status.State == klausv1alpha1.InstanceStateRunning {
				return
			}
			inst.Status.State = klausv1alpha1.InstanceStateRunning
			inst.Status.Endpoint = endpoint
			_ = c.Status().Update(ctx, &inst)
			return
		}
	}
}

// --- handleRunInstance tests ---

func TestHandleRunInstance_NonBlocking(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&klausv1alpha1.KlausInstance{}).Build()

	agent := &fakeAgentMCPClient{
		promptResult: textResult("prompt accepted"),
		sessionID:    "sess-run-1",
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	ctx, cancel := context.WithTimeout(authCtx("user@example.com"), 30*time.Second)
	defer cancel()

	// Simulate the controller transitioning the instance to Running.
	go simulateController(ctx, c, "klaus-system", "run-agent", "http://run-agent.klaus:8080")

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "run-agent",
		"message": "do something",
		"model":   "claude-sonnet-4-20250514",
	}

	result, err := s.handleRunInstance(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	var data runResult
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if data.Name != "run-agent" {
		t.Errorf("name = %q, want %q", data.Name, "run-agent")
	}
	if data.Status != "started" {
		t.Errorf("status = %q, want %q", data.Status, "started")
	}
	if data.SessionID != "sess-run-1" {
		t.Errorf("session_id = %q, want %q", data.SessionID, "sess-run-1")
	}
	if data.Owner != "user@example.com" {
		t.Errorf("owner = %q, want %q", data.Owner, "user@example.com")
	}
	if data.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", data.Model, "claude-sonnet-4-20250514")
	}

	if agent.lastPromptMessage != "do something" {
		t.Errorf("prompt message = %q, want %q", agent.lastPromptMessage, "do something")
	}
	if agent.lastBaseURL != "http://run-agent.klaus:8080/mcp" {
		t.Errorf("baseURL = %q, want %q", agent.lastBaseURL, "http://run-agent.klaus:8080/mcp")
	}
}

func TestHandleRunInstance_Blocking(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&klausv1alpha1.KlausInstance{}).Build()

	agent := &fakeAgentMCPClient{
		promptResult: textResult("prompt accepted"),
		statusResult: textResult(`{"status":"completed"}`),
		resultResult: textResult("Task finished successfully"),
		sessionID:    "sess-run-2",
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	ctx, cancel := context.WithTimeout(authCtx("user@example.com"), 30*time.Second)
	defer cancel()

	go simulateController(ctx, c, "klaus-system", "run-agent", "http://run-agent.klaus:8080")

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":     "run-agent",
		"message":  "do the work",
		"blocking": true,
	}

	result, err := s.handleRunInstance(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	var data runResult
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if data.Status != "completed" {
		t.Errorf("status = %q, want %q", data.Status, "completed")
	}
	if data.Result != "Task finished successfully" {
		t.Errorf("result = %q, want %q", data.Result, "Task finished successfully")
	}
}

func TestHandleRunInstance_MissingName(t *testing.T) {
	s := &Server{operatorNamespace: "klaus-system"}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"message": "hello",
	}

	result, err := s.handleRunInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for missing name")
	}
	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "name is required") {
		t.Errorf("error = %q, want it to contain 'name is required'", text)
	}
}

func TestHandleRunInstance_MissingMessage(t *testing.T) {
	s := &Server{operatorNamespace: "klaus-system"}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "my-agent",
	}

	result, err := s.handleRunInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for missing message")
	}
	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "message is required") {
		t.Errorf("error = %q, want it to contain 'message is required'", text)
	}
}

func TestHandleRunInstance_AlreadyExists(t *testing.T) {
	scheme := testScheme(t)
	existing := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-agent",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existing).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "existing-agent",
		"message": "hello",
	}

	result, err := s.handleRunInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for already existing instance")
	}
	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "already exists") {
		t.Errorf("error = %q, want it to contain 'already exists'", text)
	}
}

func TestHandleRunInstance_PromptError(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&klausv1alpha1.KlausInstance{}).Build()

	agent := &fakeAgentMCPClient{
		promptErr: fmt.Errorf("connection refused"),
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	ctx, cancel := context.WithTimeout(authCtx("user@example.com"), 30*time.Second)
	defer cancel()

	go simulateController(ctx, c, "klaus-system", "run-agent", "http://run-agent.klaus:8080")

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "run-agent",
		"message": "hello",
	}

	result, err := s.handleRunInstance(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for prompt failure")
	}
	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "prompt failed") {
		t.Errorf("error = %q, want it to contain 'prompt failed'", text)
	}
	if !strings.Contains(text, "connection refused") {
		t.Errorf("error = %q, want it to contain 'connection refused'", text)
	}
}

func TestHandleRunInstance_ReadinessTimeout(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&klausv1alpha1.KlausInstance{}).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       &fakeAgentMCPClient{},
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "timeout-agent",
		"message": "hello",
	}

	// Use a very short timeout to trigger readiness timeout.
	ctx, cancel := context.WithTimeout(authCtx("user@example.com"), 100*time.Millisecond)
	defer cancel()

	result, err := s.handleRunInstance(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the instance was created.
	var created klausv1alpha1.KlausInstance
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: "timeout-agent", Namespace: "klaus-system",
	}, &created); err != nil {
		t.Fatalf("instance should have been created: %v", err)
	}

	// The result should be an error about readiness timeout.
	if !result.IsError {
		t.Fatal("expected MCP error for readiness timeout")
	}
	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "readiness check failed") {
		t.Errorf("error = %q, want it to contain 'readiness check failed'", text)
	}
}

func TestHandleRunInstance_DefaultModel(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&klausv1alpha1.KlausInstance{}).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       &fakeAgentMCPClient{},
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "default-model-agent",
		"message": "hello",
	}

	// Short timeout -- we only care about verifying the created CR.
	ctx, cancel := context.WithTimeout(authCtx("user@example.com"), 100*time.Millisecond)
	defer cancel()

	_, _ = s.handleRunInstance(ctx, req)

	var created klausv1alpha1.KlausInstance
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: "default-model-agent", Namespace: "klaus-system",
	}, &created); err != nil {
		t.Fatalf("failed to get created instance: %v", err)
	}

	if created.Spec.Claude.Model != "claude-sonnet-4-20250514" {
		t.Errorf("model = %q, want %q", created.Spec.Claude.Model, "claude-sonnet-4-20250514")
	}
	if created.Spec.Owner != "user@example.com" {
		t.Errorf("owner = %q, want %q", created.Spec.Owner, "user@example.com")
	}
	if created.Spec.Claude.PermissionMode != klausv1alpha1.PermissionModeBypass {
		t.Errorf("permissionMode = %q, want %q", created.Spec.Claude.PermissionMode, klausv1alpha1.PermissionModeBypass)
	}
}

func TestHandleRunInstance_PersonalityAndSystemPrompt(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&klausv1alpha1.KlausInstance{}).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       &fakeAgentMCPClient{},
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":          "custom-agent",
		"message":       "hello",
		"system_prompt": "You are a helpful assistant",
		"personality":   "registry.io/personality:v1",
		"model":         "claude-opus-4-20250514",
	}

	ctx, cancel := context.WithTimeout(authCtx("user@example.com"), 100*time.Millisecond)
	defer cancel()

	_, _ = s.handleRunInstance(ctx, req)

	var created klausv1alpha1.KlausInstance
	if err := c.Get(context.Background(), types.NamespacedName{
		Name: "custom-agent", Namespace: "klaus-system",
	}, &created); err != nil {
		t.Fatalf("failed to get created instance: %v", err)
	}

	if created.Spec.Claude.SystemPrompt != "You are a helpful assistant" {
		t.Errorf("system_prompt = %q, want %q", created.Spec.Claude.SystemPrompt, "You are a helpful assistant")
	}
	if created.Spec.Personality != "registry.io/personality:v1" {
		t.Errorf("personality = %q, want %q", created.Spec.Personality, "registry.io/personality:v1")
	}
	if created.Spec.Claude.Model != "claude-opus-4-20250514" {
		t.Errorf("model = %q, want %q", created.Spec.Claude.Model, "claude-opus-4-20250514")
	}
}

func TestHandleRunInstance_AuthRequired(t *testing.T) {
	s := &Server{operatorNamespace: "klaus-system"}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "my-agent",
		"message": "hello",
	}

	result, err := s.handleRunInstance(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for missing auth")
	}
	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "authentication required") {
		t.Errorf("error = %q, want it to contain 'authentication required'", text)
	}
}

// --- waitForRunning tests ---

func TestWaitForRunning_ImmediatelyReady(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ready-agent",
			Namespace: "klaus-system",
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State:    klausv1alpha1.InstanceStateRunning,
			Endpoint: "http://ready-agent.klaus:8080",
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	endpoint, err := s.waitForRunning(ctx, "ready-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint != "http://ready-agent.klaus:8080" {
		t.Errorf("endpoint = %q, want %q", endpoint, "http://ready-agent.klaus:8080")
	}
}

func TestWaitForRunning_Timeout(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "slow-agent",
			Namespace: "klaus-system",
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStatePending,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := s.waitForRunning(ctx, "slow-agent")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want it to contain 'timed out'", err.Error())
	}
}

func TestWaitForRunning_ErrorState(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "error-agent",
			Namespace: "klaus-system",
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStateError,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.waitForRunning(ctx, "error-agent")
	if err == nil {
		t.Fatal("expected error for Error state instance")
	}
	if !strings.Contains(err.Error(), "Error state") {
		t.Errorf("error = %q, want it to contain 'Error state'", err.Error())
	}
}

func TestWaitForRunning_TransitionToRunning(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "transitioning-agent",
			Namespace: "klaus-system",
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStatePending,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).
		WithStatusSubresource(&klausv1alpha1.KlausInstance{}).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	var updated atomic.Bool
	go func() {
		time.Sleep(50 * time.Millisecond)
		var inst klausv1alpha1.KlausInstance
		if err := c.Get(context.Background(), types.NamespacedName{
			Name: "transitioning-agent", Namespace: "klaus-system",
		}, &inst); err != nil {
			return
		}
		inst.Status.State = klausv1alpha1.InstanceStateRunning
		inst.Status.Endpoint = "http://transitioning-agent.klaus:8080"
		_ = c.Status().Update(context.Background(), &inst)
		updated.Store(true)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	endpoint, err := s.waitForRunning(ctx, "transitioning-agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint != "http://transitioning-agent.klaus:8080" {
		t.Errorf("endpoint = %q, want %q", endpoint, "http://transitioning-agent.klaus:8080")
	}
	if !updated.Load() {
		t.Error("expected background goroutine to have run")
	}
}

func TestWaitForRunning_NotFound(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.waitForRunning(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing instance")
	}
	if !strings.Contains(err.Error(), "fetching instance") {
		t.Errorf("error = %q, want it to contain 'fetching instance'", err.Error())
	}
}
