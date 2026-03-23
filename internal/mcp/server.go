package mcp

import (
	"context"
	"io"
	"log/slog"

	klausoci "github.com/giantswarm/klaus-oci"
	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ArtifactLister discovers available OCI artifacts from a registry.
type ArtifactLister interface {
	ListPlugins(ctx context.Context, opts ...klausoci.ListOption) ([]klausoci.ListEntry, error)
	ListPersonalities(ctx context.Context, opts ...klausoci.ListOption) ([]klausoci.ListEntry, error)
	ListToolchains(ctx context.Context, opts ...klausoci.ListOption) ([]klausoci.ListEntry, error)
}

// PodLogReader reads logs from a pod container. The production implementation
// uses the Kubernetes clientset (corev1client), while tests can supply a mock.
type PodLogReader interface {
	GetLogs(ctx context.Context, namespace, podName string, opts *corev1.PodLogOptions) (io.ReadCloser, error)
}

// Server is the MCP server for the klaus-operator, exposing tools to
// create, list, delete, get, and restart KlausInstance resources, and to
// discover available OCI artifacts (plugins, personalities, toolchains).
// It implements manager.Runnable so it can be managed by controller-runtime.
type Server struct {
	client            client.Client
	operatorNamespace string
	addr              string
	ociClient         ArtifactLister
	podLogReader      PodLogReader
	agentClient       AgentMCPClient
	httpServer        *server.StreamableHTTPServer
}

// NewServer creates a new MCP server backed by the given Kubernetes client
// and OCI client for artifact discovery.
func NewServer(c client.Client, operatorNamespace, addr string, ociClient ArtifactLister, podLogReader PodLogReader, agentClient AgentMCPClient) *Server {
	s := &Server{
		client:            c,
		operatorNamespace: operatorNamespace,
		addr:              addr,
		ociClient:         ociClient,
		podLogReader:      podLogReader,
		agentClient:       agentClient,
	}

	// Create the MCP server.
	mcpSrv := server.NewMCPServer(
		"klaus-operator",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	// instanceSpecParams defines parameters shared by create_instance and run_instance.
	instanceSpecParams := []mcpgolang.ToolOption{
		mcpgolang.WithString("model", mcpgolang.Description("Claude model to use (default: claude-sonnet-4-20250514)")),
		mcpgolang.WithString("system_prompt", mcpgolang.Description("System prompt for the agent")),
		mcpgolang.WithString("personality", mcpgolang.Description("OCI reference to a personality artifact (e.g. registry/repo:tag)")),
		// High priority.
		mcpgolang.WithString("image", mcpgolang.Description("Toolchain container image override")),
		mcpgolang.WithArray("plugins", mcpgolang.Description("OCI plugin references (e.g. registry/repo:tag)"), mcpgolang.WithStringItems()),
		mcpgolang.WithString("workspace_git_repo", mcpgolang.Description("Git repository URL to clone into the workspace")),
		mcpgolang.WithString("workspace_git_ref", mcpgolang.Description("Git ref to checkout (branch, tag, or commit)")),
		mcpgolang.WithString("workspace_git_secret", mcpgolang.Description("Name of a Kubernetes Secret containing a git access token")),
		mcpgolang.WithString("workspace_storage_class", mcpgolang.Description("Kubernetes StorageClass for the workspace PVC")),
		mcpgolang.WithString("workspace_size", mcpgolang.Description("Workspace PVC size (e.g. 5Gi, 10Gi)")),
		mcpgolang.WithNumber("max_budget_usd", mcpgolang.Description("Maximum spend per session in USD")),
		mcpgolang.WithString("permission_mode", mcpgolang.Description("Tool permission mode: bypassPermissions (default) or default"), mcpgolang.Enum("bypassPermissions", "default")),
		mcpgolang.WithNumber("max_turns", mcpgolang.Description("Maximum number of agentic turns (0 = unlimited)")),
		mcpgolang.WithString("effort", mcpgolang.Description("Thinking effort level"), mcpgolang.Enum("low", "medium", "high")),
		// Medium priority.
		mcpgolang.WithArray("mcp_servers", mcpgolang.Description("KlausMCPServer resource names to attach"), mcpgolang.WithStringItems()),
		mcpgolang.WithString("append_system_prompt", mcpgolang.Description("Text appended to the default system prompt")),
		mcpgolang.WithArray("allowed_tools", mcpgolang.Description("Restrict which tools can be used"), mcpgolang.WithStringItems()),
		mcpgolang.WithArray("disallowed_tools", mcpgolang.Description("Prevent specific tools from being used"), mcpgolang.WithStringItems()),
		mcpgolang.WithString("fallback_model", mcpgolang.Description("Fallback model if the primary is unavailable")),
		mcpgolang.WithString("mode", mcpgolang.Description("Instance process mode: agent (default, autonomous coding) or chat (interactive conversation)"), mcpgolang.Enum("agent", "chat")),
	}

	// Register tools.
	createOpts := append([]mcpgolang.ToolOption{
		mcpgolang.WithDescription("Create a new Klaus agent instance for the calling user"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name for the new instance")),
	}, instanceSpecParams...)

	mcpSrv.AddTool(mcpgolang.NewTool("create_instance", createOpts...), s.handleCreateInstance)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"list_instances",
		mcpgolang.WithDescription("List the calling user's Klaus instances"),
	), s.handleListInstances)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"delete_instance",
		mcpgolang.WithDescription("Delete a Klaus instance (owner-only)"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name of the instance to delete")),
	), s.handleDeleteInstance)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"get_instance",
		mcpgolang.WithDescription("Get details and status of a Klaus instance. When the instance is running and the agent endpoint is reachable, includes agent-level status (agent_status, message_count, session_id)."),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name of the instance")),
	), s.handleGetInstance)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"restart_instance",
		mcpgolang.WithDescription("Restart a Klaus instance by cycling its Deployment"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name of the instance to restart")),
	), s.handleRestartInstance)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"stop_instance",
		mcpgolang.WithDescription("Stop a Klaus instance by scaling its Deployment to zero (owner-only). Config and state are preserved."),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name of the instance to stop")),
	), s.handleStopInstance)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"start_instance",
		mcpgolang.WithDescription("Start a previously stopped Klaus instance by scaling its Deployment back to one replica (owner-only)"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name of the instance to start")),
	), s.handleStartInstance)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"get_logs",
		mcpgolang.WithDescription("Get recent log output from a Klaus instance pod"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name of the instance")),
		mcpgolang.WithNumber("tail", mcpgolang.Description("Number of lines from end (default: 100)")),
		mcpgolang.WithString("container", mcpgolang.Description("Container name (default: klaus; use git-clone for init container logs)")),
	), s.handleGetLogs)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"prompt_instance",
		mcpgolang.WithDescription("Send a prompt to a running Klaus agent instance and optionally wait for the result"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Instance name")),
		mcpgolang.WithString("message", mcpgolang.Required(), mcpgolang.Description("Prompt message to send to the agent")),
		mcpgolang.WithBoolean("blocking", mcpgolang.Description("Wait for the agent to complete and return the result (default: false)")),
	), s.handlePromptInstance)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"get_result",
		mcpgolang.WithDescription("Retrieve the result from the last prompt sent to a Klaus agent instance"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Instance name")),
		mcpgolang.WithBoolean("full", mcpgolang.Description("Return full agent detail including tool_calls, model_usage, token_usage, cost, etc.")),
	), s.handleGetResult)

	runOpts := append([]mcpgolang.ToolOption{
		mcpgolang.WithDescription("Create a new Klaus agent instance, wait for it to become ready, and send a prompt -- a single operation combining create_instance + prompt_instance"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name for the new instance")),
		mcpgolang.WithString("message", mcpgolang.Required(), mcpgolang.Description("Prompt message to send to the agent once ready")),
	}, instanceSpecParams...)
	runOpts = append(runOpts,
		mcpgolang.WithBoolean("blocking", mcpgolang.Description("Wait for the agent to complete and return the result (default: false)")),
	)

	mcpSrv.AddTool(mcpgolang.NewTool("run_instance", runOpts...), s.handleRunInstance)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"list_plugins",
		mcpgolang.WithDescription("List available Klaus plugins from the OCI registry with version and metadata"),
	), s.handleListPlugins)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"list_personalities",
		mcpgolang.WithDescription("List available Klaus personalities from the OCI registry with version and metadata"),
	), s.handleListPersonalities)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"list_toolchains",
		mcpgolang.WithDescription("List available Klaus toolchain images from the OCI registry with version and metadata"),
	), s.handleListToolchains)

	s.httpServer = server.NewStreamableHTTPServer(mcpSrv,
		server.WithHTTPContextFunc(HTTPContextFuncAuth),
	)

	return s
}

// Start implements manager.Runnable. It starts the MCP server and shuts it
// down gracefully when the context is cancelled (i.e. when the manager stops).
func (s *Server) Start(ctx context.Context) error {
	slog.Info("starting MCP server", "addr", s.addr)

	// Start listening in a goroutine so we can wait on context cancellation.
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.Start(s.addr)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("shutting down MCP server")
		return s.httpServer.Shutdown(context.Background())
	}
}

// NeedLeaderElection implements manager.LeaderElectionRunnable to indicate the
// MCP server should run regardless of leader election status.
func (s *Server) NeedLeaderElection() bool {
	return false
}
