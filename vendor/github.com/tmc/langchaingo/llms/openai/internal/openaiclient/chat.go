package openaiclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// toolCallPattern matches tool call text that some providers embed in content/reasoning streams.
// Format: functions.NAME:INDEX{JSON} (e.g., "functions.read_file:0{"path":"..."}")
// This pattern can appear when providers serialize tool calls as text rather than structured data.
var toolCallPattern = regexp.MustCompile(`functions\.([a-zA-Z_][a-zA-Z0-9_]*):(\d+)(\{[^{}]*(?:\{[^{}]*\}[^{}]*)*\})`)

// parseToolCallsFromText extracts tool calls from text and returns the cleaned text plus parsed tool calls.
// Some providers embed tool calls as text in content/reasoning streams rather than using
// the proper tool_calls structure. This function parses them into proper ToolCall structs.
func parseToolCallsFromText(text string) (cleanText string, toolCalls []ToolCall) {
	matches := toolCallPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text, nil
	}

	slog.Debug("Found tool call in thoughts")
	// Extract tool calls from matches
	for _, match := range matches {
		if len(match) >= 8 {
			name := text[match[2]:match[3]]
			indexStr := text[match[4]:match[5]]
			args := text[match[6]:match[7]]

			var index int
			fmt.Sscanf(indexStr, "%d", &index)

			toolCalls = append(toolCalls, ToolCall{
				Type:  ToolTypeFunction,
				Index: index,
				Function: ToolFunction{
					Name:      name,
					Arguments: args,
				},
			})
		}
	}

	// Return text before the first match (the clean reasoning/content text)
	if len(matches) > 0 {
		cleanText = text[:matches[0][0]]
	} else {
		cleanText = text
	}

	return cleanText, toolCalls
}

const (
	defaultChatModel = "gpt-3.5-turbo"
)

var ErrContentExclusive = errors.New("only one of Content / MultiContent allowed in message")

type StreamOptions struct {
	// If set, an additional chunk will be streamed before the data: [DONE] message.
	// The usage field on this chunk shows the token usage statistics for the entire request,
	// and the choices field will always be an empty array.
	// All other chunks will also include a usage field, but with a null value.
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// ChatRequest is a request to complete a chat completion..
type ChatRequest struct {
	Model       string         `json:"model"`
	Messages    []*ChatMessage `json:"messages"`
	Temperature float64        `json:"temperature"`
	TopP        float64        `json:"top_p,omitempty"`
	// Deprecated: Use MaxCompletionTokens
	// Note: Some OpenAI-compatible servers still require this field
	MaxTokens           int      `json:"max_tokens,omitempty"`
	MaxCompletionTokens int      `json:"max_completion_tokens,omitempty"`
	N                   int      `json:"n,omitempty"`
	StopWords           []string `json:"stop,omitempty"`
	Stream              bool     `json:"stream,omitempty"`
	FrequencyPenalty    float64  `json:"frequency_penalty,omitempty"`
	PresencePenalty     float64  `json:"presence_penalty,omitempty"`
	Seed                int      `json:"seed,omitempty"`

	// ResponseFormat is the format of the response.
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`

	// LogProbs indicates whether to return log probabilities of the output tokens or not.
	// If true, returns the log probabilities of each output token returned in the content of message.
	// This option is currently not available on the gpt-4-vision-preview model.
	LogProbs bool `json:"logprobs,omitempty"`
	// TopLogProbs is an integer between 0 and 5 specifying the number of most likely tokens to return at each
	// token position, each with an associated log probability.
	// logprobs must be set to true if this parameter is used.
	TopLogProbs int `json:"top_logprobs,omitempty"`

	Tools []Tool `json:"tools,omitempty"`
	// This can be either a string or a ToolChoice object.
	// If it is a string, it should be one of 'none', or 'auto', otherwise it should be a ToolChoice object specifying a specific tool to use.
	ToolChoice any `json:"tool_choice,omitempty"`

	// Options for streaming response. Only set this when you set stream: true.
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`

	// ReasoningEffort controls thinking effort for reasoning models (o1, o3, GPT-5).
	// Valid values: "minimal" (GPT-5 only), "low", "medium", "high"
	ReasoningEffort string `json:"reasoning_effort,omitempty"`

	// StreamingFunc is a function to be called for each chunk of a streaming response.
	// Return an error to stop streaming early.
	StreamingFunc func(ctx context.Context, chunk []byte) error `json:"-"`

	// StreamingReasoningFunc is a function to be called for each reasoning and content chunk of a streaming response.
	// Return an error to stop streaming early.
	StreamingReasoningFunc func(ctx context.Context, reasoningChunk, chunk []byte) error `json:"-"`

	// Deprecated: use Tools instead.
	Functions []FunctionDefinition `json:"functions,omitempty"`
	// Deprecated: use ToolChoice instead.
	FunctionCallBehavior FunctionCallBehavior `json:"function_call,omitempty"`

	// Metadata allows you to specify additional information that will be passed to the model.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MarshalJSON ensures that only one of MaxTokens or MaxCompletionTokens is sent.
// OpenAI's API returns an error if both fields are present.
// Also omits temperature for reasoning models (GPT-5, o1, o3) that only accept default temperature.
func (r ChatRequest) MarshalJSON() ([]byte, error) {
	type Alias ChatRequest
	aux := struct {
		*Alias
		MaxTokens           *int     `json:"max_tokens,omitempty"`
		MaxCompletionTokens *int     `json:"max_completion_tokens,omitempty"`
		Temperature         *float64 `json:"temperature,omitempty"`
	}{
		Alias: (*Alias)(&r),
	}

	// Handle temperature for reasoning models
	if isReasoningModel(r.Model) {
		// Reasoning models (GPT-5, o1, o3) only accept temperature=1 (default)
		// Omit temperature field to let API use its default value
		aux.Temperature = nil
	} else {
		// For regular models, always send temperature
		aux.Temperature = &r.Temperature
	}

	// Ensure only one token field is sent
	if r.MaxCompletionTokens > 0 && r.MaxTokens > 0 {
		// Both are set - this shouldn't happen with our logic,
		// but if it does, prefer MaxCompletionTokens (modern field)
		aux.MaxCompletionTokens = &r.MaxCompletionTokens
		aux.MaxTokens = nil
	} else if r.MaxCompletionTokens > 0 {
		aux.MaxCompletionTokens = &r.MaxCompletionTokens
		aux.MaxTokens = nil
	} else if r.MaxTokens > 0 {
		aux.MaxTokens = &r.MaxTokens
		aux.MaxCompletionTokens = nil
	}

	return json.Marshal(&aux)
}

// isReasoningModel returns true if the model is a reasoning model that has temperature constraints.
// Reasoning models (GPT-5, o1, o3) only accept temperature=1 and reject other values.
func isReasoningModel(model string) bool {
	// o1 series: o1-preview, o1-mini
	if strings.HasPrefix(model, "o1-") {
		return true
	}
	// o3 series: o3, o3-mini (note: "o3" without suffix is also valid)
	if model == "o3" || strings.HasPrefix(model, "o3-") {
		return true
	}
	// GPT-5 series (when released)
	if strings.HasPrefix(model, "gpt-5") {
		return true
	}
	return false
}

// ToolType is the type of a tool.
type ToolType string

const (
	ToolTypeFunction ToolType = "function"
)

// Tool is a tool to use in a chat request.
type Tool struct {
	Type     ToolType           `json:"type"`
	Function FunctionDefinition `json:"function,omitempty"`
}

// ToolChoice is a choice of a tool to use.
type ToolChoice struct {
	Type     ToolType     `json:"type"`
	Function ToolFunction `json:"function,omitempty"`
}

// ToolFunction is a function to be called in a tool choice.
type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolCall is a call to a tool.
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     ToolType     `json:"type"`
	Function ToolFunction `json:"function,omitempty"`
	Index    int          `json:"index"`
}

type ResponseFormatJSONSchemaProperty struct {
	Type                 string                                       `json:"type"`
	Description          string                                       `json:"description,omitempty"`
	Enum                 []interface{}                                `json:"enum,omitempty"`
	Items                *ResponseFormatJSONSchemaProperty            `json:"items,omitempty"`
	Properties           map[string]*ResponseFormatJSONSchemaProperty `json:"properties,omitempty"`
	AdditionalProperties bool                                         `json:"additionalProperties"`
	Required             []string                                     `json:"required,omitempty"`
	Ref                  string                                       `json:"$ref,omitempty"`
}

type ResponseFormatJSONSchema struct {
	Name   string                            `json:"name"`
	Strict bool                              `json:"strict"`
	Schema *ResponseFormatJSONSchemaProperty `json:"schema"`
}

// ResponseFormat is the format of the response.
type ResponseFormat struct {
	Type       string                    `json:"type"`
	JSONSchema *ResponseFormatJSONSchema `json:"json_schema,omitempty"`
}

// ChatMessage is a message in a chat request.
type ChatMessage struct { //nolint:musttag
	// The role of the author of this message. One of system, user, assistant, function, or tool.
	Role string

	// The content of the message.
	// This field is mutually exclusive with MultiContent.
	Content string

	// MultiContent is a list of content parts to use in the message.
	MultiContent []llms.ContentPart

	// The name of the author of this message. May contain a-z, A-Z, 0-9, and underscores,
	// with a maximum length of 64 characters.
	Name string

	// ToolCalls is a list of tools that were called in the message.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// FunctionCall represents a function call that was made in the message.
	// Deprecated: use ToolCalls instead.
	FunctionCall *FunctionCall

	// ToolCallID is the ID of the tool call this message is for.
	// Only present in tool messages.
	ToolCallID string `json:"tool_call_id,omitempty"`

	// This field is used with reasoning models and represents the reasoning contents of the assistant message before the final answer.
	// DeepSeek uses "reasoning_content" while OpenAI (and OpenRouter) use "reasoning"
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// For OpenAI/OpenRouter format, which uses "reasoning" instead of "reasoning_content"
	Reasoning string `json:"reasoning,omitempty"`
}

func (m ChatMessage) MarshalJSON() ([]byte, error) {
	if m.Content != "" && m.MultiContent != nil {
		return nil, ErrContentExclusive
	}
	if text, ok := isSingleTextContent(m.MultiContent); ok {
		m.Content = text
		m.MultiContent = nil
	}
	if len(m.MultiContent) > 0 {
		msg := struct {
			Role         string             `json:"role"`
			Content      string             `json:"-"`
			MultiContent []llms.ContentPart `json:"content,omitempty"`
			Name         string             `json:"name,omitempty"`
			ToolCalls    []ToolCall         `json:"tool_calls,omitempty"`

			// Deprecated: use ToolCalls instead.
			FunctionCall *FunctionCall `json:"function_call,omitempty"`

			// ToolCallID is the ID of the tool call this message is for.
			// Only present in tool messages.
			ToolCallID string `json:"tool_call_id,omitempty"`

			// This field is used with reasoning models and represents the reasoning contents of the assistant message before the final answer.
			// DeepSeek uses "reasoning_content" while OpenAI (and OpenRouter) use "reasoning"
			ReasoningContent string `json:"reasoning_content,omitempty"`
			// For OpenAI/OpenRouter format, which uses "reasoning" instead of "reasoning_content"
			Reasoning string `json:"reasoning,omitempty"`
		}(m)
		return json.Marshal(msg)
	}
	msg := struct {
		Role         string             `json:"role"`
		Content      string             `json:"content"`
		MultiContent []llms.ContentPart `json:"-"`
		Name         string             `json:"name,omitempty"`
		ToolCalls    []ToolCall         `json:"tool_calls,omitempty"`
		// Deprecated: use ToolCalls instead.
		FunctionCall *FunctionCall `json:"function_call,omitempty"`

		// ToolCallID is the ID of the tool call this message is for.
		// Only present in tool messages.
		ToolCallID string `json:"tool_call_id,omitempty"`

		// This field is used with reasoning models and represents the reasoning contents of the assistant message before the final answer.
		ReasoningContent string `json:"reasoning_content,omitempty"`
		// For OpenAI/OpenRouter format, which uses "reasoning" instead of "reasoning_content"
		Reasoning string `json:"reasoning,omitempty"`
	}(m)
	return json.Marshal(msg)
}

func isSingleTextContent(parts []llms.ContentPart) (string, bool) {
	if len(parts) != 1 {
		return "", false
	}
	tc, isText := parts[0].(llms.TextContent)
	return tc.Text, isText
}

func (m *ChatMessage) UnmarshalJSON(data []byte) error {
	// Use an anonymous struct with proper json tags to match the MarshalJSON format
	msg := struct {
		Role         string             `json:"role"`
		Content      string             `json:"content"`
		MultiContent []llms.ContentPart `json:"-"` // not expected in response
		Name         string             `json:"name,omitempty"`
		ToolCalls    []ToolCall         `json:"tool_calls,omitempty"`
		// Deprecated: use ToolCalls instead.
		FunctionCall *FunctionCall `json:"function_call,omitempty"`

		// ToolCallID is the ID of the tool call this message is for.
		// Only present in tool messages.
		ToolCallID string `json:"tool_call_id,omitempty"`

		// This field is used with reasoning models and represents the reasoning contents of the assistant message before the final answer.
		// DeepSeek uses "reasoning_content" while OpenAI (and OpenRouter) use "reasoning"
		ReasoningContent string `json:"reasoning_content,omitempty"`
		// For OpenAI/OpenRouter format, which uses "reasoning" instead of "reasoning_content"
		Reasoning string `json:"reasoning,omitempty"`
	}{}
	err := json.Unmarshal(data, &msg)
	if err != nil {
		return err
	}

	*m = ChatMessage{
		Role:             msg.Role,
		Content:          msg.Content,
		MultiContent:     msg.MultiContent,
		Name:             msg.Name,
		ToolCalls:        msg.ToolCalls,
		FunctionCall:     msg.FunctionCall,
		ToolCallID:       msg.ToolCallID,
		ReasoningContent: msg.ReasoningContent,
		Reasoning:        msg.Reasoning,
	}

	// If ReasoningContent is empty but Reasoning is set (OpenAI/OpenRouter format),
	// copy Reasoning to ReasoningContent for consistent access
	if m.ReasoningContent == "" && m.Reasoning != "" {
		m.ReasoningContent = m.Reasoning
	}

	return nil
}

type TopLogProbs struct {
	Token   string  `json:"token"`
	LogProb float64 `json:"logprob"`
	Bytes   []byte  `json:"bytes,omitempty"`
}

// LogProb represents the probability information for a token.
type LogProb struct {
	Token   string  `json:"token"`
	LogProb float64 `json:"logprob"`
	Bytes   []byte  `json:"bytes,omitempty"` // Omitting the field if it is null
	// TopLogProbs is a list of the most likely tokens and their log probability, at this token position.
	// In rare cases, there may be fewer than the number of requested top_logprobs returned.
	TopLogProbs []TopLogProbs `json:"top_logprobs"`
}

// LogProbs is the top-level structure containing the log probability information.
type LogProbs struct {
	// Content is a list of message content tokens with log probability information.
	Content []LogProb `json:"content"`
}

type FinishReason string

const (
	FinishReasonStop          FinishReason = "stop"
	FinishReasonLength        FinishReason = "length"
	FinishReasonFunctionCall  FinishReason = "function_call"
	FinishReasonToolCalls     FinishReason = "tool_calls"
	FinishReasonContentFilter FinishReason = "content_filter"
	FinishReasonNull          FinishReason = "null"
)

func (r FinishReason) MarshalJSON() ([]byte, error) {
	if r == FinishReasonNull || r == "" {
		return []byte("null"), nil
	}
	return []byte(`"` + string(r) + `"`), nil // best effort to not break future API changes
}

// ChatCompletionChoice is a choice in a chat response.
type ChatCompletionChoice struct {
	Index        int          `json:"index"`
	Message      ChatMessage  `json:"message"`
	FinishReason FinishReason `json:"finish_reason"`
	LogProbs     *LogProbs    `json:"logprobs,omitempty"`
}

// ChatUsage is the usage of a chat completion request.
type ChatUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
		AudioTokens  int `json:"audio_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens          int `json:"reasoning_tokens"`
		AudioTokens              int `json:"audio_tokens"`
		AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
		RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
	} `json:"completion_tokens_details"`
}

// ChatCompletionResponse is a response to a chat request.
type ChatCompletionResponse struct {
	ID                string                  `json:"id,omitempty"`
	Created           int64                   `json:"created,omitempty"`
	Choices           []*ChatCompletionChoice `json:"choices,omitempty"`
	Model             string                  `json:"model,omitempty"`
	Object            string                  `json:"object,omitempty"`
	Usage             ChatUsage               `json:"usage,omitempty"`
	SystemFingerprint string                  `json:"system_fingerprint"`
}

type Usage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	TotalTokens         int `json:"total_tokens"`
	PromptTokensDetails struct {
		CachedTokens int `json:"cached_tokens"`
		AudioTokens  int `json:"audio_tokens"`
	} `json:"prompt_tokens_details"`
	CompletionTokensDetails struct {
		ReasoningTokens          int `json:"reasoning_tokens"`
		AudioTokens              int `json:"audio_tokens"`
		AcceptedPredictionTokens int `json:"accepted_prediction_tokens"`
		RejectedPredictionTokens int `json:"rejected_prediction_tokens"`
	} `json:"completion_tokens_details"`
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// StreamedChatResponsePayload is a chunk from the stream.
type StreamedChatResponsePayload struct {
	ID      string  `json:"id,omitempty"`
	Created float64 `json:"created,omitempty"`
	Model   string  `json:"model,omitempty"`
	Object  string  `json:"object,omitempty"`
	Choices []struct {
		Index float64 `json:"index,omitempty"`
		Delta struct {
			Role         string        `json:"role,omitempty"`
			Content      string        `json:"content,omitempty"`
			FunctionCall *FunctionCall `json:"function_call,omitempty"`
			// ToolCalls is a list of tools that were called in the message.
			ToolCalls []*ToolCall `json:"tool_calls,omitempty"`
			// This field is used with reasoning models and represents the reasoning contents of the assistant message before the final answer.
			// DeepSeek uses "reasoning_content" while OpenAI (and OpenRouter) use "reasoning"
			ReasoningContent string `json:"reasoning_content,omitempty"`
			// For OpenAI/OpenRouter format, which uses "reasoning" instead of "reasoning_content"
			Reasoning string `json:"reasoning,omitempty"`
		} `json:"delta,omitempty"`
		FinishReason FinishReason `json:"finish_reason,omitempty"`
	} `json:"choices,omitempty"`
	SystemFingerprint string `json:"system_fingerprint"`
	// An optional field that will only be present when you set stream_options: {"include_usage": true} in your request.
	// When present, it contains a null value except for the last chunk which contains the token usage statistics
	// for the entire request.
	Usage *Usage `json:"usage,omitempty"`
	Error error  `json:"-"` // use for error handling only
}

// FunctionDefinition is a definition of a function that can be called by the model.
type FunctionDefinition struct {
	// Name is the name of the function.
	Name string `json:"name"`
	// Description is a description of the function.
	Description string `json:"description,omitempty"`
	// Parameters is a list of parameters for the function.
	Parameters any `json:"parameters"`
	// Strict is a flag to enable structured output mode.
	Strict bool `json:"strict,omitempty"`
}

// FunctionCallBehavior is the behavior to use when calling functions.
type FunctionCallBehavior string

const (
	// FunctionCallBehaviorUnspecified is the empty string.
	FunctionCallBehaviorUnspecified FunctionCallBehavior = ""
	// FunctionCallBehaviorNone will not call any functions.
	FunctionCallBehaviorNone FunctionCallBehavior = "none"
	// FunctionCallBehaviorAuto will call functions automatically.
	FunctionCallBehaviorAuto FunctionCallBehavior = "auto"
)

// FunctionCall is a call to a function.
type FunctionCall struct {
	// Name is the name of the function to call.
	Name string `json:"name"`
	// Arguments is the set of arguments to pass to the function.
	Arguments string `json:"arguments"`
}

func (c *Client) createChat(ctx context.Context, payload *ChatRequest) (*ChatCompletionResponse, error) {
	if payload.StreamingFunc != nil || payload.StreamingReasoningFunc != nil {
		payload.Stream = true
		if payload.StreamOptions == nil {
			payload.StreamOptions = &StreamOptions{IncludeUsage: true}
		}
	}
	// Build request payload

	// Filter out internal metadata that shouldn't be sent to the API
	originalMetadata := payload.Metadata
	if payload.Metadata != nil {
		filteredMetadata := make(map[string]any)
		for k, v := range payload.Metadata {
			// Skip internal openai: prefixed metadata fields
			if !strings.HasPrefix(k, "openai:") {
				filteredMetadata[k] = v
			}
		}
		if len(filteredMetadata) > 0 {
			payload.Metadata = filteredMetadata
		} else {
			payload.Metadata = nil
		}
	}

	payloadBytes, err := json.Marshal(payload)

	// Restore original metadata
	payload.Metadata = originalMetadata
	if err != nil {
		return nil, err
	}

	// Build request
	body := bytes.NewReader(payloadBytes)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.buildURL("/chat/completions", payload.Model), body)
	if err != nil {
		return nil, err
	}

	c.setHeaders(req)

	// Send request
	r, err := c.httpClient.Do(req)
	if err != nil {
		return nil, sanitizeHTTPError(err)
	}
	defer r.Body.Close()

	if r.StatusCode != http.StatusOK {
		msg := fmt.Sprintf("API returned unexpected status code: %d", r.StatusCode)

		// No need to check the error here: if it fails, we'll just return the
		// status code.
		var errResp errorMessage
		if err := json.NewDecoder(r.Body).Decode(&errResp); err != nil {
			return nil, errors.New(msg)
		}

		return nil, fmt.Errorf("%s: %s", msg, errResp.Error.Message)
	}
	if payload.StreamingFunc != nil || payload.StreamingReasoningFunc != nil {
		return parseStreamingChatResponse(ctx, r, payload)
	}
	// Parse response
	var response ChatCompletionResponse
	return &response, json.NewDecoder(r.Body).Decode(&response)
}

func parseStreamingChatResponse(ctx context.Context, r *http.Response, payload *ChatRequest) (*ChatCompletionResponse,
	error,
) { //nolint:cyclop,lll
	scanner := bufio.NewScanner(r.Body)
	responseChan := make(chan StreamedChatResponsePayload)

	// Create a context that can be cancelled to stop the goroutine
	readerCtx, cancelReader := context.WithCancel(ctx)
	defer cancelReader()

	go func() {
		defer close(responseChan)
		for scanner.Scan() {
			// Check if context is cancelled
			select {
			case <-readerCtx.Done():
				return
			default:
			}

			line := scanner.Text()
			if line == "" {
				continue
			}

			// Skip SSE comment lines (any line starting with ':')
			// According to SSE spec: https://www.w3.org/TR/eventsource/
			// "Lines that start with a U+003A COLON character (:) are comments"
			if strings.HasPrefix(line, ":") {
				continue
			}

			// Only process lines that start with "data:"
			if !strings.HasPrefix(line, "data:") {
				// Skip any other non-data lines (like event:, id:, retry:, etc.)
				continue
			}

			data := strings.TrimPrefix(line, "data:") // here use `data:` instead of `data: ` for compatibility
			data = strings.TrimSpace(data)
			if data == "[DONE]" {
				return
			}

			// Debug log raw SSE data - this shows exactly what the API returns
			slog.Debug("SSE raw", "data", data)

			var streamPayload StreamedChatResponsePayload
			err := json.NewDecoder(bytes.NewReader([]byte(data))).Decode(&streamPayload)
			if err != nil {
				// Skip non-JSON data values that some providers might send
				// This could happen if the data field contains non-JSON content
				continue
			}

			// Non-blocking send with context check
			select {
			case <-readerCtx.Done():
				return
			case responseChan <- streamPayload:
			}
		}
		if err := scanner.Err(); err != nil {
			select {
			case <-readerCtx.Done():
				return
			case responseChan <- StreamedChatResponsePayload{Error: fmt.Errorf("error reading streaming response: %w", err)}:
			}
			return
		}
	}()
	// Combine response
	return combineStreamingChatResponse(readerCtx, payload, responseChan)
}

func combineStreamingChatResponse(
	ctx context.Context,
	payload *ChatRequest,
	responseChan chan StreamedChatResponsePayload,
) (*ChatCompletionResponse, error) {
	response := ChatCompletionResponse{
		Choices: []*ChatCompletionChoice{
			{},
		},
	}

	for streamResponse := range responseChan {
		if streamResponse.Error != nil {
			return nil, streamResponse.Error
		}

		if streamResponse.Usage != nil {
			response.Usage.CompletionTokens = streamResponse.Usage.CompletionTokens
			response.Usage.PromptTokens = streamResponse.Usage.PromptTokens
			response.Usage.TotalTokens = streamResponse.Usage.TotalTokens
			response.Usage.PromptTokensDetails.AudioTokens = streamResponse.Usage.PromptTokensDetails.AudioTokens
			response.Usage.PromptTokensDetails.CachedTokens = streamResponse.Usage.PromptTokensDetails.CachedTokens
			response.Usage.CompletionTokensDetails.AudioTokens = streamResponse.Usage.CompletionTokensDetails.AudioTokens
			response.Usage.CompletionTokensDetails.AcceptedPredictionTokens = streamResponse.Usage.CompletionTokensDetails.AcceptedPredictionTokens
			response.Usage.CompletionTokensDetails.RejectedPredictionTokens = streamResponse.Usage.CompletionTokensDetails.RejectedPredictionTokens
			response.Usage.CompletionTokensDetails.ReasoningTokens = streamResponse.Usage.CompletionTokensDetails.ReasoningTokens
		}

		if len(streamResponse.Choices) == 0 {
			continue
		}
		choice := streamResponse.Choices[0]

		// Debug: log what's in each field of the parsed delta
		slog.Debug("Parsed delta",
			"content", choice.Delta.Content,
			"reasoning", choice.Delta.Reasoning,
			"reasoning_content", choice.Delta.ReasoningContent,
			"tool_calls_count", len(choice.Delta.ToolCalls),
		)

		// Parse and extract tool calls from content before streaming
		// Some providers embed tool calls as text (e.g., "functions.read_file:0{...}")
		contentText, contentToolCalls := parseToolCallsFromText(choice.Delta.Content)
		contentChunk := []byte(contentText)
		response.Choices[0].Message.Content += contentText // Only keep clean content
		response.Choices[0].FinishReason = choice.FinishReason

		// Add any tool calls parsed from content to the response
		if len(contentToolCalls) > 0 {
			for _, tc := range contentToolCalls {
				response.Choices[0].Message.ToolCalls = updateToolCalls(
					response.Choices[0].Message.ToolCalls, []*ToolCall{&tc})
			}
		}

		// Handle both reasoning formats: DeepSeek uses "reasoning_content", OpenAI/Claude uses "reasoning"
		// Accumulate from both fields to ensure compatibility with all providers
		reasoningDelta := ""
		if choice.Delta.ReasoningContent != "" {
			reasoningDelta += choice.Delta.ReasoningContent
		}
		if choice.Delta.Reasoning != "" {
			reasoningDelta += choice.Delta.Reasoning
		}

		// Parse and extract tool calls from reasoning before streaming to user
		reasoningForStream, reasoningToolCalls := parseToolCallsFromText(reasoningDelta)
		reasoningChunk := []byte(reasoningForStream)
		response.Choices[0].Message.ReasoningContent += reasoningForStream // Only keep clean reasoning

		// Add any tool calls parsed from reasoning to the response
		if len(reasoningToolCalls) > 0 {
			for _, tc := range reasoningToolCalls {
				response.Choices[0].Message.ToolCalls = updateToolCalls(
					response.Choices[0].Message.ToolCalls, []*ToolCall{&tc})
			}
		}

		// Update function calls and tool calls, but don't pass them to StreamingFunc
		// Tool calls are handled separately through the response.Choices[0].Message.ToolCalls field
		if choice.Delta.FunctionCall != nil {
			updateFunctionCall(response.Choices[0].Message, choice.Delta.FunctionCall)
		}

		// Track if this delta contains tool calls - if so, don't stream content
		// as it may contain tool call data that shouldn't be displayed
		hasToolCalls := len(choice.Delta.ToolCalls) > 0

		if hasToolCalls {
			response.Choices[0].Message.ToolCalls = updateToolCalls(response.Choices[0].Message.ToolCalls,
				choice.Delta.ToolCalls)
			// Clear content chunk to prevent tool call data from being streamed
			// Some providers include tool call info in both tool_calls and content fields
			contentChunk = []byte{}
		}

		// Only call StreamingFunc for actual content deltas, not when there are tool calls
		// This prevents tool call data from being displayed in the chat
		if payload.StreamingFunc != nil && len(contentChunk) > 0 {
			slog.Debug("Streaming content", "chunk", string(contentChunk))
			err := payload.StreamingFunc(ctx, contentChunk)
			if err != nil {
				return nil, fmt.Errorf("streaming func returned an error: %w", err)
			}
		}
		// Only call StreamingReasoningFunc when there's actual reasoning content
		// Don't call it for regular content chunks - that's what StreamingFunc is for
		// Also don't pass contentChunk when there are tool calls
		if payload.StreamingReasoningFunc != nil && len(reasoningChunk) > 0 {
			slog.Debug("Streaming reasoning", "reasoning", string(reasoningChunk), "content", string(contentChunk))
			err := payload.StreamingReasoningFunc(ctx, reasoningChunk, contentChunk)
			if err != nil {
				return nil, fmt.Errorf("streaming reasoning func returned an error: %w", err)
			}
		}
	}
	return &response, nil
}

func updateFunctionCall(message ChatMessage, functionCall *FunctionCall) {
	if message.FunctionCall == nil {
		message.FunctionCall = functionCall
	} else {
		message.FunctionCall.Arguments += functionCall.Arguments
	}
}

func updateToolCalls(tools []ToolCall, delta []*ToolCall) []ToolCall {
	if len(delta) == 0 {
		return tools
	}
	for _, t := range delta {
		// Ensure we have enough space in the tools slice
		for len(tools) <= t.Index {
			tools = append(tools, ToolCall{})
		}

		existing := &tools[t.Index]

		// If this is a new tool call (ID not yet set), initialize it
		if existing.ID == "" && t.ID != "" {
			existing.ID = t.ID
			existing.Type = t.Type
		}

		// Preserve the function name - don't overwrite with empty
		if t.Function.Name != "" {
			existing.Function.Name = t.Function.Name
		}

		// Accumulate arguments
		if t.Function.Arguments != "" {
			existing.Function.Arguments += t.Function.Arguments
		}
	}

	return tools
}

// StreamingChatResponseTools is a helper function to append tool calls to the stack.
// Deprecated: This function previously returned a JSON-marshaled chunk which caused
// tool call JSON to be displayed in streaming output. Use updateToolCalls instead.
func StreamingChatResponseTools(tools []ToolCall, delta []*ToolCall) ([]byte, []ToolCall) {
	return []byte{}, updateToolCalls(tools, delta)
}
