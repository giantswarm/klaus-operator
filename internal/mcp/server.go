package mcp

import (
	"log/slog"

	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Server is the MCP server for the klaus-operator, exposing tools to
// create, list, delete, get, and restart KlausInstance resources.
type Server struct {
	client            client.Client
	operatorNamespace string
	httpServer        *server.StreamableHTTPServer
}

// NewServer creates a new MCP server backed by the given Kubernetes client.
func NewServer(c client.Client, operatorNamespace string) *Server {
	s := &Server{
		client:            c,
		operatorNamespace: operatorNamespace,
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

// Start starts the MCP server on the given address using streamable-http transport.
func (s *Server) Start(addr string) error {
	slog.Info("starting MCP server", "addr", addr)
	return s.httpServer.Start(addr)
}
