package shogunate

import (
	"context"

	"github.com/afittestide/asimi/shogunate/tools"
)

// Forge receives Tasks from the Chancellor via the tasks channel.
// When an LLM is configured, it creates sessions to process tasks through tool execution.
type Forge struct {
	*MinisterBase // embedded base for database access and session creation
}

// NewForge creates a new Forge that processes tasks via the Task pattern.
func NewForge(base *MinisterBase) *Forge {
	base.ministerID = "forge"
	f := &Forge{
		MinisterBase: base,
	}
	f.self = f
	return f
}

// SystemPrompt returns the Forge's system prompt template.
func (f *Forge) SystemPrompt() string {
	return `工部. Your domain is 地—simple, clear code forged into existence.

Your ledger is the forge_manifest table. You create manifests with status='forged' and leave them for the Judge to review. You do NOT commit code—commits happen after Judge and Censor approve. When status='rejected', you reforge.

CRITICAL RULES:
- If requirements are unclear, invoke Zhengming—do not guess
- When work is done, create a manifest to record the change (status will be 'forged')
- Generate idiomatic, clear code
- Write tests to verify your code will always work
- Run only the tests that cover your code as the 刑部 will run the complete testing suite
- Do NOT commit to git - other members of the shogunate need to approve your work`
}

// Tools returns the Forge's LLM tools for interactive sessions.
func (f *Forge) Tools() []Tool {
	if f.toolRegistry != nil {
		perm, _ := tools.ParsePermissions("rwxr---w-")
		registered := f.toolRegistry.ForPermissions("forge", perm)
		result := make([]Tool, len(registered))
		for i, t := range registered {
			result[i] = t
		}
		return result
	}
	// Fallback: file tools only (no DB-dependent tools without registry)
	var toolList []Tool
	for _, t := range tools.GetFileTools(f.config.LLM, f.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	if f.runner != nil {
		toolList = append(toolList, tools.NewRunShellCommand(f.CheckHostCommand, f.runner, f.msgChan, f.RepoInfo().ProjectRoot))
	}
	return toolList
}

// Run starts the Forge's processing loop
func (f *Forge) Run(ctx context.Context) {
	f.RunLoop(ctx, f, nil, f.MinisterBase.processTask)
}
