// Package atif implements the Agent Tool Interaction Format (ATIF) for
// recording coding agent trajectories. Events are written as JSONL and
// consumed by evaluation frameworks like Harbor.
package atif

// ATIF schema version
const Version = 3

// Event types
const (
	TypeSession             = "session"
	TypeAgentStart          = "agent_start"
	TypeTurnStart           = "turn_start"
	TypeTurnEnd             = "turn_end"
	TypeMessageStart        = "message_start"
	TypeMessageEnd          = "message_end"
	TypeToolExecutionStart  = "tool_execution_start"
	TypeToolExecutionEnd    = "tool_execution_end"
	TypeToolExecutionUpdate = "tool_execution_update"
	TypeModelChange         = "model_change"
	TypeThinkingLevelChange = "thinking_level_change"
)

// SchemaContentBlock represents a content block within a message.
type SchemaContentBlock struct {
	Type              string `json:"type"`
	Text              string `json:"text,omitempty"`
	Thinking          string `json:"thinking,omitempty"`
	ThinkingSignature string `json:"thinkingSignature,omitempty"`
	ID                string `json:"id,omitempty"`
	Name              string `json:"name,omitempty"`
	Arguments         any    `json:"arguments,omitempty"`
	ToolCallID        string `json:"toolCallId,omitempty"`
	ToolName          string `json:"toolName,omitempty"`
	Content           any    `json:"content,omitempty"`
}

// SchemaUsage represents token usage breakdown.
type SchemaUsage struct {
	Input       int         `json:"input"`
	Output      int         `json:"output"`
	CacheRead   int         `json:"cacheRead,omitempty"`
	CacheWrite  int         `json:"cacheWrite,omitempty"`
	Reasoning   int         `json:"reasoning,omitempty"`
	TotalTokens int         `json:"totalTokens"`
	Cost        *SchemaCost `json:"cost,omitempty"`
}

// SchemaCost represents cost breakdown per component.
type SchemaCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead,omitempty"`
	CacheWrite float64 `json:"cacheWrite,omitempty"`
	Total      float64 `json:"total"`
}

// SchemaMessageData carries message content and metadata.
type SchemaMessageData struct {
	Role          string               `json:"role"`
	Content       []SchemaContentBlock `json:"content"`
	API           string               `json:"api,omitempty"`
	Provider      string               `json:"provider,omitempty"`
	Model         string               `json:"model,omitempty"`
	Usage         *SchemaUsage         `json:"usage,omitempty"`
	StopReason    string               `json:"stopReason,omitempty"`
	ErrorMessage  *string              `json:"errorMessage,omitempty"`
	ToolCallID    string               `json:"toolCallId,omitempty"`
	ToolName      string               `json:"toolName,omitempty"`
	IsError       bool                 `json:"isError,omitempty"`
	ResponseID    string               `json:"responseId,omitempty"`
	RawStopReason string               `json:"rawStopReason,omitempty"`
}

// ToolResultSchemaData carries tool execution results.
type ToolResultSchemaData struct {
	Role       string               `json:"role"`
	ToolCallID string               `json:"toolCallId"`
	ToolName   string               `json:"toolName"`
	Content    []SchemaContentBlock `json:"content"`
	IsError    bool                 `json:"isError"`
	Timestamp  int64                `json:"timestamp"`
}

// SessionEvent is the root event that opens a trajectory.
type SessionEvent struct {
	Type      string `json:"type"`
	Version   int    `json:"version"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Cwd       string `json:"cwd"`
}

// AgentStartEvent marks the start of agent execution.
type AgentStartEvent struct {
	Type string `json:"type"`
}

// TurnStartEvent marks the beginning of a turn.
type TurnStartEvent struct {
	Type string `json:"type"`
}

// MessageStartEvent is sent when a message begins.
type MessageStartEvent struct {
	Type      string            `json:"type"`
	Message   SchemaMessageData `json:"message"`
	Timestamp int64             `json:"timestamp"`
}

// MessageEndEvent is sent when a message completes.
type MessageEndEvent struct {
	Type      string            `json:"type"`
	Message   SchemaMessageData `json:"message"`
	Timestamp int64             `json:"timestamp"`
}

// ToolExecutionStartEvent marks when a tool call begins executing.
type ToolExecutionStartEvent struct {
	Type       string `json:"type"`
	ToolCallID string `json:"toolCallId"`
	ToolName   string `json:"toolName"`
	Args       any    `json:"args"`
}

// ToolExecutionUpdateEvent is sent when a tool produces partial output.
type ToolExecutionUpdateEvent struct {
	Type          string            `json:"type"`
	ToolCallID    string            `json:"toolCallId"`
	ToolName      string            `json:"toolName"`
	Args          any               `json:"args,omitempty"`
	PartialResult PartialResultData `json:"partialResult"`
}

// PartialResultData carries partial tool output.
type PartialResultData struct {
	Content []SchemaContentBlock `json:"content"`
	Details any                  `json:"details,omitempty"`
}

// ToolExecutionEndEvent marks the completion of a tool call.
type ToolExecutionEndEvent struct {
	Type       string     `json:"type"`
	ToolCallID string     `json:"toolCallId"`
	ToolName   string     `json:"toolName"`
	Result     ResultData `json:"result"`
	IsError    bool       `json:"isError"`
}

// ResultData carries final tool output.
type ResultData struct {
	Content []SchemaContentBlock `json:"content"`
}

// TurnEndEvent marks the end of a turn with all tool results.
type TurnEndEvent struct {
	Type        string                 `json:"type"`
	Message     SchemaMessageData      `json:"message"`
	ToolResults []ToolResultSchemaData `json:"toolResults"`
}

// ModelChangeEvent records a change in model or provider.
type ModelChangeEvent struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	ParentID  string `json:"parentId,omitempty"`
	Timestamp string `json:"timestamp"`
	Provider  string `json:"provider"`
	ModelID   string `json:"modelId"`
}

// ThinkingLevelChangeEvent records a change in thinking level.
type ThinkingLevelChangeEvent struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	ParentID      string `json:"parentId,omitempty"`
	Timestamp     string `json:"timestamp"`
	ThinkingLevel string `json:"thinkingLevel"`
}
