package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"

	klausv1alpha1 "github.com/giantswarm/klaus-operator/api/v1alpha1"
	"github.com/giantswarm/klaus-operator/internal/resources"
)

// handleCreateInstance creates a new KlausInstance for the calling user.
func (s *Server) handleCreateInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	user, err := s.extractUser(ctx)
	if err != nil {
		return mcpError("authentication required: " + err.Error()), nil
	}

	args := request.GetArguments()

	name, _ := args["name"].(string)
	if name == "" {
		return mcpError("name is required"), nil
	}

	model, _ := args["model"].(string)
	if model == "" {
		model = "claude-sonnet-4-20250514"
	}

	systemPrompt, _ := args["system_prompt"].(string)
	personality, _ := args["personality"].(string)

	instance := &klausv1alpha1.KlausInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: s.operatorNamespace,
		},
		Spec: klausv1alpha1.KlausInstanceSpec{
			Owner: user,
			Claude: klausv1alpha1.ClaudeConfig{
				Model:          model,
				PermissionMode: "bypassPermissions",
				SystemPrompt:   systemPrompt,
			},
		},
	}

	if personality != "" {
		instance.Spec.PersonalityRef = &klausv1alpha1.PersonalityReference{
			Name: personality,
		}
	}

	if err := s.client.Create(ctx, instance); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return mcpError("instance '" + name + "' already exists"), nil
		}
		return mcpError("failed to create instance: " + err.Error()), nil
	}

	return mcpSuccess(map[string]any{
		"name":      name,
		"owner":     user,
		"model":     model,
		"namespace": resources.UserNamespace(user),
		"status":    "creating",
	}), nil
}

// handleListInstances lists the calling user's instances.
func (s *Server) handleListInstances(ctx context.Context, _ mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	user, err := s.extractUser(ctx)
	if err != nil {
		return mcpError("authentication required: " + err.Error()), nil
	}

	var instanceList klausv1alpha1.KlausInstanceList
	if err := s.client.List(ctx, &instanceList, client.InNamespace(s.operatorNamespace)); err != nil {
		return mcpError("failed to list instances: " + err.Error()), nil
	}

	var userInstances []map[string]any
	for _, inst := range instanceList.Items {
		if inst.Spec.Owner != user {
			continue
		}
		userInstances = append(userInstances, map[string]any{
			"name":        inst.Name,
			"state":       string(inst.Status.State),
			"endpoint":    inst.Status.Endpoint,
			"mode":        inst.Status.Mode,
			"personality": inst.Status.Personality,
			"plugins":     inst.Status.PluginCount,
			"mcpServers":  inst.Status.MCPServerCount,
			"age":         time.Since(inst.CreationTimestamp.Time).Truncate(time.Second).String(),
		})
	}

	return mcpSuccess(map[string]any{
		"owner":     user,
		"count":     len(userInstances),
		"instances": userInstances,
	}), nil
}

// handleDeleteInstance deletes a KlausInstance (owner-only).
func (s *Server) handleDeleteInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	user, err := s.extractUser(ctx)
	if err != nil {
		return mcpError("authentication required: " + err.Error()), nil
	}

	args := request.GetArguments()
	name, _ := args["name"].(string)
	if name == "" {
		return mcpError("name is required"), nil
	}

	// Fetch instance and verify ownership.
	var instance klausv1alpha1.KlausInstance
	if err := s.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: s.operatorNamespace,
	}, &instance); err != nil {
		if apierrors.IsNotFound(err) {
			return mcpError("instance '" + name + "' not found"), nil
		}
		return mcpError("failed to get instance: " + err.Error()), nil
	}

	if instance.Spec.Owner != user {
		return mcpError("access denied: you do not own instance '" + name + "'"), nil
	}

	if err := s.client.Delete(ctx, &instance); err != nil {
		return mcpError("failed to delete instance: " + err.Error()), nil
	}

	return mcpSuccess(map[string]any{
		"name":    name,
		"status":  "deleting",
		"message": "Instance '" + name + "' is being deleted",
	}), nil
}

// handleGetInstance returns details about a KlausInstance.
func (s *Server) handleGetInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	user, err := s.extractUser(ctx)
	if err != nil {
		return mcpError("authentication required: " + err.Error()), nil
	}

	args := request.GetArguments()
	name, _ := args["name"].(string)
	if name == "" {
		return mcpError("name is required"), nil
	}

	var instance klausv1alpha1.KlausInstance
	if err := s.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: s.operatorNamespace,
	}, &instance); err != nil {
		if apierrors.IsNotFound(err) {
			return mcpError("instance '" + name + "' not found"), nil
		}
		return mcpError("failed to get instance: " + err.Error()), nil
	}

	if instance.Spec.Owner != user {
		return mcpError("access denied: you do not own instance '" + name + "'"), nil
	}

	result := map[string]any{
		"name":        instance.Name,
		"owner":       instance.Spec.Owner,
		"state":       string(instance.Status.State),
		"endpoint":    instance.Status.Endpoint,
		"mode":        instance.Status.Mode,
		"model":       instance.Spec.Claude.Model,
		"personality": instance.Status.Personality,
		"plugins":     instance.Status.PluginCount,
		"mcpServers":  instance.Status.MCPServerCount,
		"created":     instance.CreationTimestamp.Format(time.RFC3339),
		"namespace":   resources.UserNamespace(instance.Spec.Owner),
	}

	if instance.Status.LastActivity != nil {
		result["lastActivity"] = instance.Status.LastActivity.Format(time.RFC3339)
	}

	return mcpSuccess(result), nil
}

// handleRestartInstance restarts a KlausInstance by cycling its Deployment.
func (s *Server) handleRestartInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	user, err := s.extractUser(ctx)
	if err != nil {
		return mcpError("authentication required: " + err.Error()), nil
	}

	args := request.GetArguments()
	name, _ := args["name"].(string)
	if name == "" {
		return mcpError("name is required"), nil
	}

	// Fetch instance and verify ownership.
	var instance klausv1alpha1.KlausInstance
	if err := s.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: s.operatorNamespace,
	}, &instance); err != nil {
		if apierrors.IsNotFound(err) {
			return mcpError("instance '" + name + "' not found"), nil
		}
		return mcpError("failed to get instance: " + err.Error()), nil
	}

	if instance.Spec.Owner != user {
		return mcpError("access denied: you do not own instance '" + name + "'"), nil
	}

	// Restart by patching the Deployment with a restart annotation.
	namespace := resources.UserNamespace(instance.Spec.Owner)
	var deployment appsv1.Deployment
	if err := s.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}, &deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return mcpError("deployment for instance '" + name + "' not found (instance may still be starting)"), nil
		}
		return mcpError("failed to get deployment: " + err.Error()), nil
	}

	// Add/update restart annotation to trigger rollout.
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	if err := s.client.Update(ctx, &deployment); err != nil {
		return mcpError("failed to restart deployment: " + err.Error()), nil
	}

	return mcpSuccess(map[string]any{
		"name":    name,
		"status":  "restarting",
		"message": "Instance '" + name + "' is being restarted",
	}), nil
}

// extractUser extracts the user identity from the request context.
// In production, this comes from the forwarded JWT token via muster.
func (s *Server) extractUser(ctx context.Context) (string, error) {
	// Try to extract from context (set by middleware or transport).
	// For streamable-http with muster, the token is forwarded in the
	// Authorization header. We extract the user from the JWT payload.
	// TODO: Implement proper token extraction from context when mcp-go
	// supports request header access. For now, return a placeholder.
	return "", fmt.Errorf("user extraction not yet implemented -- use direct API")
}

func mcpSuccess(data any) *mcpgolang.CallToolResult {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return mcpError("failed to marshal response: " + err.Error())
	}
	return &mcpgolang.CallToolResult{
		Content: []mcpgolang.Content{
			mcpgolang.NewTextContent(string(jsonBytes)),
		},
	}
}

func mcpError(msg string) *mcpgolang.CallToolResult {
	return &mcpgolang.CallToolResult{
		Content: []mcpgolang.Content{
			mcpgolang.NewTextContent(msg),
		},
		IsError: true,
	}
}
