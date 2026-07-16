package tools

import (
	"context"

	"github.com/afittestide/asimi/internal/runners"
)

// ToolRegistrationOpts bundles all dependencies needed to construct and
// register builtin tools. Ctx carries shared identity (RepoInfo, MinisterID,
// Username, Project, DB) — tools query the DB directly through Ctx.DB.
//
// Chancellor-backed interfaces (EdictManager, ZhengmingRequester,
// MinisterInvoker, RitualLauncher) need runtime dispatch and are kept.
//
// Nil fields are safe — the corresponding tools simply won't be registered.
type ToolRegistrationOpts struct {
	Ctx         ToolContext
	Runner      runners.Runner
	MsgChan     *chan<- runners.Msg
	HostChecker func(cmd string) (runOnHost, needsApproval bool)
	DBPath      string

	// Chancellor-backed — need runtime dispatch, not DB
	EdictManager       EdictManager
	ZhengmingRequester ZhengmingRequester
	WaitForZhengming   func(ctx context.Context, requestID string) (string, error)
	NotifyFn           func() func(any)

	MinisterInvoker MinisterInvoker
	RitualLauncher  RitualLauncher
}

// RegisterBuiltinTools populates the registry with all builtin tools,
// classified by realm and access level.
func RegisterBuiltinTools(registry *ToolRegistry, opts ToolRegistrationOpts) {
	registerEarthTools(registry, opts)
	registerHeavenTools(registry, opts)
	registerIntentTools(registry, opts)
	registerExtraTools(registry, opts)
}

// registerEarthTools registers source code operation tools.
func registerEarthTools(r *ToolRegistry, opts ToolRegistrationOpts) {
	earthRead := Permissions{Earth: Access{Read: true}}
	earthWrite := Permissions{Earth: Access{Write: true}}
	earthExec := Permissions{Earth: Access{Execute: true}}

	projectRoot := opts.Ctx.ProjectRoot()

	r.Register(NewReadFileTool(projectRoot), earthRead)
	r.Register(ReadManyFilesTool{ProjectRoot: projectRoot}, earthRead)
	r.Register(GlobTool{ProjectRoot: projectRoot}, earthRead)
	r.Register(GrepTool{ProjectRoot: projectRoot}, earthRead)
	r.Register(WriteFileTool{ProjectRoot: projectRoot}, earthWrite)
	r.Register(ReplaceTextTool{ProjectRoot: projectRoot}, earthWrite)

	if opts.Runner != nil {
		r.Register(NewRunShellCommand(opts.HostChecker, opts.Runner, opts.MsgChan, projectRoot), earthExec)
	}
}

// registerHeavenTools registers test/verification tools.
func registerHeavenTools(r *ToolRegistry, opts ToolRegistrationOpts) {
	heavenRead := Permissions{Heaven: Access{Read: true}}
	heavenReadWrite := Permissions{Heaven: Access{Read: true, Write: true}}
	heavenWrite := Permissions{Heaven: Access{Write: true}}

	r.Register(ListPendingManifestsTool{Ctx: opts.Ctx}, heavenRead)
	r.Register(GetManifestByCommitTool{Ctx: opts.Ctx}, heavenRead)

	if opts.DBPath != "" {
		r.Register(AsimiSQLTool{DBPath: opts.DBPath, ProjectRoot: opts.Ctx.ProjectRoot()}, heavenReadWrite)
	}

	r.Register(CreateManifestTool{Ctx: opts.Ctx}, heavenWrite)
	r.Register(RecordVerdictTool{Ctx: opts.Ctx}, heavenWrite)
	r.Register(UpdateManifestStatusTool{Ctx: opts.Ctx}, heavenWrite)
}

// registerIntentTools registers edict/ling/zhengming tools.
func registerIntentTools(r *ToolRegistry, opts ToolRegistrationOpts) {
	intentRead := Permissions{Intent: Access{Read: true}}
	intentWrite := Permissions{Intent: Access{Write: true}}
	intentExec := Permissions{Intent: Access{Execute: true}}

	r.Register(ListLingTool{Ctx: opts.Ctx}, intentRead)
	if opts.EdictManager != nil {
		r.Register(GetEdictStatusTool{
			Manager:  opts.EdictManager,
			DB:       opts.Ctx.DB,
			Username: opts.Ctx.Username,
			Project:  opts.Ctx.Project,
		}, intentRead)
	}
	r.Register(ListEdictsTool{DB: opts.Ctx.DB, Username: opts.Ctx.Username, Project: opts.Ctx.Project}, intentRead)
	r.Register(QueryCourtTool{DB: opts.Ctx.DB, Username: opts.Ctx.Username, Project: opts.Ctx.Project}, intentRead)
	r.Register(ListQuenchedManifestsTool{Ctx: opts.Ctx}, intentRead)
	r.Register(QueryPrecedentsTool{Ctx: opts.Ctx}, intentRead)
	r.Register(GetIncidentTool{Ctx: opts.Ctx}, intentRead)

	r.Register(InsertLingTool{Ctx: opts.Ctx}, intentWrite)
	r.Register(UpdateLingStatusTool{Ctx: opts.Ctx}, intentWrite)
	if opts.EdictManager != nil {
		r.Register(UpdateEdictTool{Manager: opts.EdictManager, Username: opts.Ctx.Username, Project: opts.Ctx.Project}, intentWrite)
	}
	r.Register(TransitionEdictTool{DB: opts.Ctx.DB, Username: opts.Ctx.Username, Project: opts.Ctx.Project}, intentWrite)
	r.Register(CreateIncidentTool{Ctx: opts.Ctx}, intentWrite)
	r.Register(ResolveIncidentTool{Ctx: opts.Ctx}, intentWrite)

	if opts.ZhengmingRequester != nil && opts.NotifyFn != nil {
		r.Register(SuggestEdictTool{
			Ctx:       opts.Ctx,
			Requester: opts.ZhengmingRequester,
			NotifyFn:  opts.NotifyFn,
		}, intentExec)
	}
	r.Register(RecordPrecedentTool{
		Ctx: opts.Ctx,
	}, intentExec)
	r.Register(ApproveDocTool{}, intentExec)
}

// registerExtraTools registers static and factory-based extra tools.
// Static extra tools (invoke_minister, enact_ritual) are registered once
// and returned to any minister whose def lists them in extra_tools.
// Factory extra tools (request_zhengming) produce a per-minister instance
// so the tool carries the correct MinisterID for routing.
func registerExtraTools(r *ToolRegistry, opts ToolRegistrationOpts) {
	if opts.MinisterInvoker != nil {
		r.RegisterExtra("invoke_minister", InvokeMinisterTool{Ctx: opts.Ctx, Invoker: opts.MinisterInvoker})
	}
	if opts.RitualLauncher != nil {
		r.RegisterExtra("enact_ritual", InvokeRitualTool{Ctx: opts.Ctx, Launcher: opts.RitualLauncher})
	}
	if opts.ZhengmingRequester != nil {
		r.RegisterExtraFactory("request_zhengming", func(mid string) Tool {
			return RequestZhengmingTool{
				MinisterID:    mid,
				Requester:     opts.ZhengmingRequester,
				WaitForAnswer: opts.WaitForZhengming,
				Username:      opts.Ctx.Username,
				Project:       opts.Ctx.Project,
			}
		})
	}
}
