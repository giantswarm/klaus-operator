package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
)

// promptResult is the JSON structure returned by handlePromptInstance.
type promptResult struct {
	Instance  string `json:"instance"`
	Status    string `json:"status"`
	SessionID string `json:"session_id,omitempty"`
	Result    string `json:"result,omitempty"`
}

const (
	// maxMessageBytes caps the prompt message size to prevent memory exhaustion.
	maxMessageBytes = 1 << 20 // 1 MiB
	// maxBlockingWait caps how long a blocking prompt will wait for a result.
	maxBlockingWait = 10 * time.Minute
)

// handlePromptInstance sends a prompt to a running agent instance and
// optionally waits for the result.
func (s *Server) handlePromptInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	instance, errResult := s.getOwnedInstance(ctx, request)
	if errResult != nil {
		return errResult, nil
	}

	args := request.GetArguments()

	message, _ := args["message"].(string)
	if message == "" {
		return mcpError("message is required"), nil
	}
	if len(message) > maxMessageBytes {
		return mcpError("message exceeds maximum size (1 MiB)"), nil
	}

	blocking := false
	if v, ok := args["blocking"].(bool); ok {
		blocking = v
	}

	baseURL, errResult := s.agentBaseURL(instance)
	if errResult != nil {
		return errResult, nil
	}

	if s.agentClient == nil {
		return mcpError("agent MCP client not configured"), nil
	}

	toolResult, err := s.agentClient.Prompt(ctx, instance.Name, baseURL, message)
	if err != nil {
		return mcpError(fmt.Sprintf("sending prompt to %q: %v", instance.Name, err)), nil
	}

	if !blocking {
		return mcpSuccess(promptResult{
			Instance:  instance.Name,
			Status:    "started",
			SessionID: s.agentClient.SessionID(instance.Name),
			Result:    extractText(toolResult),
		}), nil
	}

	pollCtx, cancel := context.WithTimeout(ctx, maxBlockingWait)
	defer cancel()

	result, err := s.waitForResult(pollCtx, instance.Name, baseURL)
	if err != nil {
		return mcpError(fmt.Sprintf("waiting for result from %q: %v", instance.Name, err)), nil
	}

	return mcpSuccess(promptResult{
		Instance:  instance.Name,
		Status:    "completed",
		SessionID: s.agentClient.SessionID(instance.Name),
		Result:    result,
	}), nil
}

// agentResult is the JSON structure returned by handleGetResult.
type agentResult struct {
	Instance     string `json:"instance"`
	Status       string `json:"status"`
	MessageCount int    `json:"message_count"`
	Result       string `json:"result,omitempty"`
}

// agentToolResponse represents the JSON payload returned by the agent's
// result MCP tool inside the container.
type agentToolResponse struct {
	Status       string `json:"status"`
	MessageCount int    `json:"message_count"`
	ResultText   string `json:"result_text"`
}

// handleGetResult retrieves the result from the last prompt sent to an agent.
func (s *Server) handleGetResult(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	instance, errResult := s.getOwnedInstance(ctx, request)
	if errResult != nil {
		return errResult, nil
	}

	args := request.GetArguments()
	full := false
	if v, ok := args["full"].(bool); ok {
		full = v
	}

	baseURL, errResult := s.agentBaseURL(instance)
	if errResult != nil {
		return errResult, nil
	}

	if s.agentClient == nil {
		return mcpError("agent MCP client not configured"), nil
	}

	toolResult, err := s.agentClient.Result(ctx, instance.Name, baseURL, full)
	if err != nil {
		return mcpError(fmt.Sprintf("fetching result from %q: %v", instance.Name, err)), nil
	}

	if toolResult.IsError {
		return mcpSuccess(agentResult{
			Instance: instance.Name,
			Status:   "error",
			Result:   extractText(toolResult),
		}), nil
	}

	// When full is requested, pass the raw agent JSON through without
	// re-parsing into the reduced agentResult struct.
	if full {
		text := extractText(toolResult)
		return &mcpgolang.CallToolResult{
			Content: []mcpgolang.Content{
				mcpgolang.NewTextContent(text),
			},
		}, nil
	}

	text := extractText(toolResult)

	var parsed agentToolResponse
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed.Status != "" {
		return mcpSuccess(agentResult{
			Instance:     instance.Name,
			Status:       parsed.Status,
			MessageCount: parsed.MessageCount,
			Result:       parsed.ResultText,
		}), nil
	}

	// Fallback: response is not the expected JSON structure.
	return mcpSuccess(agentResult{
		Instance: instance.Name,
		Status:   "completed",
		Result:   text,
	}), nil
}

// agentBaseURL constructs the MCP endpoint URL for a running instance.
// Returns an MCP error result if the instance is not running.
func (s *Server) agentBaseURL(instance *klausv1alpha1.KlausInstance) (string, *mcpgolang.CallToolResult) {
	if instance.Status.State != klausv1alpha1.InstanceStateRunning {
		return "", mcpError(fmt.Sprintf("instance %q is not running (state: %s)", instance.Name, instance.Status.State))
	}
	if instance.Status.Endpoint == "" {
		return "", mcpError(fmt.Sprintf("instance %q has no endpoint yet", instance.Name))
	}
	return instance.Status.Endpoint + "/mcp", nil
}

// terminalStatuses are the agent statuses that indicate the task is done.
var terminalStatuses = map[string]bool{
	"completed": true,
	"error":     true,
	"failed":    true,
}

// waitForResult polls the agent's status tool until the task completes or the
// context is cancelled, then retrieves the result.
func (s *Server) waitForResult(ctx context.Context, name, baseURL string) (string, error) {
	poll := 2 * time.Second
	const maxPoll = 30 * time.Second

	for {
		timer := time.NewTimer(poll)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.C:
		}

		statusResult, err := s.agentClient.Status(ctx, name, baseURL)
		if err != nil {
			return "", fmt.Errorf("polling status: %w", err)
		}

		if terminalStatuses[parseStatusField(statusResult)] {
			break
		}

		if poll < maxPoll {
			poll = min(poll*2, maxPoll)
		}
	}

	resultResp, err := s.agentClient.Result(ctx, name, baseURL, false)
	if err != nil {
		return "", fmt.Errorf("fetching result: %w", err)
	}

	return extractText(resultResp), nil
}

// extractText returns the concatenated text content from an MCP tool result.
func extractText(result *mcpgolang.CallToolResult) string {
	if result == nil {
		return ""
	}

	var parts []string
	for _, c := range result.Content {
		if tc, ok := c.(mcpgolang.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}

	raw, err := json.Marshal(result.Content)
	if err != nil {
		return ""
	}
	return string(raw)
}

// parseStatusField extracts the "status" field from a JSON tool result.
func parseStatusField(result *mcpgolang.CallToolResult) string {
	text := extractText(result)
	if text == "" {
		return ""
	}
	var parsed struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err == nil && parsed.Status != "" {
		return parsed.Status
	}
	return text
}
