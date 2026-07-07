package shogunate

import (
	"context"

	"github.com/afittestide/asimi/shogunate/tools"
)

// StrategistRole defines the Strategist's identity and capabilities
const StrategistRole = `兵部, and the planner of the shogunate.
Your domain is strategy and sequence.

When you are summoned for Planning, you decompose the edict into executable ling (令, task orders) with clear dependencies. All changes to a specific file must run in sequence as panellization destroys isolation.
You enforce temporal order for large efforts.

Speak in milestones and dependency graphs.
Use the insert_ling tool repeatedly to break a large task into mutiple small ones.

CRITICAL RULES:
- If the Ruler's intent is ambiguous, invoke Zhengming—do not guess
- Each ling must be atomic, clear, and testable
- Dependencies must form a directed acyclic graph (no cycles)
- Dependencies must use exact ling_id values (e.g. '74183c66ba0507ba') as returned by insert_ling. Never use shorthand like '470-1' — the DAG resolver only matches full ling_ids.
- You have read/write on ling; read-only on edicts and filesystem
- Break complex tasks ito multiple lings when possible`

// Strategist decomposes edicts into executable ling (令, task orders)
type Strategist struct {
	*MinisterBase // embedded base for database access and session creation
}

// NewStrategist creates a new Strategist minister
func NewStrategist(base *MinisterBase) *Strategist {
	base.ministerID = "strategist"
	s := &Strategist{
		MinisterBase: base,
	}
	s.self = s
	return s
}

// SystemPrompt returns the Strategist's system prompt template.
func (s *Strategist) SystemPrompt() string {
	return StrategistRole
}

// Tools returns the Strategist's LLM tools for interactive sessions
func (s *Strategist) Tools() []Tool {
	if s.toolRegistry != nil {
		perm, _ := tools.ParsePermissions("r-----rw-")
		registered := s.toolRegistry.ForPermissions("strategist", perm)
		result := make([]Tool, len(registered))
		for i, t := range registered {
			result[i] = t
		}
		return result
	}
	// Fallback: zhengming + read-only tools (no DB-dependent tools without registry)
	toolList := []Tool{
		tools.RequestZhengmingTool{MinisterID: s.ministerID, Requester: s, WaitForAnswer: s.WaitForZhengming, Username: s.Username(), Project: s.Project()},
	}
	for _, t := range tools.GetROTools(s.config.LLM, s.RepoInfo().ProjectRoot) {
		toolList = append(toolList, t)
	}
	return toolList
}

// Run starts the Strategist's task processing loop
func (s *Strategist) Run(ctx context.Context) {
	s.RunLoop(ctx, s, nil, s.MinisterBase.processTask)
}
