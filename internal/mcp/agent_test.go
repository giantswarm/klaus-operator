package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// fakeAgentMCPClient implements AgentMCPClient for testing.
type fakeAgentMCPClient struct {
	promptResult *mcpgolang.CallToolResult
	promptErr    error
	resultResult *mcpgolang.CallToolResult
	resultErr    error
	statusResult *mcpgolang.CallToolResult
	statusErr    error
	sessionID    string

	// Captured call arguments for assertions.
	lastPromptMessage string
	lastResultFull    bool
	lastInstanceName  string
	lastBaseURL       string
}

func (f *fakeAgentMCPClient) Prompt(_ context.Context, instanceName, baseURL, message string) (*mcpgolang.CallToolResult, error) {
	f.lastInstanceName = instanceName
	f.lastBaseURL = baseURL
	f.lastPromptMessage = message
	return f.promptResult, f.promptErr
}

func (f *fakeAgentMCPClient) Status(_ context.Context, instanceName, baseURL string) (*mcpgolang.CallToolResult, error) {
	f.lastInstanceName = instanceName
	f.lastBaseURL = baseURL
	return f.statusResult, f.statusErr
}

func (f *fakeAgentMCPClient) Result(_ context.Context, instanceName, baseURL string, full bool) (*mcpgolang.CallToolResult, error) {
	f.lastInstanceName = instanceName
	f.lastBaseURL = baseURL
	f.lastResultFull = full
	return f.resultResult, f.resultErr
}

func (f *fakeAgentMCPClient) SessionID(_ string) string {
	return f.sessionID
}

func (f *fakeAgentMCPClient) Close() {}

// --- helpers ---

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := klausv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return scheme
}

func runningInstance(name, owner, endpoint string) *klausv1alpha1.KlausInstance {
	return &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: owner,
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State:    klausv1alpha1.InstanceStateRunning,
			Endpoint: endpoint,
		},
	}
}

func authCtx(email string) context.Context {
	return context.WithValue(context.Background(), authTokenKey,
		"Bearer "+buildTestJWT(fmt.Sprintf(`{"email":%q}`, email)))
}

func textResult(text string) *mcpgolang.CallToolResult {
	return &mcpgolang.CallToolResult{
		Content: []mcpgolang.Content{
			mcpgolang.NewTextContent(text),
		},
	}
}

func errorResult(text string) *mcpgolang.CallToolResult {
	return &mcpgolang.CallToolResult{
		Content: []mcpgolang.Content{
			mcpgolang.NewTextContent(text),
		},
		IsError: true,
	}
}

// --- prompt_instance tests ---

func TestHandlePromptInstance_NonBlocking(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	agent := &fakeAgentMCPClient{
		promptResult: textResult("prompt accepted"),
		sessionID:    "sess-123",
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "my-agent",
		"message": "hello agent",
	}

	result, err := s.handlePromptInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	var data promptResult
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if data.Instance != "my-agent" {
		t.Errorf("instance = %q, want %q", data.Instance, "my-agent")
	}
	if data.Status != "started" {
		t.Errorf("status = %q, want %q", data.Status, "started")
	}
	if data.SessionID != "sess-123" {
		t.Errorf("session_id = %q, want %q", data.SessionID, "sess-123")
	}
	if data.Result != "prompt accepted" {
		t.Errorf("result = %q, want %q", data.Result, "prompt accepted")
	}

	// Verify the agent client received correct arguments.
	if agent.lastPromptMessage != "hello agent" {
		t.Errorf("prompt message = %q, want %q", agent.lastPromptMessage, "hello agent")
	}
	if agent.lastBaseURL != "http://my-agent.klaus:8080/mcp" {
		t.Errorf("baseURL = %q, want %q", agent.lastBaseURL, "http://my-agent.klaus:8080/mcp")
	}
}

func TestHandlePromptInstance_AccessDenied(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "other@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       &fakeAgentMCPClient{},
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "my-agent",
		"message": "hello",
	}

	result, err := s.handlePromptInstance(authCtx("user@example.com"), req)
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

func TestHandlePromptInstance_NotRunning(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStatePending,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       &fakeAgentMCPClient{},
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "my-agent",
		"message": "hello",
	}

	result, err := s.handlePromptInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for non-running instance")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "not running") {
		t.Errorf("error message = %q, want it to contain 'not running'", text)
	}
}

func TestHandlePromptInstance_MissingMessage(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       &fakeAgentMCPClient{},
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "my-agent",
	}

	result, err := s.handlePromptInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for missing message")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "message is required") {
		t.Errorf("error message = %q, want it to contain 'message is required'", text)
	}
}

func TestHandlePromptInstance_PromptError(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	agent := &fakeAgentMCPClient{
		promptErr: fmt.Errorf("connection refused"),
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name":    "my-agent",
		"message": "hello",
	}

	result, err := s.handlePromptInstance(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for prompt failure")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "connection refused") {
		t.Errorf("error message = %q, want it to contain 'connection refused'", text)
	}
}

// --- get_result tests ---

func TestHandleGetResult_Summary(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	agentResponse := `{"status":"completed","message_count":5,"result_text":"Task done successfully"}`
	agent := &fakeAgentMCPClient{
		resultResult: textResult(agentResponse),
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "my-agent",
	}

	result, err := s.handleGetResult(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	var data agentResult
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if data.Instance != "my-agent" {
		t.Errorf("instance = %q, want %q", data.Instance, "my-agent")
	}
	if data.Status != "completed" { //nolint:goconst
		t.Errorf("status = %q, want %q", data.Status, "completed")
	}
	if data.MessageCount != 5 {
		t.Errorf("message_count = %d, want 5", data.MessageCount)
	}
	if data.Result != "Task done successfully" {
		t.Errorf("result = %q, want %q", data.Result, "Task done successfully")
	}

	if agent.lastResultFull {
		t.Error("expected full=false to be passed to agent client")
	}
}

func TestHandleGetResult_Full(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	rawJSON := `{"status":"completed","message_count":5,"result_text":"done","tool_calls":[],"cost":0.05}`
	agent := &fakeAgentMCPClient{
		resultResult: textResult(rawJSON),
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "my-agent",
		"full": true,
	}

	result, err := s.handleGetResult(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	// Full mode passes through raw JSON without wrapping.
	text := result.Content[0].(mcpgolang.TextContent).Text
	if text != rawJSON {
		t.Errorf("got %q, want raw passthrough %q", text, rawJSON)
	}

	if !agent.lastResultFull {
		t.Error("expected full=true to be passed to agent client")
	}
}

func TestHandleGetResult_AccessDenied(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "other@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       &fakeAgentMCPClient{},
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "my-agent",
	}

	result, err := s.handleGetResult(authCtx("user@example.com"), req)
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

func TestHandleGetResult_NotRunning(t *testing.T) {
	scheme := testScheme(t)
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-agent",
			Namespace: "klaus-system",
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: "user@example.com",
		},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStateStopped,
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       &fakeAgentMCPClient{},
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "my-agent",
	}

	result, err := s.handleGetResult(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected MCP error for non-running instance")
	}

	text := result.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "not running") {
		t.Errorf("error message = %q, want it to contain 'not running'", text)
	}
}

func TestHandleGetResult_AgentError(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	agent := &fakeAgentMCPClient{
		resultResult: errorResult("agent busy"),
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "my-agent",
	}

	result, err := s.handleGetResult(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The handler wraps agent errors into a structured response (not IsError).
	if result.IsError {
		t.Fatal("expected non-error MCP result wrapping the agent error")
	}

	var data agentResult
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if data.Status != "error" {
		t.Errorf("status = %q, want %q", data.Status, "error")
	}
	if data.Result != "agent busy" {
		t.Errorf("result = %q, want %q", data.Result, "agent busy")
	}
}

func TestHandleGetResult_FallbackParsing(t *testing.T) {
	scheme := testScheme(t)
	instance := runningInstance("my-agent", "user@example.com", "http://my-agent.klaus:8080")

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(instance).Build()

	// Non-standard JSON response that doesn't match agentToolResponse.
	agent := &fakeAgentMCPClient{
		resultResult: textResult("plain text result"),
	}

	s := &Server{
		client:            c,
		operatorNamespace: "klaus-system",
		agentClient:       agent,
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Arguments = map[string]any{
		"name": "my-agent",
	}

	result, err := s.handleGetResult(authCtx("user@example.com"), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected MCP error: %s", result.Content[0].(mcpgolang.TextContent).Text)
	}

	var data agentResult
	text := result.Content[0].(mcpgolang.TextContent).Text
	if err := json.Unmarshal([]byte(text), &data); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if data.Status != "completed" {
		t.Errorf("status = %q, want %q", data.Status, "completed")
	}
	if data.Result != "plain text result" {
		t.Errorf("result = %q, want %q", data.Result, "plain text result")
	}
}

// --- agentBaseURL tests ---

func TestAgentBaseURL_Running(t *testing.T) {
	s := &Server{}
	instance := runningInstance("test", "user@example.com", "http://test.klaus:8080")

	url, errResult := s.agentBaseURL(instance)
	if errResult != nil {
		t.Fatalf("unexpected error: %s", errResult.Content[0].(mcpgolang.TextContent).Text)
	}
	if url != "http://test.klaus:8080/mcp" {
		t.Errorf("url = %q, want %q", url, "http://test.klaus:8080/mcp")
	}
}

func TestAgentBaseURL_NotRunning(t *testing.T) {
	s := &Server{}
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Status: klausv1alpha1.KlausInstanceStatus{
			State:    klausv1alpha1.InstanceStatePending,
			Endpoint: "http://test.klaus:8080",
		},
	}

	_, errResult := s.agentBaseURL(instance)
	if errResult == nil {
		t.Fatal("expected error for non-running instance")
	}
	text := errResult.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "not running") {
		t.Errorf("error = %q, want it to contain 'not running'", text)
	}
}

func TestAgentBaseURL_NoEndpoint(t *testing.T) {
	s := &Server{}
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
		Status: klausv1alpha1.KlausInstanceStatus{
			State: klausv1alpha1.InstanceStateRunning,
		},
	}

	_, errResult := s.agentBaseURL(instance)
	if errResult == nil {
		t.Fatal("expected error for missing endpoint")
	}
	text := errResult.Content[0].(mcpgolang.TextContent).Text
	if !strings.Contains(text, "no endpoint") {
		t.Errorf("error = %q, want it to contain 'no endpoint'", text)
	}
}

// --- helper function tests ---

func TestExtractText(t *testing.T) {
	tests := []struct {
		name   string
		result *mcpgolang.CallToolResult
		want   string
	}{
		{
			name:   "nil result",
			result: nil,
			want:   "",
		},
		{
			name:   "single text",
			result: textResult("hello"),
			want:   "hello",
		},
		{
			name: "multiple text parts",
			result: &mcpgolang.CallToolResult{
				Content: []mcpgolang.Content{
					mcpgolang.NewTextContent("first"),
					mcpgolang.NewTextContent("second"),
				},
			},
			want: "first\nsecond",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractText(tt.result)
			if got != tt.want {
				t.Errorf("extractText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseStatusField(t *testing.T) {
	tests := []struct {
		name   string
		result *mcpgolang.CallToolResult
		want   string
	}{
		{
			name:   "json with status",
			result: textResult(`{"status":"completed","message_count":3}`),
			want:   "completed",
		},
		{
			name:   "plain text",
			result: textResult("running"),
			want:   "running",
		},
		{
			name:   "nil",
			result: nil,
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStatusField(tt.result)
			if got != tt.want {
				t.Errorf("parseStatusField() = %q, want %q", got, tt.want)
			}
		})
	}
}
