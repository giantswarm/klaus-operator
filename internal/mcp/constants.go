package mcp

// Shared string constants used across MCP tool handlers. Extracted from
// repeated literals to satisfy goconst and to centralise the JSON keys and
// status values that form the MCP tool response contract.
const (
	// JSON keys returned in tool response payloads.
	keyName        = "name"
	keyStatus      = "status"
	keyMessage     = "message"
	keyOwner       = "owner"
	keyModel       = "model"
	keyMode        = "mode"
	keyPersonality = "personality"
	keyPlugins     = "plugins"

	// Status values returned in tool response payloads.
	statusStarted   = "started"
	statusCompleted = "completed"
	statusError     = "error"

	// klausContainerName is the canonical container name in instance pods.
	klausContainerName = "klaus"
)
