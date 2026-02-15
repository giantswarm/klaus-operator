package mcp

import (
	"context"
	"log/slog"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Server is the MCP server for the klaus-operator, exposing tools to
// create, list, delete, get, and restart KlausInstance resources.
// It implements manager.Runnable so it can be managed by controller-runtime.
type Server struct {
	client            client.Client
	operatorNamespace string
	addr              string
	httpServer        *server.StreamableHTTPServer
}

// NewServer creates a new MCP server backed by the given Kubernetes client.
func NewServer(c client.Client, operatorNamespace, addr string) *Server {
	s := &Server{
		client:            c,
		operatorNamespace: operatorNamespace,
		addr:              addr,
	}

	// Create the MCP server.
	mcpSrv := server.NewMCPServer(
		"klaus-operator",
		"0.1.0",
		server.WithToolCapabilities(true),
	)

	// Register tools.
	mcpSrv.AddTool(mcpgolang.NewTool(
		"create_instance",
		mcpgolang.WithDescription("Create a new Klaus agent instance for the calling user"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name for the new instance")),
		mcpgolang.WithString("model", mcpgolang.Description("Claude model to use (default: claude-sonnet-4-20250514)")),
		mcpgolang.WithString("system_prompt", mcpgolang.Description("System prompt for the agent")),
		mcpgolang.WithString("personality", mcpgolang.Description("Name of a KlausPersonality to use as template")),
	), s.handleCreateInstance)

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
		mcpgolang.WithDescription("Get details and status of a Klaus instance"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name of the instance")),
	), s.handleGetInstance)

	mcpSrv.AddTool(mcpgolang.NewTool(
		"restart_instance",
		mcpgolang.WithDescription("Restart a Klaus instance by cycling its Deployment"),
		mcpgolang.WithString("name", mcpgolang.Required(), mcpgolang.Description("Name of the instance to restart")),
	), s.handleRestartInstance)

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
