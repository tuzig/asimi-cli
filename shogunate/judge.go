package shogunate

import (
	"context"

	"github.com/afittestide/asimi/shogunate/tools"
)

// JudgePrompt defines the Judge's identity and capabilities
const JudgePrompt = `刑部. Your domain is 天—test results and testing code.

You preside over the verdicts table. You review 'forged' manifests against the working tree. When tests pass, you update forge_manifest to 'quenched'. When they fail, you mark 'rejected'.

You are adversarial and data-driven. Your word is final.

CRITICAL RULES:
- If test criteria are ambiguous, invoke Zhengming—do not guess
- Code is guilty until proven innocent
- Verdicts are immutable once rendered
- Evidence must be preserved in JSON format
- You have read/write on verdicts and forge_manifest.status/verdict_id; execute access on shell`

// Judge evaluates code through CI and renders verdicts
type Judge struct {
	*MinisterBase // embedded base for database access and session creation
	ci            CIRunner
}

// NewJudge creates a new Judge minister
func NewJudge(base *MinisterBase, ci CIRunner) *Judge {
	base.ministerID = "judge"
	j := &Judge{
		MinisterBase: base,
		ci:           ci,
	}
	j.self = j
	return j
}

// SystemPrompt returns the Judge's system prompt template.
func (j *Judge) SystemPrompt() string {
	return JudgePrompt
}

// Tools returns the Judge's LLM tools for interactive sessions
func (j *Judge) Tools() []Tool {
	if j.toolRegistry != nil {
		perm, _ := tools.ParsePermissions("rwxrwxr--")
		registered := j.toolRegistry.ForPermissions("judge", perm)
		result := make([]Tool, len(registered))
		for i, t := range registered {
			result[i] = t
		}
		return result
	}
	// Fallback: edit tools only (no DB-dependent tools without registry)
	var toolList []Tool
	for _, t := range tools.GetEditTools(j.config.LLM, j.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	if j.runner != nil {
		toolList = append(toolList, tools.NewRunShellCommand(j.CheckHostCommand, j.runner, j.msgChan, j.RepoInfo().ProjectRoot))
	}
	return toolList
}

// Run starts the Judge's task processing loop
func (j *Judge) Run(ctx context.Context) {
	j.RunLoop(ctx, j, nil, j.MinisterBase.processTask)
}
