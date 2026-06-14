package tools

import (
	"context"

	"github.com/afittestide/asimi/internal/runners"
	"gorm.io/gorm"
)

// ToolRegistrationOpts bundles all dependencies needed to construct and
// register builtin tools. Fields are grouped by concern:
//   - Common: DB, ProjectRoot, Runner, MsgChan, HostChecker, Username, Project
//   - Intent interfaces: EdictManager, ZhengmingRequester, PrecedentStore
//   - Minister-bound tools: pre-constructed Tool instances for tools that
//     embed minister references (defined in the shogunate package)
//
// Nil fields are safe — the corresponding tools simply won't be registered.
type ToolRegistrationOpts struct {
	// Common dependencies (used by earth and heaven tools)
	DB          *gorm.DB
	DBPath      string
	ProjectRoot string
	Runner      runners.Runner
	MsgChan     chan<- runners.Msg
	HostChecker func(cmd string) (runOnHost, needsApproval bool)
	Username    string
	Project     string

	// Intent — EdictManager (implements update_edict, get_edict_status)
	EdictManager EdictManager

	// Intent — ZhengmingRequester + WaitForZhengming (implements request_zhengming)
	ZhengmingRequester ZhengmingRequester
	WaitForZhengming   func(ctx context.Context, requestID string) (string, error)

	// ZhengmingMinisterIDs lists minister IDs that should get a private
	// request_zhengming tool with their own MinisterID for correct routing.
	// Each gets a RegisterPrivate instance; no shared public instance is registered.
	ZhengmingMinisterIDs []string

	// Intent — PrecedentStore (implements record_precedent, list_quenched_manifests, query_precedents)
	PrecedentStore PrecedentStore

	// Intent — FailureRecorder for record_precedent
	AddFailure FailureRecorder

	// Intent — NotifyFn for suggest_edict (lazy getter for current notify)
	NotifyFn func() func(any)

	// Minister-bound tools — pre-constructed Tool instances passed from
	// the shogunate package. These embed minister references and can't be
	// constructed here (circular import).
	//
	// Heaven tools
	ListPendingManifestsTool Tool // list_pending_manifests
	GetManifestByCommitTool  Tool // get_manifest_by_commit
	CreateManifestTool       Tool // create_manifest
	RecordVerdictTool        Tool // record_verdict
	UpdateManifestStatusTool Tool // update_manifest_status

	// Intent tools
	InsertLingTool       Tool // insert_ling
	ListLingTool         Tool // list_ling
	UpdateLingStatusTool Tool // update_ling_status
	CreateIncidentTool   Tool // create_incident
	ResolveIncidentTool  Tool // resolve_incident
	GetIncidentTool      Tool // get_incident

	// Private tools (chancellor only)
	InvokeMinisterTool Tool // invoke_minister (nil if unavailable)
	EnactRitualTool    Tool // enact_ritual (nil if no ritual runner)
}

// RegisterBuiltinTools populates the registry with all builtin tools,
// classified by realm and access level.
//
// Permission classifications:
//
//	Earth  (source code):
//	  Read:    read_file, read_many_files, glob, grep
//	  Write:   write_file, replace_text
//	  Execute: run_shell_command
//
//	Heaven (test/verification):
//	  Read:      list_pending_manifests, get_manifest_by_commit
//	  Read+Write: asimisql
//	  Write:     create_manifest, record_verdict, update_manifest_status
//
//	Intent (edict/ling/zhengming):
//	  Read:    list_ling, get_edict_status, list_edicts, query_court,
//	           list_quenched_manifests, query_precedents, get_incident
//	  Write:   insert_ling, update_ling_status, update_edict,
//	           transition_edict, create_incident, resolve_incident
//	  Execute: request_zhengming, suggest_edict, record_precedent, approve_doc
//
//	Private (chancellor only):
//	  invoke_minister, enact_ritual
func RegisterBuiltinTools(registry *ToolRegistry, opts ToolRegistrationOpts) {
	registerEarthTools(registry, opts)
	registerHeavenTools(registry, opts)
	registerIntentTools(registry, opts)
	registerPrivateTools(registry, opts)
}

// registerEarthTools registers source code operation tools.
func registerEarthTools(r *ToolRegistry, opts ToolRegistrationOpts) {
	earthRead := Permissions{Earth: Access{Read: true}}
	earthWrite := Permissions{Earth: Access{Write: true}}
	earthExec := Permissions{Earth: Access{Execute: true}}

	// Earth/Read — file exploration tools
	r.Register(NewReadFileTool(opts.ProjectRoot), earthRead)
	r.Register(ReadManyFilesTool{ProjectRoot: opts.ProjectRoot}, earthRead)
	r.Register(GlobTool{ProjectRoot: opts.ProjectRoot}, earthRead)
	r.Register(GrepTool{ProjectRoot: opts.ProjectRoot}, earthRead)

	// Earth/Write — file modification tools
	r.Register(WriteFileTool{ProjectRoot: opts.ProjectRoot}, earthWrite)
	r.Register(ReplaceTextTool{ProjectRoot: opts.ProjectRoot}, earthWrite)

	// Earth/Execute — shell command execution
	if opts.Runner != nil {
		r.Register(NewRunShellCommand(opts.HostChecker, opts.Runner, opts.MsgChan, opts.ProjectRoot), earthExec)
	}
}

// registerHeavenTools registers test/verification tools.
func registerHeavenTools(r *ToolRegistry, opts ToolRegistrationOpts) {
	heavenRead := Permissions{Heaven: Access{Read: true}}
	heavenReadWrite := Permissions{Heaven: Access{Read: true, Write: true}}
	heavenWrite := Permissions{Heaven: Access{Write: true}}

	// Heaven/Read — manifest inspection
	if opts.ListPendingManifestsTool != nil {
		r.Register(opts.ListPendingManifestsTool, heavenRead)
	}
	if opts.GetManifestByCommitTool != nil {
		r.Register(opts.GetManifestByCommitTool, heavenRead)
	}

	// Heaven/Read+Write — SQL database queries
	if opts.DBPath != "" {
		r.Register(AsimiSQLTool{DBPath: opts.DBPath, ProjectRoot: opts.ProjectRoot}, heavenReadWrite)
	}

	// Heaven/Write — manifest and verdict recording
	if opts.CreateManifestTool != nil {
		r.Register(opts.CreateManifestTool, heavenWrite)
	}
	if opts.RecordVerdictTool != nil {
		r.Register(opts.RecordVerdictTool, heavenWrite)
	}
	if opts.UpdateManifestStatusTool != nil {
		r.Register(opts.UpdateManifestStatusTool, heavenWrite)
	}
}

// registerIntentTools registers edict/ling/zhengming tools.
func registerIntentTools(r *ToolRegistry, opts ToolRegistrationOpts) {
	intentRead := Permissions{Intent: Access{Read: true}}
	intentWrite := Permissions{Intent: Access{Write: true}}
	intentExec := Permissions{Intent: Access{Execute: true}}

	// Intent/Read — edict and ling inspection
	if opts.ListLingTool != nil {
		r.Register(opts.ListLingTool, intentRead)
	}
	if opts.EdictManager != nil {
		r.Register(GetEdictStatusTool{Manager: opts.EdictManager, DB: opts.DB, Username: opts.Username, Project: opts.Project}, intentRead)
	}
	r.Register(ListEdictsTool{DB: opts.DB, Username: opts.Username, Project: opts.Project}, intentRead)
	r.Register(QueryCourtTool{DB: opts.DB, Username: opts.Username, Project: opts.Project}, intentRead)
	if opts.PrecedentStore != nil {
		r.Register(ListQuenchedManifestsTool{Store: opts.PrecedentStore, Username: opts.Username, Project: opts.Project}, intentRead)
		r.Register(QueryPrecedentsTool{Store: opts.PrecedentStore, Username: opts.Username, Project: opts.Project}, intentRead)
	}
	if opts.GetIncidentTool != nil {
		r.Register(opts.GetIncidentTool, intentRead)
	}

	// Intent/Write — edict, ling, and incident modification
	if opts.InsertLingTool != nil {
		r.Register(opts.InsertLingTool, intentWrite)
	}
	if opts.UpdateLingStatusTool != nil {
		r.Register(opts.UpdateLingStatusTool, intentWrite)
	}
	if opts.EdictManager != nil {
		r.Register(UpdateEdictTool{Manager: opts.EdictManager, Username: opts.Username, Project: opts.Project}, intentWrite)
	}
	r.Register(TransitionEdictTool{DB: opts.DB, Username: opts.Username, Project: opts.Project}, intentWrite)
	if opts.CreateIncidentTool != nil {
		r.Register(opts.CreateIncidentTool, intentWrite)
	}
	if opts.ResolveIncidentTool != nil {
		r.Register(opts.ResolveIncidentTool, intentWrite)
	}

	// Intent/Execute — zhengming, edict suggestions, precedent recording, doc review
	// Register per-minister private request_zhengming tools so each carries
	// the correct MinisterID for routing. No shared public instance.
	for _, mid := range opts.ZhengmingMinisterIDs {
		r.RegisterPrivate(mid, RequestZhengmingTool{
			MinisterID:    mid,
			Requester:     opts.ZhengmingRequester,
			WaitForAnswer: opts.WaitForZhengming,
			Username:      opts.Username,
			Project:       opts.Project,
		})
	}
	if opts.ZhengmingRequester != nil && opts.NotifyFn != nil {
		r.Register(SuggestEdictTool{
			Requester: opts.ZhengmingRequester,
			NotifyFn:  opts.NotifyFn,
			Username:  opts.Username,
			Project:   opts.Project,
		}, intentExec)
	}
	if opts.PrecedentStore != nil {
		r.Register(RecordPrecedentTool{
			Store:      opts.PrecedentStore,
			Username:   opts.Username,
			Project:    opts.Project,
			AddFailure: opts.AddFailure,
		}, intentExec)
	}
	r.Register(ApproveDocTool{}, intentExec)
}

// registerPrivateTools registers minister-exclusive tools.
func registerPrivateTools(r *ToolRegistry, opts ToolRegistrationOpts) {
	// Chancellor private tools
	if opts.InvokeMinisterTool != nil {
		r.RegisterPrivate("chancellor", opts.InvokeMinisterTool)
	}
	if opts.EnactRitualTool != nil {
		r.RegisterPrivate("chancellor", opts.EnactRitualTool)
	}
}
