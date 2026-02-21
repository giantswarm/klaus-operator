package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	klausoci "github.com/giantswarm/klaus-oci"
	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

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
	instance, errResult := s.getOwnedInstance(ctx, request)
	if errResult != nil {
		return errResult, nil
	}

	if err := s.client.Delete(ctx, instance); err != nil {
		return mcpError("failed to delete instance: " + err.Error()), nil
	}

	return mcpSuccess(map[string]any{
		"name":    instance.Name,
		"status":  "deleting",
		"message": "Instance '" + instance.Name + "' is being deleted",
	}), nil
}

// handleGetInstance returns details about a KlausInstance.
func (s *Server) handleGetInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	instance, errResult := s.getOwnedInstance(ctx, request)
	if errResult != nil {
		return errResult, nil
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

	if instance.Status.Toolchain != "" {
		result["toolchain"] = instance.Status.Toolchain
	}

	if instance.Status.LastActivity != nil {
		result["lastActivity"] = instance.Status.LastActivity.Format(time.RFC3339)
	}

	return mcpSuccess(result), nil
}

// handleRestartInstance restarts a KlausInstance by cycling its Deployment.
func (s *Server) handleRestartInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	instance, errResult := s.getOwnedInstance(ctx, request)
	if errResult != nil {
		return errResult, nil
	}

	// Restart by patching the Deployment with a restart annotation.
	namespace := resources.UserNamespace(instance.Spec.Owner)
	var deployment appsv1.Deployment
	if err := s.client.Get(ctx, types.NamespacedName{
		Name:      instance.Name,
		Namespace: namespace,
	}, &deployment); err != nil {
		if apierrors.IsNotFound(err) {
			return mcpError("deployment for instance '" + instance.Name + "' not found (instance may still be starting)"), nil
		}
		return mcpError("failed to get deployment: " + err.Error()), nil
	}

	// Use a strategic merge patch to avoid conflicts with the controller's
	// concurrent reconciliation of the same Deployment object.
	patch := client.MergeFrom(deployment.DeepCopy())
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	if err := s.client.Patch(ctx, &deployment, patch); err != nil {
		return mcpError("failed to restart deployment: " + err.Error()), nil
	}

	return mcpSuccess(map[string]any{
		"name":    instance.Name,
		"status":  "restarting",
		"message": "Instance '" + instance.Name + "' is being restarted",
	}), nil
}

// getOwnedInstance extracts the user and instance name from a tool request,
// fetches the KlausInstance, and verifies ownership. Returns the instance on
// success, or an MCP error result on failure.
func (s *Server) getOwnedInstance(ctx context.Context, request mcpgolang.CallToolRequest) (*klausv1alpha1.KlausInstance, *mcpgolang.CallToolResult) {
	user, err := s.extractUser(ctx)
	if err != nil {
		return nil, mcpError("authentication required: " + err.Error())
	}

	args := request.GetArguments()
	name, _ := args["name"].(string)
	if name == "" {
		return nil, mcpError("name is required")
	}

	var instance klausv1alpha1.KlausInstance
	if err := s.client.Get(ctx, types.NamespacedName{
		Name:      name,
		Namespace: s.operatorNamespace,
	}, &instance); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, mcpError("instance '" + name + "' not found")
		}
		return nil, mcpError("failed to get instance: " + err.Error())
	}

	if instance.Spec.Owner != user {
		return nil, mcpError("access denied: you do not own instance '" + name + "'")
	}

	return &instance, nil
}

// extractUser extracts the user identity from the request context.
// The Authorization header is injected into context by HTTPContextFuncAuth via
// mcp-go's WithHTTPContextFunc. The token is a JWT forwarded by muster.
func (s *Server) extractUser(ctx context.Context) (string, error) {
	token := AuthTokenFromContext(ctx)
	if token == "" {
		return "", fmt.Errorf("no Authorization header in request")
	}
	return ExtractUserFromToken(token)
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

// handleListPlugins lists available Klaus plugins from the OCI registry.
func (s *Server) handleListPlugins(ctx context.Context, _ mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	return s.listArtifacts(ctx, klausoci.DefaultPluginRegistry, "plugins")
}

// handleListPersonalities lists available Klaus personalities from the OCI registry.
func (s *Server) handleListPersonalities(ctx context.Context, _ mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	return s.listArtifacts(ctx, klausoci.DefaultPersonalityRegistry, "personalities")
}

// handleListToolchains lists available Klaus toolchain images from the OCI registry.
func (s *Server) handleListToolchains(ctx context.Context, _ mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
	return s.listArtifacts(ctx, klausoci.DefaultToolchainRegistry, "toolchains")
}

func (s *Server) listArtifacts(ctx context.Context, registryBase, kind string) (*mcpgolang.CallToolResult, error) {
	if s.ociClient == nil {
		return mcpError("OCI client not configured"), nil
	}

	artifacts, err := s.ociClient.ListArtifacts(ctx, registryBase)
	if err != nil {
		return mcpError(fmt.Sprintf("failed to list %s: %s", kind, err.Error())), nil
	}

	items := make([]map[string]any, 0, len(artifacts))
	for _, a := range artifacts {
		item := map[string]any{
			"repository": a.Repository,
			"reference":  a.Reference,
		}
		if a.Name != "" {
			item["name"] = a.Name
		}
		if a.Version != "" {
			item["version"] = a.Version
		}
		if a.Type != "" {
			item["type"] = a.Type
		}
		items = append(items, item)
	}

	return mcpSuccess(map[string]any{
		"kind":  kind,
		"count": len(items),
		"items": items,
	}), nil
}

func mcpError(msg string) *mcpgolang.CallToolResult {
	return &mcpgolang.CallToolResult{
		Content: []mcpgolang.Content{
			mcpgolang.NewTextContent(msg),
		},
		IsError: true,
	}
}
