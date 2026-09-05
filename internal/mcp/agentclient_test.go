package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcpgolang "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// agentEndpoint is a stand-in for the MCP endpoint inside a klaus agent
// container. It counts session setups and tool calls so tests can tell a reused
// session from a rebuilt one, and can drop the next N connections mid-request
// to simulate an agent that restarted since the last call.
type agentEndpoint struct {
	inner http.Handler

	mu          sync.Mutex
	dropNext    int
	dropped     int
	setups      int
	toolCalls   int
	promptCalls int
}

// dropConnections makes the endpoint kill the next n requests at the TCP level.
func (e *agentEndpoint) dropConnections(n int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.dropNext = n
}

func (e *agentEndpoint) snapshot() (setups, toolCalls, dropped, promptCalls int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.setups, e.toolCalls, e.dropped, e.promptCalls
}

func (e *agentEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.mu.Lock()
	drop := e.dropNext > 0
	if drop {
		e.dropNext--
		e.dropped++
	}
	e.mu.Unlock()

	if drop {
		// Close the TCP connection without writing a response. mcp-go reports
		// this as a transport-level failure, which is what an agent pod that
		// went away looks like from the operator's side.
		e.killConnection(w)
		return
	}

	if r.Method == http.MethodPost {
		var probe struct {
			Method string `json:"method"`
		}
		if body, err := readAndRestoreBody(r); err == nil {
			_ = json.Unmarshal(body, &probe)
		}

		e.mu.Lock()
		switch probe.Method {
		case "initialize", string(mcpgolang.MethodServerDiscover):
			e.setups++
		case string(mcpgolang.MethodToolsCall):
			e.toolCalls++
		}
		e.mu.Unlock()
	}

	e.inner.ServeHTTP(w, r)
}

func (e *agentEndpoint) killConnection(w http.ResponseWriter) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
		return
	}

	conn, _, err := hijacker.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		// Reset rather than FIN, so the client always sees a failure instead
		// of a truncated but well-formed close.
		_ = tcpConn.SetLinger(0)
	}
	_ = conn.Close()
}

// readAndRestoreBody reads r.Body and puts an equivalent reader back, so the
// wrapped handler still sees the full request.
func readAndRestoreBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}

	body, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	return body, err
}

// newAgentEndpoint starts an MCP server exposing the "status" and "prompt"
// tools, mimicking the agent-side endpoint the operator talks to.
func newAgentEndpoint(t *testing.T) (*agentEndpoint, string) {
	t.Helper()

	endpoint := &agentEndpoint{}

	mcpServer := server.NewMCPServer("test-agent", "0.0.1", server.WithToolCapabilities(false))
	mcpServer.AddTool(
		mcpgolang.NewTool("status", mcpgolang.WithDescription("report agent status")),
		func(_ context.Context, _ mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
			return mcpgolang.NewToolResultText("running"), nil
		},
	)
	mcpServer.AddTool(
		mcpgolang.NewTool("prompt", mcpgolang.WithDescription("send a prompt to the agent")),
		func(_ context.Context, _ mcpgolang.CallToolRequest) (*mcpgolang.CallToolResult, error) {
			endpoint.mu.Lock()
			endpoint.promptCalls++
			endpoint.mu.Unlock()

			return mcpgolang.NewToolResultText("accepted"), nil
		},
	)

	endpoint.inner = server.NewStreamableHTTPServer(mcpServer, server.WithDisableLocalhostProtection(true))

	httpServer := httptest.NewServer(endpoint)
	t.Cleanup(httpServer.Close)

	return endpoint, httpServer.URL + "/mcp"
}

// newTestAgentMCPClient returns the production client with a primed session for
// "inst", plus the client that was cached for it.
func newTestAgentMCPClient(t *testing.T, baseURL string) (*agentMCPClient, *mcpclient.Client) {
	t.Helper()

	c, ok := NewAgentMCPClient().(*agentMCPClient)
	if !ok {
		t.Fatal("NewAgentMCPClient did not return *agentMCPClient")
	}
	t.Cleanup(c.Close)

	if _, err := c.Status(context.Background(), "inst", baseURL); err != nil {
		t.Fatalf("priming call: unexpected error: %v", err)
	}

	c.mu.Lock()
	cached, cachedOK := c.sessions["inst"]
	c.mu.Unlock()
	if !cachedOK {
		t.Fatal("session was not cached after a successful call")
	}

	return c, cached
}

func (c *agentMCPClient) cachedSession(instanceName string) (*mcpclient.Client, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	mc, ok := c.sessions[instanceName]

	return mc, ok
}

// TestAgentMCPClientReusesCachedSession pins the point of the cache: a second
// call must not re-initialize the session. mcp-go v1 no longer offers a liveness
// probe to run first, so the cached client is handed back as is.
func TestAgentMCPClientReusesCachedSession(t *testing.T) {
	endpoint, baseURL := newAgentEndpoint(t)
	c, first := newTestAgentMCPClient(t, baseURL)

	if _, err := c.Status(context.Background(), "inst", baseURL); err != nil {
		t.Fatalf("second Status: unexpected error: %v", err)
	}

	if second, _ := c.cachedSession("inst"); second != first {
		t.Error("cached session was replaced, want it reused")
	}

	setups, toolCalls, _, _ := endpoint.snapshot()
	if setups != 1 {
		t.Errorf("session setups = %d, want 1 (cached session should be reused)", setups)
	}
	if toolCalls != 2 {
		t.Errorf("tool calls = %d, want 2", toolCalls)
	}
}

// TestAgentMCPClientEvictsSessionOnTransportError covers the stale-client path
// the removed Ping probe used to guard: the failure of the call itself is now
// the liveness signal, so a transport error must evict the cached session.
func TestAgentMCPClientEvictsSessionOnTransportError(t *testing.T) {
	endpoint, baseURL := newAgentEndpoint(t)
	c, first := newTestAgentMCPClient(t, baseURL)

	// Every attempt fails, so even the retried Status call cannot recover.
	endpoint.dropConnections(10)
	if _, err := c.Status(context.Background(), "inst", baseURL); err == nil {
		t.Fatal("Status against a broken endpoint: got nil error, want a transport failure")
	}

	if _, stillCached := c.cachedSession("inst"); stillCached {
		t.Fatal("session survived a transport error, want it evicted")
	}

	endpoint.dropConnections(0)
	if _, err := c.Status(context.Background(), "inst", baseURL); err != nil {
		t.Fatalf("Status after recovery: unexpected error: %v", err)
	}

	second, _ := c.cachedSession("inst")
	if second == first {
		t.Error("cached session was not rebuilt after eviction")
	}
}

// TestAgentMCPClientRetriesIdempotentToolOnStaleSession checks that the
// transparent recovery the Ping probe used to provide survives for the
// read-only tools: an agent that restarted since the last call must not surface
// as an error to the caller.
func TestAgentMCPClientRetriesIdempotentToolOnStaleSession(t *testing.T) {
	endpoint, baseURL := newAgentEndpoint(t)
	c, first := newTestAgentMCPClient(t, baseURL)

	// Kill only the next request: the cached session is dead, the endpoint is
	// healthy.
	endpoint.dropConnections(1)
	if _, err := c.Status(context.Background(), "inst", baseURL); err != nil {
		t.Fatalf("Status over a stale session: unexpected error: %v", err)
	}

	second, ok := c.cachedSession("inst")
	if !ok {
		t.Fatal("no session cached after the retry")
	}
	if second == first {
		t.Error("stale session was reused, want it rebuilt")
	}

	setups, toolCalls, dropped, _ := endpoint.snapshot()
	if dropped != 1 {
		t.Errorf("dropped requests = %d, want 1", dropped)
	}
	if setups != 2 {
		t.Errorf("session setups = %d, want 2 (one per session)", setups)
	}
	if toolCalls != 2 {
		t.Errorf("tool calls = %d, want 2 (the priming call and the retry)", toolCalls)
	}
}

// TestAgentMCPClientDoesNotRetryPrompt is the safety counterpart: prompt is not
// idempotent, so a transport failure must never put a second copy of the
// message on the wire, even though the endpoint would happily accept it.
func TestAgentMCPClientDoesNotRetryPrompt(t *testing.T) {
	endpoint, baseURL := newAgentEndpoint(t)
	c, _ := newTestAgentMCPClient(t, baseURL)

	endpoint.dropConnections(1)
	if _, err := c.Prompt(context.Background(), "inst", baseURL, "hello"); err == nil {
		t.Fatal("Prompt over a stale session: got nil error, want the transport failure reported")
	}

	if _, stillCached := c.cachedSession("inst"); stillCached {
		t.Error("session survived a transport error, want it evicted")
	}

	_, _, dropped, promptCalls := endpoint.snapshot()
	if dropped != 1 {
		t.Errorf("dropped requests = %d, want 1", dropped)
	}
	if promptCalls != 0 {
		t.Errorf("prompt tool invoked %d times, want 0 (a retry would duplicate the message)", promptCalls)
	}
}

// TestAgentMCPClientKeepsSessionOnToolError guards the other half of the
// eviction predicate: a JSON-RPC error says nothing about the connection, so the
// session must survive it rather than being torn down and rebuilt.
func TestAgentMCPClientKeepsSessionOnToolError(t *testing.T) {
	endpoint, baseURL := newAgentEndpoint(t)
	c, first := newTestAgentMCPClient(t, baseURL)

	// "result" is not registered on this endpoint, so the server answers with a
	// JSON-RPC error rather than failing the transport.
	_, err := c.Result(context.Background(), "inst", baseURL, false)
	if err == nil {
		t.Fatal("Result for an unknown tool: got nil error, want a JSON-RPC error")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Result error = %v, want a tool-not-found error", err)
	}

	second, stillCached := c.cachedSession("inst")
	if !stillCached {
		t.Fatal("session was evicted by a JSON-RPC error, want it kept")
	}
	if second != first {
		t.Error("session was replaced by a JSON-RPC error, want the original kept")
	}

	if setups, _, _, _ := endpoint.snapshot(); setups != 1 {
		t.Errorf("session setups = %d, want 1 (no rebuild for a JSON-RPC error)", setups)
	}
}

// TestInvalidateSessionKeepsReplacedSession covers the identity check: a call
// that failed on a session another caller has already replaced must not evict
// the healthy replacement.
func TestInvalidateSessionKeepsReplacedSession(t *testing.T) {
	_, baseURL := newAgentEndpoint(t)
	c, first := newTestAgentMCPClient(t, baseURL)

	replacement, err := mcpclient.NewStreamableHttpClient(baseURL)
	if err != nil {
		t.Fatalf("creating replacement client: %v", err)
	}
	t.Cleanup(func() { _ = replacement.Close() })

	c.mu.Lock()
	c.sessions["inst"] = replacement
	c.mu.Unlock()

	// A late failure on the superseded client.
	c.invalidateSession("inst", first)

	current, ok := c.cachedSession("inst")
	if !ok {
		t.Fatal("replacement session was evicted, want it kept")
	}
	if current != replacement {
		t.Error("cached session is not the replacement")
	}
}

// TestIsTransportError checks the predicate that decides whether a failed call
// invalidates the cached session.
func TestIsTransportError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "transport failure",
			err:  fmt.Errorf("calling tool: %w", mcptransport.NewError(errors.New("connection reset"))),
			want: true,
		},
		{
			name: "terminated session",
			err:  mcptransport.NewError(fmt.Errorf("failed to send request: %w", mcptransport.ErrSessionTerminated)),
			want: true,
		},
		{
			name: "json-rpc error response",
			err:  errors.New("tool 'prompt' not found"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTransportError(tt.err); got != tt.want {
				t.Errorf("isTransportError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
