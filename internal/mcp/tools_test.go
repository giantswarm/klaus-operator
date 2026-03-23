package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// fakePodLogReader implements PodLogReader for testing, capturing the last
// call's arguments for assertion.
type fakePodLogReader struct {
	logs     string
	err      error
	lastOpts *corev1.PodLogOptions
}

func (f *fakePodLogReader) GetLogs(_ context.Context, _, _ string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	f.lastOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader(f.logs)), nil
}

func TestHandleGetInstance_AgentStatusEnrichment(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")
	instance.Spec.Claude.Model = "claude-sonnet-4-20250514"

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	agent := &fakeAgentMCPClient{
		statusResult: textResult(`{"status":"busy","message_count":7,"session_id":"sess-abc"}`),
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "my-agent"}

	result, err := s.handleGetInstance(authCtx("user@example.com"), req)
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

	// CRD-level fields should still be present.
	if data["state"] != "Running" {
		t.Errorf("state = %v, want %q", data["state"], "Running")
	}

	// Agent-level fields should be enriched.
	if data["agent_status"] != "busy" {
		t.Errorf("agent_status = %v, want %q", data["agent_status"], "busy")
	}
	if data["message_count"] != float64(7) {
		t.Errorf("message_count = %v, want 7", data["message_count"])
	}
	if data["session_id"] != "sess-abc" {
		t.Errorf("session_id = %v, want %q", data["session_id"], "sess-abc")
	}

	// Verify agent client received the right base URL.
	if agent.lastBaseURL != "http://my-agent.klaus:8080/mcp" {
		t.Errorf("baseURL = %q, want %q", agent.lastBaseURL, "http://my-agent.klaus:8080/mcp")
	}
}

func TestHandleGetInstance_AgentUnreachable(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")
	instance.Spec.Claude.Model = "claude-sonnet-4-20250514"

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	agent := &fakeAgentMCPClient{
		statusErr: fmt.Errorf("connection refused"),
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "my-agent"}

	result, err := s.handleGetInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should succeed with CRD-level data, not return an error.
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	var data map[string]any
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// CRD-level fields should be present.
	if data["state"] != "Running" {
		t.Errorf("state = %v, want %q", data["state"], "Running")
	}

	// Agent-level fields should be absent.
	if _, ok := data["agent_status"]; ok {
		t.Errorf("agent_status should be absent when agent is unreachable, got %v", data["agent_status"])
	}
	if _, ok := data["message_count"]; ok {
		t.Errorf("message_count should be absent when agent is unreachable, got %v", data["message_count"])
	}
	if _, ok := data["session_id"]; ok {
		t.Errorf("session_id should be absent when agent is unreachable, got %v", data["session_id"])
	}
}

func TestHandleGetInstance_NotRunningSkipsAgent(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "my-agent",
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
			State: klausv1alpha1.InstanceStatePending,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	agent := &fakeAgentMCPClient{
		statusResult: textResult(`{"status":"busy","message_count":3,"session_id":"sess-xyz"}`),
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "my-agent"}

	result, err := s.handleGetInstance(authCtx("user@example.com"), req)
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

	// Agent status should NOT have been called for non-running instance.
	if _, ok := data["agent_status"]; ok {
		t.Errorf("agent_status should be absent for non-running instance, got %v", data["agent_status"])
	}

	// The agent client should not have been called.
	if agent.lastInstanceName != "" {
		t.Errorf("agent client should not have been called, but lastInstanceName = %q", agent.lastInstanceName)
	}
}

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

func TestHandleGetLogs_Success(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := klausv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}

	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance-abc123",
			Namespace: "klaus-user-user-example-com",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "klaus",
				"app.kubernetes.io/instance": "test-instance",
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, pod).
		Build()

	logReader := &fakePodLogReader{
		logs: "line1\nline2\nline3\n",
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		podLogReader:      logReader,
	}

	ctx := context.WithValue(context.Background(), authTokenKey,
		"Bearer "+buildTestJWT(`{"email":"user@example.com"}`))

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "test-instance"}

	result, err := s.handleGetLogs(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if text != "line1\nline2\nline3\n" {
		t.Errorf("got logs %q, want %q", text, "line1\nline2\nline3\n")
	}

	// Verify defaults were applied.
	if logReader.lastOpts.Container != "klaus" {
		t.Errorf("container = %q, want %q", logReader.lastOpts.Container, "klaus")
	}
	if *logReader.lastOpts.TailLines != 100 {
		t.Errorf("tailLines = %d, want 100", *logReader.lastOpts.TailLines)
	}
}

func TestHandleGetLogs_CustomTailAndContainer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := klausv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}

	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance-xyz",
			Namespace: "klaus-user-user-example-com",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "klaus",
				"app.kubernetes.io/instance": "test-instance",
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, pod).
		Build()

	logReader := &fakePodLogReader{
		logs: "init log output",
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		podLogReader:      logReader,
	}

	ctx := context.WithValue(context.Background(), authTokenKey,
		"Bearer "+buildTestJWT(`{"email":"user@example.com"}`))

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":      "test-instance",
		"tail":      float64(50),
		"container": "git-clone",
	}

	result, err := s.handleGetLogs(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if text != "init log output" {
		t.Errorf("got logs %q, want %q", text, "init log output")
	}

	// Verify custom parameters were forwarded.
	if logReader.lastOpts.Container != "git-clone" {
		t.Errorf("container = %q, want %q", logReader.lastOpts.Container, "git-clone")
	}
	if *logReader.lastOpts.TailLines != 50 {
		t.Errorf("tailLines = %d, want 50", *logReader.lastOpts.TailLines)
	}
}

func TestHandleGetLogs_NoPods(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := klausv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}

	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance).
		Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		podLogReader:      &fakePodLogReader{},
	}

	ctx := context.WithValue(context.Background(), authTokenKey,
		"Bearer "+buildTestJWT(`{"email":"user@example.com"}`))

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "test-instance"}

	result, err := s.handleGetLogs(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for missing pods")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "no pods found") {
		t.Errorf("error message = %q, want it to contain 'no pods found'", text)
	}
}

func TestHandleGetLogs_LogReaderError(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := klausv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add corev1 scheme: %v", err)
	}

	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance-abc",
			Namespace: "klaus-user-user-example-com",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "klaus",
				"app.kubernetes.io/instance": "test-instance",
			},
		},
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(instance, pod).
		Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		podLogReader: &fakePodLogReader{
			err: fmt.Errorf("container not found"),
		},
	}

	ctx := context.WithValue(context.Background(), authTokenKey,
		"Bearer "+buildTestJWT(`{"email":"user@example.com"}`))

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "test-instance"}

	result, err := s.handleGetLogs(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for log reader failure")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "container not found") {
		t.Errorf("error message = %q, want it to contain 'container not found'", text)
	}
}

func TestHandleGetLogs_AccessDenied(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := klausv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}

	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-instance",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "other@example.com",
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

	result, err := s.handleGetLogs(ctx, req)
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

func TestHandleCreateInstance_MinimalArgs(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "basic-instance",
	}

	result, err := s.handleCreateInstance(authCtx("user@example.com"), req)
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

	if data["name"] != "basic-instance" {
		t.Errorf("name = %v, want %q", data["name"], "basic-instance")
	}
	if data["model"] != "claude-sonnet-4-20250514" {
		t.Errorf("model = %v, want default model", data["model"])
	}
	if data["status"] != "creating" {
		t.Errorf("status = %v, want %q", data["status"], "creating")
	}
}

func TestHandleCreateInstance_FullSpec(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":                    "full-instance",
		"model":                   "claude-opus-4-20250514",
		"system_prompt":           "You are a Go expert",
		"personality":             "gsoci.azurecr.io/giantswarm/personalities/go-dev:latest",
		"image":                   "gsoci.azurecr.io/giantswarm/klaus-go:1.25",
		"plugins":                 []any{"gsoci.azurecr.io/giantswarm/plugins/code-reviewer:v0.1.0"},
		"workspace_git_repo":      "https://github.com/org/repo.git",
		"workspace_git_ref":       "main",
		"workspace_git_secret":    "git-token",
		"workspace_storage_class": "premium-rwo",
		"workspace_size":          "10Gi",
		"max_budget_usd":          float64(5.0),
		"permission_mode":         "default",
		"max_turns":               float64(25),
		"effort":                  "high",
		"mcp_servers":             []any{"github-server"},
		"append_system_prompt":    "Always use tests",
		"allowed_tools":           []any{"Read", "Write"},
		"disallowed_tools":        []any{"WebSearch"},
		"fallback_model":          "claude-haiku-4-5-20251001",
		"mode":                    "chat",
	}

	result, err := s.handleCreateInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	// Verify the instance was created in the fake client.
	var instance klausv1alpha1.KlausInstance
	if err := c.Get(context.Background(), client.ObjectKey{
		Name: "full-instance", Namespace: "klaus-system",
	}, &instance); err != nil {
		t.Fatalf("failed to get created instance: %v", err)
	}

	spec := instance.Spec
	if spec.Claude.Model != "claude-opus-4-20250514" {
		t.Errorf("Model = %q", spec.Claude.Model)
	}
	if spec.Image != "gsoci.azurecr.io/giantswarm/klaus-go:1.25" {
		t.Errorf("Image = %q", spec.Image)
	}
	if len(spec.Plugins) != 1 {
		t.Fatalf("Plugins = %d, want 1", len(spec.Plugins))
	}
	if spec.Plugins[0].Tag != "v0.1.0" {
		t.Errorf("Plugin tag = %q", spec.Plugins[0].Tag)
	}
	if spec.Workspace == nil {
		t.Fatal("Workspace should not be nil")
	}
	if spec.Workspace.GitRepo != "https://github.com/org/repo.git" {
		t.Errorf("GitRepo = %q", spec.Workspace.GitRepo)
	}
	if spec.Workspace.GitSecretRef == nil || spec.Workspace.GitSecretRef.Name != "git-token" {
		t.Errorf("GitSecretRef = %+v", spec.Workspace.GitSecretRef)
	}
	if spec.Claude.PermissionMode != klausv1alpha1.PermissionModeDefault {
		t.Errorf("PermissionMode = %q", spec.Claude.PermissionMode)
	}
	if spec.Claude.MaxBudgetUSD == nil || *spec.Claude.MaxBudgetUSD != 5.0 {
		t.Errorf("MaxBudgetUSD = %v", spec.Claude.MaxBudgetUSD)
	}
	if spec.Claude.MaxTurns == nil || *spec.Claude.MaxTurns != 25 {
		t.Errorf("MaxTurns = %v", spec.Claude.MaxTurns)
	}
	if spec.Claude.Effort != klausv1alpha1.EffortHigh {
		t.Errorf("Effort = %q", spec.Claude.Effort)
	}
	if len(spec.MCPServers) != 1 || spec.MCPServers[0].Name != "github-server" {
		t.Errorf("MCPServers = %+v", spec.MCPServers)
	}
	if spec.Claude.AppendSystemPrompt != "Always use tests" {
		t.Errorf("AppendSystemPrompt = %q", spec.Claude.AppendSystemPrompt)
	}
	if spec.Claude.FallbackModel != "claude-haiku-4-5-20251001" {
		t.Errorf("FallbackModel = %q", spec.Claude.FallbackModel)
	}
	if spec.Claude.Mode == nil || *spec.Claude.Mode != "chat" {
		t.Errorf("Mode = %v, want %q", spec.Claude.Mode, "chat")
	}
}

func TestHandleCreateInstance_InvalidPlugin(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "bad-plugin",
		"plugins": []any{"no-tag-or-digest"},
	}

	result, err := s.handleCreateInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for invalid plugin reference")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "plugin reference") {
		t.Errorf("error = %q, want it to mention plugin reference", text)
	}
}
