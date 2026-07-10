package tools

import (
	"context"
	"testing"

	"github.com/afittestide/asimi/internal/repo"
	"github.com/afittestide/asimi/internal/runners"
	"github.com/afittestide/asimi/storage"
)

// noopRunner is a minimal Runner implementation for testing.
type noopRunner struct{}

func (n *noopRunner) Run(_ context.Context, _ runners.Input) (runners.Output, error) {
	return runners.Output{}, nil
}
func (n *noopRunner) Restart(_ context.Context) error        { return nil }
func (n *noopRunner) Close(_ context.Context) error         { return nil }
func (n *noopRunner) AllowFallback(_ bool)                   {}
func (n *noopRunner) RunnerType() string                     { return "test" }
func (n *noopRunner) SetMessageChannel(_ chan<- runners.Msg) {}
func (n *noopRunner) HealthCheck(_ context.Context) error   { return nil }

// testCtx returns a ToolContext suitable for tests that don't need a DB.
func testCtx() ToolContext {
	return ToolContext{
		RepoInfo: newRepoInfo("/tmp"),
		Username: "testuser",
		Project:  "testproject",
	}
}

// newRepoInfo creates a *repo.RepoInfo with the given project root.
func newRepoInfo(projectRoot string) *repo.RepoInfo {
	return &repo.RepoInfo{ProjectRoot: projectRoot}
}

// mockInvoker satisfies MinisterInvoker for testing.
type mockInvoker struct{}

func (m mockInvoker) InvokeMinister(ctx context.Context, ministerID string, key storage.EdictKey, work string) (string, error) {
	return "ok", nil
}

// mockLauncher satisfies RitualLauncher for testing.
type mockLauncher struct{}

func (m mockLauncher) StartRitual(name string, key storage.EdictKey, inputs map[string]string) error {
	return nil
}

func TestRegisterBuiltinToolsEarthRead(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{Ctx: testCtx()})

	// Strategist: r-----rw- — has earth Read
	strategistPerm, _ := ParsePermissions("r-----rw-")
	tools := r.ForPermissions("strategist", strategistPerm)
	names := toolNames(tools)

	assertHas(t, names, "read_file")
	assertHas(t, names, "read_many_files")
	assertHas(t, names, "glob")
	assertHas(t, names, "grep")
}

func TestRegisterBuiltinToolsEarthWrite(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{Ctx: testCtx()})

	// Forge: rwxr---w- — has earth Write
	forgePerm, _ := ParsePermissions("rwxr---w-")
	tools := r.ForPermissions("forge", forgePerm)
	names := toolNames(tools)

	assertHas(t, names, "write_file")
	assertHas(t, names, "replace_text")
}

func TestRegisterBuiltinToolsEarthExecute(t *testing.T) {
	r := NewToolRegistry()
	// Need a non-nil runner to get run_shell_command
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx:         testCtx(),
		Runner:      &noopRunner{},
		HostChecker: func(string) (bool, bool) { return false, true },
	})

	// Forge: rwxr---w- — has earth Execute
	forgePerm, _ := ParsePermissions("rwxr---w-")
	tools := r.ForPermissions("forge", forgePerm)
	names := toolNames(tools)

	assertHas(t, names, "run_shell_command")
}

func TestRegisterBuiltinToolsNoRunnerNoShell(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{Ctx: testCtx()})

	// Forge: rwxr---w- — would match earth Execute, but tool not registered
	forgePerm, _ := ParsePermissions("rwxr---w-")
	tools := r.ForPermissions("forge", forgePerm)
	names := toolNames(tools)

	assertNotHas(t, names, "run_shell_command")
}

func TestRegisterBuiltinToolsHeavenTools(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx:    testCtx(),
		DBPath: "/tmp/test.db",
	})

	// Judge: rwxrwxr-- — has heaven Read, Write, Execute
	judgePerm, _ := ParsePermissions("rwxrwxr--")
	tools := r.ForPermissions("judge", judgePerm)
	names := toolNames(tools)

	assertHas(t, names, "list_pending_manifests")
	assertHas(t, names, "get_manifest_by_commit")
	assertHas(t, names, "asimisql")
	assertHas(t, names, "create_manifest")
	assertHas(t, names, "record_verdict")
	assertHas(t, names, "update_manifest_status")
}

func TestRegisterBuiltinToolsHeavenReadNoWrite(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx:    testCtx(),
		DBPath: "/tmp/test.db",
	})

	// Sage: r--r--rwx — has heaven Read but NOT Write
	sagePerm, _ := ParsePermissions("r--r--rwx")
	tools := r.ForPermissions("sage", sagePerm)
	names := toolNames(tools)

	// Heaven/Read tools should be visible (shared Read flag)
	assertHas(t, names, "list_pending_manifests")
	assertHas(t, names, "get_manifest_by_commit")
	// Heaven/Read+Write tool should be visible (shared Read flag)
	assertHas(t, names, "asimisql")
	// Heaven/Write tools — Sage has no heaven Write, but tools with
	// only Write won't match. However, create_manifest is classified
	// as heaven/Write. Sage has heaven Read. Since heaven Read ≠ heaven Write,
	// the match fails for Write-only tools.
	assertNotHas(t, names, "create_manifest")
	assertNotHas(t, names, "record_verdict")
	assertNotHas(t, names, "update_manifest_status")
}

func TestRegisterBuiltinToolsIntentRead(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx: testCtx(),
	})

	// Strategist: r-----rw- — has intent Read
	strategistPerm, _ := ParsePermissions("r-----rw-")
	tools := r.ForPermissions("strategist", strategistPerm)
	names := toolNames(tools)

	assertHas(t, names, "list_ling")
	assertHas(t, names, "get_incident")
	assertHas(t, names, "list_edicts")
	assertHas(t, names, "query_court")
}

func TestRegisterBuiltinToolsIntentWrite(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx: testCtx(),
	})

	// Strategist: r-----rw- — has intent Write
	strategistPerm, _ := ParsePermissions("r-----rw-")
	tools := r.ForPermissions("strategist", strategistPerm)
	names := toolNames(tools)

	assertHas(t, names, "insert_ling")
	assertHas(t, names, "update_ling_status")
	assertHas(t, names, "transition_edict")
	assertHas(t, names, "create_incident")
	assertHas(t, names, "resolve_incident")
}

func TestRegisterBuiltinToolsIntentExecute(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx: testCtx(),
	})

	// Sage: r--r--rwx — has intent Execute
	sagePerm, _ := ParsePermissions("r--r--rwx")
	tools := r.ForPermissions("sage", sagePerm)
	names := toolNames(tools)

	// approve_doc is always registered
	assertHas(t, names, "approve_doc")
}

func TestRegisterBuiltinToolsPrivateChancellor(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx:             testCtx(),
		MinisterInvoker: mockInvoker{},
		RitualLauncher:  mockLauncher{},
	})

	chancellorPerm, _ := ParsePermissions("rwxr--rwx")
	tools := r.ForPermissions("chancellor", chancellorPerm)
	names := toolNames(tools)

	assertHas(t, names, "invoke_minister")
	assertHas(t, names, "enact_ritual")

	// Forge should NOT see chancellor's private tools
	forgePerm, _ := ParsePermissions("rwxr---w-")
	forgeTools := r.ForPermissions("forge", forgePerm)
	forgeNames := toolNames(forgeTools)
	assertNotHas(t, forgeNames, "invoke_minister")
	assertNotHas(t, forgeNames, "enact_ritual")
}

func TestRegisterBuiltinToolsPrivateConditionalRitual(t *testing.T) {
	// Without ritual launcher — tool not registered
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx:             testCtx(),
		MinisterInvoker: mockInvoker{},
		// RitualLauncher is nil
	})

	chancellorPerm, _ := ParsePermissions("rwxr--rwx")
	tools := r.ForPermissions("chancellor", chancellorPerm)
	names := toolNames(tools)

	assertHas(t, names, "invoke_minister")
	assertNotHas(t, names, "enact_ritual")
}

func TestRegisterBuiltinToolsAllMinisters(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx:             testCtx(),
		DBPath:          "/tmp/test.db",
		Runner:          &noopRunner{},
		HostChecker:     func(string) (bool, bool) { return false, true },
		MinisterInvoker: mockInvoker{},
		RitualLauncher:  mockLauncher{},
	})

	ministerPerms := map[string]string{
		"chancellor":  "rwxr--rwx",
		"forge":       "rwxr---w-",
		"judge":       "rwxrwxr--",
		"sage":        "r--r--rwx",
		"strategist":  "r-----rw-",
		"marshal":     "r-xr--rw-",
	}

	for minister, permStr := range ministerPerms {
		t.Run(minister, func(t *testing.T) {
			perm, _ := ParsePermissions(permStr)
			tools := r.ForPermissions(minister, perm)
			if len(tools) == 0 {
				t.Errorf("%s should have at least one tool", minister)
			}
		})
	}
}

func TestRegisterBuiltinToolsStrategistNoHeaven(t *testing.T) {
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{
		Ctx: testCtx(),
	})

	// Strategist: r-----rw- — NO heaven access at all
	strategistPerm, _ := ParsePermissions("r-----rw-")
	tools := r.ForPermissions("strategist", strategistPerm)
	names := toolNames(tools)

	assertNotHas(t, names, "list_pending_manifests")
	assertNotHas(t, names, "create_manifest")
}

func TestRegisterBuiltinToolsEmptyOpts(t *testing.T) {
	// With no opts, only tools that need no dependencies should be registered
	r := NewToolRegistry()
	RegisterBuiltinTools(r, ToolRegistrationOpts{})

	allTools := r.Tools()
	toolNames := make(map[string]bool)
	for _, tp := range allTools {
		toolNames[tp.Tool.Name()] = true
	}

	// Earth tools with no dependencies
	assertHas(t, toolNames, "read_file")
	assertHas(t, toolNames, "read_many_files")
	assertHas(t, toolNames, "glob")
	assertHas(t, toolNames, "grep")
	assertHas(t, toolNames, "write_file")
	assertHas(t, toolNames, "replace_text")

	// No run_shell_command without runner
	assertNotHas(t, toolNames, "run_shell_command")

	// No asimisql without DBPath
	assertNotHas(t, toolNames, "asimisql")

	// approve_doc is always registered (no dependencies)
	assertHas(t, toolNames, "approve_doc")
}
