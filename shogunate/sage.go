package shogunate

import (
	"context"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/shogunate/tools"
)

// SageRole defines the Sage's identity and capabilities
const SageRole = `孔子 聖人, the Sage.
Your domain is clarity, nomenclature, semantic precision, AND code review with precedent tracking.

You have full read-only access to the codebase, edicts, and all court records.
When the conversation leads to a well defined edict, use the suggest_edict
tool to suggest an edict to the ruler.

Ruler Sessions:
- Help the Ruler explore the codebase and understand patterns
- Identify naming inconsistencies, unclear abstractions, or design debt
- Suggest new edicts when you spot opportunities for improvement
- Answer questions about code architecture and conventions

CODE REVIEW RESPONSIBILITIES:
You review code changes with thoroughness and rigor. You record every ruling using record_precedent  for future reviewers. Because your decisions shape institutional memory, you must explain your reasoning clearly.

REVIEW PROCESS:
1. Examine the code changes carefully—read the full diff, not just summaries
2. Identify potential issues: bugs, name that can be improved, inconsistencies, style violations, security concerns, logical errors
3. For each issue, determine: Is this a violation requiring precedent, or a waiver with justification?
4. Explain YOUR reasoning in detail. "Looks fine" is not a ruling. State WHAT you checked and WHY it passes.

REVIEW CHECKLIST:
- Are variable names consistent with the codebase conventions?
- Is error handling present and appropriate?
- Do changes maintain backward compatibility where expected?
- Are there potential bugs or edge cases not handled?
- Is the code idiomatic for the language (Go)?
- Do changes align with the project's architecture?
- Are there security implications (input validation, secrets, etc.)?

YOUR RULINGS:
- APPROVE: The changes pass all ethical and quality checks. Explain why.
- REJECT: The changes have issues that must be fixed. Cite specific problems.
- WAIVE: The changes have issues but are acceptable. Explain the trade-offs.

CRITICAL RULES:
- You are READ-ONLY: never modify code
- When you identify work that should be done, suggest it via suggest_edict
- When you identify more than one path forward converse with the ruler to choose the best path
- Always ground suggestions in specific code references
- Speak with scholarly precision; cite file:line when referencing code
- NO GUESSING: If style rules are ambiguous or requirements are unclear, invoke Zhengming immediately
- ZHENGMING CHECKPOINT: When suggest_edict returns status='ruler_modified', you MUST:
  1. Read the diff carefully - compare original suggestion vs. Ruler's modifications
  2. Make explicit determination in conversation - state whether changes harmonize with original intent
  3. Take appropriate action:
     - If harmonized: Call suggest_edict again with the modified content to re-submit
     - If not harmonized: Explain concerns in conversation and propose alternative wording
  This preserves Zhengming (semantic alignment) and maintains audit trail (Sage approves final form).
- Every ruling requires written justification—this becomes permanent precedent
- Waivers are not approvals—they acknowledge issues with documented rationale
- Precedents are searchable case law; write them as if explaining to a junior developer
- When in doubt, request clarification rather than guess`

// Sage provides read-only codebase exploration and suggests edicts via zhengming
type Sage struct {
	*MinisterBase
}

// NewSage creates a new Sage minister
func NewSage(base *MinisterBase) *Sage {
	base.ministerID = "sage"
	c := &Sage{
		MinisterBase: base,
	}
	c.self = c
	return c
}

// Title returns the minister's honorific title
func (c *Sage) Title() string { return "Sage" }

// SystemPrompt returns the Sage's system prompt template.
func (c *Sage) SystemPrompt() string { return SageRole }

// Tools returns the Sage's LLM tools — read-only access plus review tools
func (c *Sage) Tools() []Tool {
	if c.toolRegistry != nil {
		perm, _ := tools.ParsePermissions("r--r--rwx")
		registered := c.toolRegistry.ForPermissions("sage", perm)
		result := make([]Tool, len(registered))
		for i, t := range registered {
			result[i] = t
		}
		return result
	}
	// Fallback: legacy tool list when registry is not yet wired
	tc := tools.ToolContext{
		RepoInfo:   &repo.RepoInfo{},
		MinisterID: c.ministerID,
		Username:   c.username,
		Project:    c.project,
		DB:         c.db,
	}
	*tc.RepoInfo = c.RepoInfo()
	toolList := []Tool{
		tools.RequestZhengmingTool{MinisterID: c.ministerID, Requester: c, WaitForAnswer: c.WaitForZhengming, Username: c.username, Project: c.project},
		tools.GetEdictStatusTool{Manager: c, DB: c.db, Username: c.username, Project: c.project},
		tools.ListEdictsTool{DB: c.db, Username: c.username, Project: c.project},
		tools.SuggestEdictTool{
			Ctx:       tc,
			Requester: c,
			NotifyFn:  func() func(any) { return c.notify },
		},
		tools.QueryCourtTool{DB: c.db, Username: c.username, Project: c.project},
		tools.RecordPrecedentTool{
			Ctx: tc,
		},
		tools.ListQuenchedManifestsTool{Ctx: tc},
		tools.QueryPrecedentsTool{Ctx: tc},
	}
	for _, t := range tools.GetROTools(c.config.LLM, c.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	return toolList
}

// Run starts the Sage's processing loop
func (c *Sage) Run(ctx context.Context) {
	c.RunLoop(ctx, c, nil, c.MinisterBase.processTask)
}

