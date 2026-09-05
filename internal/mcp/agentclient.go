package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcpgolang "github.com/mark3labs/mcp-go/mcp"
)

// Readability aliases for the callTool retry flag. Only tools that an agent can
// safely receive twice may be retried, because a transport error cannot tell us
// whether the agent already processed the request.
const (
	retryStaleSession   = true
	noRetryStaleSession = false
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

// getOrCreateSession returns the cached session for instanceName, creating and
// initializing one if there is none. The second return value reports whether
// the session came from the cache.
//
// A cached session is handed back without a liveness probe. Protocol version
// 2026-07-28 removed both the ping RPC and protocol-level sessions, so no round
// trip proves the agent is still reachable: on a modern connection mcp-go's
// Client.Ping returns nil without sending anything. The outcome of the tool
// call itself is the authoritative liveness signal, so callToolOnce evicts the
// entry when a call fails at the transport level.
func (c *agentMCPClient) getOrCreateSession(ctx context.Context, instanceName, baseURL string) (*mcpclient.Client, bool, error) {
	c.mu.Lock()
	cached, ok := c.sessions[instanceName]
	c.mu.Unlock()

	if ok {
		return cached, true, nil
	}

	mc, err := mcpclient.NewStreamableHttpClient(baseURL)
	if err != nil {
		return nil, false, fmt.Errorf("creating MCP client for %s: %w", baseURL, err)
	}

	if err := mc.Start(ctx); err != nil {
		return nil, false, fmt.Errorf("starting MCP transport for %s: %w", baseURL, err)
	}

	initReq := mcpgolang.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcpgolang.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcpgolang.Implementation{
		Name:    "klaus-operator",
		Version: "0.1.0",
	}
	if _, err := mc.Initialize(ctx, initReq); err != nil {
		_ = mc.Close()
		return nil, false, fmt.Errorf("initializing MCP session for %s: %w", baseURL, err)
	}

	c.mu.Lock()
	if existing, ok := c.sessions[instanceName]; ok {
		// A concurrent caller won the race, so its session is the cached one.
		// Reporting it as such is also what we want if it turns out to be dead
		// on arrival: an idempotent call is then retried on a rebuilt session.
		_ = mc.Close()
		c.mu.Unlock()
		return existing, true, nil
	}
	c.sessions[instanceName] = mc
	c.mu.Unlock()

	return mc, false, nil
}

// callTool invokes toolName on the instance's session.
//
// When retryOnStale is set, a transport failure on a session that came from the
// cache is retried once on a rebuilt session. That is the case the removed Ping
// probe used to absorb: the agent restarted since the last call, so the cached
// connection is dead but the endpoint itself is healthy. Retrying keeps the
// recovery transparent for tools that can safely be sent twice; for the others
// the failure is reported and the caller decides.
func (c *agentMCPClient) callTool(ctx context.Context, instanceName, baseURL, toolName string, args map[string]any, retryOnStale bool) (*mcpgolang.CallToolResult, error) {
	result, reused, err := c.callToolOnce(ctx, instanceName, baseURL, toolName, args)
	if err == nil || !retryOnStale || !reused || !isTransportError(err) {
		return result, err
	}

	// callToolOnce has already evicted the dead session, so this runs on a
	// freshly initialized one.
	result, _, err = c.callToolOnce(ctx, instanceName, baseURL, toolName, args)

	return result, err
}

// callToolOnce makes a single attempt, reporting whether the session it used
// came from the cache.
func (c *agentMCPClient) callToolOnce(ctx context.Context, instanceName, baseURL, toolName string, args map[string]any) (*mcpgolang.CallToolResult, bool, error) {
	mc, reused, err := c.getOrCreateSession(ctx, instanceName, baseURL)
	if err != nil {
		return nil, false, err
	}

	req := mcpgolang.CallToolRequest{}
	req.Params.Name = toolName
	req.Params.Arguments = args

	result, err := mc.CallTool(ctx, req)
	if err != nil {
		// Only a transport failure means the session is unusable. A JSON-RPC
		// error response says nothing about the connection, so tearing the
		// session down for one would churn sessions whenever a caller passes
		// bad arguments.
		if isTransportError(err) {
			c.invalidateSession(instanceName, mc)
		}
		return nil, reused, fmt.Errorf("calling tool %q on %s: %w", toolName, instanceName, err)
	}

	return result, reused, nil
}

// isTransportError reports whether err means the connection to the agent is
// unusable. mcp-go wraps every transport-level send failure -- a broken
// connection, and the 404 telling a legacy session that the server no longer
// knows it -- in *transport.Error; errors carried in a JSON-RPC error response
// are returned unwrapped.
func isTransportError(err error) bool {
	var transportErr *mcptransport.Error
	return errors.As(err, &transportErr)
}

// Prompt is never retried: sending a prompt twice would make the agent act on
// it twice, and a transport error cannot distinguish a request that was never
// delivered from one whose response was lost.
func (c *agentMCPClient) Prompt(ctx context.Context, instanceName, baseURL, message string) (*mcpgolang.CallToolResult, error) {
	return c.callTool(ctx, instanceName, baseURL, "prompt", map[string]any{
		keyMessage: message,
	}, noRetryStaleSession)
}

func (c *agentMCPClient) Status(ctx context.Context, instanceName, baseURL string) (*mcpgolang.CallToolResult, error) {
	return c.callTool(ctx, instanceName, baseURL, "status", nil, retryStaleSession)
}

func (c *agentMCPClient) Result(ctx context.Context, instanceName, baseURL string, full bool) (*mcpgolang.CallToolResult, error) {
	var args map[string]any
	if full {
		args = map[string]any{"full": true}
	}
	return c.callTool(ctx, instanceName, baseURL, "result", args, retryStaleSession)
}

func (c *agentMCPClient) SessionID(instanceName string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	if mc, ok := c.sessions[instanceName]; ok {
		return mc.GetSessionId()
	}
	return ""
}

// invalidateSession drops mc from the cache and closes it. A session another
// caller has already replaced is left alone, so a late failure on a dead client
// cannot evict its healthy successor.
func (c *agentMCPClient) invalidateSession(instanceName string, mc *mcpclient.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if cur, ok := c.sessions[instanceName]; ok && cur == mc {
		_ = cur.Close()
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
