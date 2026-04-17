package mcp

import (
	"context"
	"fmt"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// handleStopInstance sets spec.stopped=true on a KlausInstance so the
// controller scales the Deployment to zero replicas.
func (s *Server) handleStopInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	instance, errResult := s.getOwnedInstance(ctx, request)
	if errResult != nil {
		return errResult, nil
	}

	// Already stopped -- return a clear message, not an error.
	if instance.Spec.Stopped {
		return mcpSuccess(map[string]any{
			"name":    instance.Name,
			"status":  "already_stopped",
			"message": fmt.Sprintf("Instance '%s' is already stopped", instance.Name),
		}), nil
	}

	// Patch spec.stopped = true using a merge patch.
	base := instance.DeepCopy()
	instance.Spec.Stopped = true
	if err := s.client.Patch(ctx, instance, client.MergeFrom(base)); err != nil {
		return mcpError("failed to stop instance: " + err.Error()), nil
	}

	return mcpSuccess(map[string]any{
		"name":    instance.Name,
		"status":  "stopping",
		"message": fmt.Sprintf("Instance '%s' is being stopped", instance.Name),
	}), nil
}

// handleStartInstance clears spec.stopped on a KlausInstance so the
// controller scales the Deployment back to 1 replica.
func (s *Server) handleStartInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	instance, errResult := s.getOwnedInstance(ctx, request)
	if errResult != nil {
		return errResult, nil
	}

	// Not stopped -- return a clear message, not an error.
	if !instance.Spec.Stopped {
		return mcpSuccess(map[string]any{
			"name":    instance.Name,
			"status":  "already_running",
			"message": fmt.Sprintf("Instance '%s' is not stopped (state: %s)", instance.Name, instance.Status.State),
		}), nil
	}

	// Patch spec.stopped = false using a merge patch.
	base := instance.DeepCopy()
	instance.Spec.Stopped = false
	if err := s.client.Patch(ctx, instance, client.MergeFrom(base)); err != nil {
		return mcpError("failed to start instance: " + err.Error()), nil
	}

	return mcpSuccess(map[string]any{
		"name":    instance.Name,
		"status":  "starting",
		"message": fmt.Sprintf("Instance '%s' is being started", instance.Name),
	}), nil
}
