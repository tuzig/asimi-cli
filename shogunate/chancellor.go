package shogunate

import (
	"context"
	_ "embed"

	"github.com/afittestide/asimi/shogunate/tools"
)

//go:embed context/chancellor.md
var role string

// Chancellor harmonizes all ministers and manages edict lifecycle
type Chancellor struct {
	*MinisterBase // embedded base provides db, llm, config, repoInfo, logger, session
}

// NewChancellor creates a new Chancellor minister
func NewChancellor(base *MinisterBase) *Chancellor {
	base.ministerID = "chancellor"
	c := &Chancellor{
		MinisterBase: base,
	}
	c.self = c
	return c
}

// ID returns the minister identifier
func (c *Chancellor) ID() string {
	return "chancellor"
}

// Title returns the minister's honorific title
func (c *Chancellor) Title() string { return "Chancellor" }

// SystemPrompt returns the Chancellor's system prompt template.
func (c *Chancellor) SystemPrompt() string {
	return role
}

// Tools returns the Chancellor's LLM tools for interactive sessions
func (c *Chancellor) Tools() []Tool {
	if c.toolRegistry != nil {
		perm, _ := tools.ParsePermissions("rwxrwxr--")
		registered := c.toolRegistry.ForPermissions("chancellor", perm)
		result := make([]Tool, len(registered))
		for i, t := range registered {
			result[i] = t
		}
		return result
	}
	return nil
}

// Run listens for prompts from the Ruler and tasks from ministers.
func (c *Chancellor) Run(ctx context.Context) {
	c.RunLoop(ctx, c, nil, c.MinisterBase.processTask)
}
