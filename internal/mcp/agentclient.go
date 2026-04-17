package mcp

import (
	"context"
	"fmt"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcpgolang "github.com/mark3labs/mcp-go/mcp"
)

// AgentMCPClient communicates with the MCP endpoint running inside a klaus
// agent container. The production implementation caches sessions per instance
// to avoid repeated initialization. Tests can supply a mock.
type AgentMCPClient interface {
	Prompt(ctx context.Context, instanceName, baseURL, message string) (*mcpgolang.CallToolResult, error)
	Status(ctx context.Context, instanceName, baseURL string) (*mcpgolang.CallToolResult, error)
	Result(ctx context.Context, instanceName, baseURL string, full bool) (*mcpgolang.CallToolResult, error)
	SessionID(instanceName string) string
	Close()
}

// agentMCPClient is the production AgentMCPClient backed by mcp-go's
// StreamableHttpClient with per-instance session caching.
type agentMCPClient struct {
	mu       sync.Mutex
	sessions map[string]*mcpclient.Client
}

// NewAgentMCPClient creates a new AgentMCPClient with session caching.
func NewAgentMCPClient() AgentMCPClient {
	return &agentMCPClient{
		sessions: make(map[string]*mcpclient.Client),
	}
}

func (c *agentMCPClient) getOrCreateSession(ctx context.Context, instanceName, baseURL string) (*mcpclient.Client, error) {
	c.mu.Lock()
	cached, ok := c.sessions[instanceName]
	c.mu.Unlock()

	if ok {
		if err := cached.Ping(ctx); err == nil {
			return cached, nil
		}
		c.mu.Lock()
		if cur, ok := c.sessions[instanceName]; ok && cur == cached {
			_ = cached.Close()
			delete(c.sessions, instanceName)
		}
		c.mu.Unlock()
	}

	mc, err := mcpclient.NewStreamableHttpClient(baseURL)
	if err != nil {
		return nil, fmt.Errorf("creating MCP client for %s: %w", baseURL, err)
	}

	if err := mc.Start(ctx); err != nil {
		return nil, fmt.Errorf("starting MCP transport for %s: %w", baseURL, err)
	}

	initReq := mcpgolang.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgolang.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgolang.Implementation{
		Name:    "klaus-operator",
		Version: "0.1.0",
	}
	if _, err := mc.Initialize(ctx, initReq); err != nil {
		_ = mc.Close()
		return nil, fmt.Errorf("initializing MCP session for %s: %w", baseURL, err)
	}

	c.mu.Lock()
	if existing, ok := c.sessions[instanceName]; ok {
		_ = mc.Close()
		c.mu.Unlock()
		return existing, nil
	}
	c.sessions[instanceName] = mc
	c.mu.Unlock()

	return mc, nil
}

func (c *agentMCPClient) callTool(ctx context.Context, instanceName, baseURL, toolName string, args map[string]any) (*mcpgolang.CallToolResult, error) {
	mc, err := c.getOrCreateSession(ctx, instanceName, baseURL)
	if err != nil {
		return nil, err
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args

	result, err := mc.CallTool(ctx, req)
	if err != nil {
		c.invalidateSession(instanceName)
		return nil, fmt.Errorf("calling tool %q on %s: %w", toolName, instanceName, err)
	}

	return result, nil
}

func (c *agentMCPClient) Prompt(ctx context.Context, instanceName, baseURL, message string) (*mcpgolang.CallToolResult, error) {
	return c.callTool(ctx, instanceName, baseURL, "prompt", map[string]any{
		"message": message,
	})
}

func (c *agentMCPClient) Status(ctx context.Context, instanceName, baseURL string) (*mcpgolang.CallToolResult, error) {
	return c.callTool(ctx, instanceName, baseURL, "status", nil)
}

func (c *agentMCPClient) Result(ctx context.Context, instanceName, baseURL string, full bool) (*mcpgolang.CallToolResult, error) {
	var args map[string]any
	if full {
		args = map[string]any{"full": true}
	}
	return c.callTool(ctx, instanceName, baseURL, "result", args)
}

func (c *agentMCPClient) SessionID(instanceName string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if mc, ok := c.sessions[instanceName]; ok {
		return mc.GetSessionId()
	}
	return ""
}

func (c *agentMCPClient) invalidateSession(instanceName string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if mc, ok := c.sessions[instanceName]; ok {
		_ = mc.Close()
		delete(c.sessions, instanceName)
	}
}

func (c *agentMCPClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, mc := range c.sessions {
		_ = mc.Close()
		delete(c.sessions, name)
	}
}
