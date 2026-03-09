package mcp

import (
	"context"
	"fmt"
	"time"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
	"github.com/giantswarm/klaus-operator/internal/resources"
)

const (
	// readinessTimeout caps how long we wait for an instance to become Running.
	readinessTimeout = 5 * time.Minute
	// initialReadinessPoll is the initial polling interval for readiness checks.
	initialReadinessPoll = 2 * time.Second
	// maxReadinessPoll caps the exponential backoff for readiness polling.
	maxReadinessPoll = 10 * time.Second
)

// runResult is the JSON structure returned by handleRunInstance.
type runResult struct {
	Name      string `json:"name"`
	Owner     string `json:"owner"`
	Model     string `json:"model"`
	Namespace string `json:"namespace"`
	Status    string `json:"status"`
	SessionID string `json:"session_id,omitempty"`
	Result    string `json:"result,omitempty"`
}

// handleRunInstance creates a KlausInstance, waits for it to become Running,
// sends a prompt, and optionally waits for the agent to complete.
func (s *Server) handleRunInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	user, err := s.extractUser(ctx)
	if err != nil {
		return mcpError("authentication required: " + err.Error()), nil
	}

	args := request.GetArguments()

	name, _ := args["name"].(string)
	if name == "" {
		return mcpError("name is required"), nil
	}

	message, _ := args["message"].(string)
	if message == "" {
		return mcpError("message is required"), nil
	}
	if len(message) > maxMessageBytes {
		return mcpError("message exceeds maximum size (1 MiB)"), nil
	}

	model, _ := args["model"].(string)
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	systemPrompt, _ := args["system_prompt"].(string)
	personality, _ := args["personality"].(string)

	blocking := false
	if v, ok := args["blocking"].(bool); ok {
		blocking = v
	}

	// Stage 1: Create the KlausInstance CR.
	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.operatorNamespace,
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: user,
			Claude: klausv1alpha1.ClaudeConfig{
				Model:          model,
				PermissionMode: klausv1alpha1.PermissionModeBypass,
				SystemPrompt:   systemPrompt,
			},
		},
	}

	if personality != "" {
		instance.Spec.Personality = personality
	}

	if err := s.client.Create(ctx, instance); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return mcpError("instance '" + name + "' already exists"), nil
		}
		return mcpError("failed to create instance: " + err.Error()), nil
	}

	// Stage 2: Wait for the instance to become Running.
	readyCtx, readyCancel := context.WithTimeout(ctx, readinessTimeout)
	defer readyCancel()

	endpoint, err := s.waitForRunning(readyCtx, name)
	if err != nil {
		return mcpError(fmt.Sprintf("instance created but readiness check failed: %v", err)), nil
	}

	baseURL := endpoint + "/mcp"

	// Stage 3: Send the prompt.
	if s.agentClient == nil {
		return mcpError("instance created and running but agent MCP client not configured"), nil
	}

	toolResult, err := s.agentClient.Prompt(ctx, name, baseURL, message)
	if err != nil {
		return mcpError(fmt.Sprintf("instance running but prompt failed: %v", err)), nil
	}

	// Stage 4: Return immediately or wait for result.
	res := runResult{
		Name:      name,
		Owner:     user,
		Model:     model,
		Namespace: resources.UserNamespace(user),
		Status:    "started",
		SessionID: s.agentClient.SessionID(name),
		Result:    extractText(toolResult),
	}

	if !blocking {
		return mcpSuccess(res), nil
	}

	pollCtx, pollCancel := context.WithTimeout(ctx, maxBlockingWait)
	defer pollCancel()

	result, err := s.waitForResult(pollCtx, name, baseURL)
	if err != nil {
		return mcpError(fmt.Sprintf("prompt sent but waiting for result failed: %v", err)), nil
	}

	res.Status = "completed"
	res.Result = result
	return mcpSuccess(res), nil
}

// waitForRunning polls the KlausInstance status until it reaches Running,
// returning the endpoint URL. Returns an error on timeout or terminal failure.
func (s *Server) waitForRunning(ctx context.Context, name string) (string, error) {
	poll := initialReadinessPoll
	nn := types.NamespacedName{Name: name, Namespace: s.operatorNamespace}

	for {
		var instance klausv1alpha1.KlausInstance
		if err := s.client.Get(ctx, nn, &instance); err != nil {
			return "", fmt.Errorf("fetching instance: %w", err)
		}

		switch instance.Status.State {
		case klausv1alpha1.InstanceStateRunning:
			if instance.Status.Endpoint != "" {
				return instance.Status.Endpoint, nil
			}
			// Running but no endpoint yet; keep polling.
		case klausv1alpha1.InstanceStateError:
			return "", fmt.Errorf("instance %q entered Error state", name)
		}

		// Wait before the next poll attempt.
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", fmt.Errorf("timed out waiting for instance %q to become running", name)
		case <-timer.C:
		}

		if poll < maxReadinessPoll {
			poll = min(poll*2, maxReadinessPoll)
		}
	}
}
