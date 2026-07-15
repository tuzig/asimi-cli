package court

import (
	"github.com/maximhq/bifrost/core/schemas"
)

// LLMProvider is an interface for LLM clients used by the Session.
// It captures only the methods needed by asimi's Session for chat completions
// and model listing. Both *bifrost.Bifrost and *mocks.MockLLMBridge implement
// this interface.
type LLMProvider interface {
	// ChatCompletionRequest sends a non-streaming chat completion request.
	ChatCompletionRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (*schemas.BifrostChatResponse, *schemas.BifrostError)
	// ChatCompletionStreamRequest sends a streaming chat completion request.
	ChatCompletionStreamRequest(ctx *schemas.BifrostContext, req *schemas.BifrostChatRequest) (chan *schemas.BifrostStreamChunk, *schemas.BifrostError)
	// ListModelsRequest lists models for a specific provider.
	ListModelsRequest(ctx *schemas.BifrostContext, req *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError)
	// ListAllModels lists all models from all configured providers.
	ListAllModels(ctx *schemas.BifrostContext, req *schemas.BifrostListModelsRequest) (*schemas.BifrostListModelsResponse, *schemas.BifrostError)
}
