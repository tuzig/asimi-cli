package tools

import (
	"context"
	"testing"
)

// mockTool is a minimal Tool implementation for testing.
type mockTool struct {
	name string
}

func (m mockTool) Name() string                                       { return m.name }
func (m mockTool) Description() string                                { return "" }
func (m mockTool) Call(_ context.Context, _ string) (string, error)   { return "", nil }
func (m mockTool) Format(_, _ string, _ error) string                { return "" }
func (m mockTool) ParameterSchema() map[string]any                   { return nil }

// ---------------------------------------------------------------------------
// ParsePermissions — valid strings
// ---------------------------------------------------------------------------

func TestParsePermissionsValid(t *testing.T) {
	tests := []struct {
		input   string
		earth  Access
		heaven Access
		intent Access
	}{
		{
			input:   "rwxrwxrwx",
			earth:  Access{true, true, true},
			heaven: Access{true, true, true},
			intent: Access{true, true, true},
		},
		{
			input:   "r-----rw-",
			earth:  Access{true, false, false},
			heaven: Access{false, false, false},
			intent: Access{true, true, false},
		},
		{
			input:   "r-----rwx",
			earth:  Access{true, false, false},
			heaven: Access{false, false, false},
			intent: Access{true, true, true},
		},
		{
			input:   "r--r--rwx",
			earth:  Access{true, false, false},
			heaven: Access{true, false, false},
			intent: Access{true, true, true},
		},
		{
			input:   "r-xr--rw-",
			earth:  Access{true, false, true},
			heaven: Access{true, false, false},
			intent: Access{true, true, false},
		},
		{
			input:   "rwxr---w-",
			earth:  Access{true, true, true},
			heaven: Access{true, false, false},
			intent: Access{false, true, false},
		},
		{
			input:   "rwxrwxr--",
			earth:  Access{true, true, true},
			heaven: Access{true, true, true},
			intent: Access{true, false, false},
		},
		{
			input:   "rwxr--rwx",
			earth:  Access{true, true, true},
			heaven: Access{true, false, false},
			intent: Access{true, true, true},
		},
		{
			input:   "---------",
			earth:  Access{},
			heaven: Access{},
			intent: Access{},
		},
		{
			// Only read in every realm
			input:   "r--r--r--",
			earth:  Access{Read: true},
			heaven: Access{Read: true},
			intent: Access{Read: true},
		},
		{
			// Only write in every realm
			input:   "-w--w--w-",
			earth:  Access{Write: true},
			heaven: Access{Write: true},
			intent: Access{Write: true},
		},
		{
			// Only execute in every realm
			input:   "--x--x--x",
			earth:  Access{Execute: true},
			heaven: Access{Execute: true},
			intent: Access{Execute: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			p, err := ParsePermissions(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if p.Earth != tt.earth {
				t.Errorf("earth = %+v, want %+v", p.Earth, tt.earth)
			}
			if p.Heaven != tt.heaven {
				t.Errorf("heaven = %+v, want %+v", p.Heaven, tt.heaven)
			}
			if p.Intent != tt.intent {
				t.Errorf("intent = %+v, want %+v", p.Intent, tt.intent)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParsePermissions — error cases
// ---------------------------------------------------------------------------

func TestParsePermissionsErrors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"too_short", "rwx"},
		{"too_short_6", "rwxrwx"},
		{"too_long", "rwxrwxrwxrwx"},
		{"invalid_char_in_earth_read", "awxrwxrwx"},
		{"invalid_char_in_earth_write", "rawrwxrwx"},
		{"invalid_char_in_heaven_write", "rwxrawrwx"},
		{"invalid_char_in_intent_execute", "rwxrwxrwa"},
		{"numeric_char", "rwx1wxrwx"},
		{"space_char", "rwxrwxrw "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePermissions(tt.input)
			if err == nil {
				t.Fatalf("expected error for input %q, got nil", tt.input)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ParsePermissions — String round-trip
// ---------------------------------------------------------------------------

func TestPermissionsStringRoundTrip(t *testing.T) {
	strings := []string{
		"rwxrwxrwx",
		"r-----rw-",
		"r-----rwx",
		"r--r--rwx",
		"r-xr--rw-",
		"rwxr---w-",
		"rwxrwxr--",
		"rwxr--rwx",
		"---------",
		"r--r--r--",
		"-w--w--w-",
		"--x--x--x",
	}
	for _, s := range strings {
		t.Run(s, func(t *testing.T) {
			p, err := ParsePermissions(s)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			got := p.String()
			if got != s {
				t.Errorf("String() = %q, want %q", got, s)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Permissions.Match — per-flag matching logic
// ---------------------------------------------------------------------------

func TestPermissionsMatchReadRead(t *testing.T) {
	minister := Permissions{Earth: Access{Read: true}}
	tool := Permissions{Earth: Access{Read: true}}

	if !minister.Match(tool) {
		t.Error("minister with earth Read should match tool with earth Read")
	}
}

func TestPermissionsMatchWriteWrite(t *testing.T) {
	minister := Permissions{Intent: Access{Write: true}}
	tool := Permissions{Intent: Access{Write: true}}

	if !minister.Match(tool) {
		t.Error("minister with intent Write should match tool with intent Write")
	}
}

func TestPermissionsMatchExecuteExecute(t *testing.T) {
	minister := Permissions{Heaven: Access{Execute: true}}
	tool := Permissions{Heaven: Access{Execute: true}}

	if !minister.Match(tool) {
		t.Error("minister with heaven Execute should match tool with heaven Execute")
	}
}

func TestPermissionsMatchReadVsWriteNoMatch(t *testing.T) {
	// Minister has Read, tool has only Write in same realm — no match
	minister := Permissions{Earth: Access{Read: true}}
	tool := Permissions{Earth: Access{Write: true}}

	if minister.Match(tool) {
		t.Error("minister with earth Read should NOT match tool with only earth Write")
	}
}

func TestPermissionsMatchWriteVsExecuteNoMatch(t *testing.T) {
	// Minister has Write, tool has only Execute in same realm — no match
	minister := Permissions{Earth: Access{Write: true}}
	tool := Permissions{Earth: Access{Execute: true}}

	if minister.Match(tool) {
		t.Error("minister with earth Write should NOT match tool with only earth Execute")
	}
}

func TestPermissionsMatchCrossRealmNoMatch(t *testing.T) {
	// Minister has Read in earth, tool has Read in heaven — no match
	minister := Permissions{Earth: Access{Read: true}}
	tool := Permissions{Heaven: Access{Read: true}}

	if minister.Match(tool) {
		t.Error("minister with earth Read should NOT match tool with heaven Read (different realm)")
	}
}

func TestPermissionsMatchMultiFlagOverlap(t *testing.T) {
	// Minister has Read+Write, tool has only Write — match on Write
	minister := Permissions{Earth: Access{Read: true, Write: true}}
	tool := Permissions{Earth: Access{Write: true}}

	if !minister.Match(tool) {
		t.Error("minister with earth Read+Write should match tool with earth Write (shared Write)")
	}
}

func TestPermissionsMatchAnyRealm(t *testing.T) {
	// Minister matches in Intent realm even though Earth and Heaven don't match
	minister := Permissions{
		Earth:  Access{Write: true},  // no overlap with tool's earth
		Heaven: Access{},             // no access
		Intent: Access{Execute: true}, // overlap with tool's intent
	}
	tool := Permissions{
		Earth:  Access{Read: true},
		Intent: Access{Execute: true},
	}

	if !minister.Match(tool) {
		t.Error("should match on intent Execute even though earth doesn't match")
	}
}

func TestPermissionsMatchNoOverlap(t *testing.T) {
	minister := Permissions{Earth: Access{Read: true}}
	tool := Permissions{Heaven: Access{Write: true}, Intent: Access{Execute: true}}

	if minister.Match(tool) {
		t.Error("no overlapping flags in any realm — should not match")
	}
}

// ---------------------------------------------------------------------------
// ToolRegistry — NewToolRegistry
// ---------------------------------------------------------------------------

func TestNewToolRegistry(t *testing.T) {
	r := NewToolRegistry()
	if r == nil {
		t.Fatal("NewToolRegistry returned nil")
	}
	if len(r.Tools()) != 0 {
		t.Error("new registry should have no public tools")
	}
	if len(r.PrivateTools("any")) != 0 {
		t.Error("new registry should have no private tools")
	}
}

// ---------------------------------------------------------------------------
// ToolRegistry — Register
// ---------------------------------------------------------------------------

func TestRegister(t *testing.T) {
	r := NewToolRegistry()
	perm, _ := ParsePermissions("rwxr---w-")
	r.Register(mockTool{name: "read_file"}, perm)

	tools := r.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Tool.Name() != "read_file" {
		t.Errorf("tool name = %q, want %q", tools[0].Tool.Name(), "read_file")
	}
	if tools[0].Permissions != perm {
		t.Errorf("permissions = %+v, want %+v", tools[0].Permissions, perm)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	r := NewToolRegistry()
	perm, _ := ParsePermissions("r-----rw-")
	r.Register(mockTool{name: "dup"}, perm)

	defer func() {
		if rec := recover(); rec == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()
	r.Register(mockTool{name: "dup"}, perm)
}

// ---------------------------------------------------------------------------
// ToolRegistry — Update
// ---------------------------------------------------------------------------

func TestUpdate(t *testing.T) {
	r := NewToolRegistry()
	perm, _ := ParsePermissions("r-----rw-")
	r.Register(mockTool{name: "read_file"}, perm)

	// Update with new instance (same name, different behavior)
	updatedTool := mockTool{name: "read_file"}
	r.Update(updatedTool)

	// Verify the tool is still registered
	tools := r.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool after update, got %d", len(tools))
	}
	if tools[0].Tool.Name() != "read_file" {
		t.Errorf("tool name = %q, want %q", tools[0].Tool.Name(), "read_file")
	}
	// Permissions should be preserved
	if tools[0].Permissions != perm {
		t.Errorf("permissions changed after update")
	}
}

func TestUpdateNonExistentNoOp(t *testing.T) {
	r := NewToolRegistry()
	perm, _ := ParsePermissions("r-----rw-")
	r.Register(mockTool{name: "existing"}, perm)

	// Update on non-existent tool should be no-op (no panic)
	r.Update(mockTool{name: "nonexistent"})

	tools := r.Tools()
	if len(tools) != 1 {
		t.Errorf("expected 1 tool after no-op update, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// ToolRegistry — RegisterPrivate
// ---------------------------------------------------------------------------

func TestRegisterPrivate(t *testing.T) {
	r := NewToolRegistry()
	r.RegisterPrivate("chancellor", mockTool{name: "invoke_minister"})
	r.RegisterPrivate("chancellor", mockTool{name: "enact_ritual"})

	privs := r.PrivateTools("chancellor")
	if len(privs) != 2 {
		t.Fatalf("expected 2 private tools, got %d", len(privs))
	}
	names := toolNames(privs)
	assertHas(t, names, "invoke_minister")
	assertHas(t, names, "enact_ritual")

	// Other ministers see no private tools
	if len(r.PrivateTools("forge")) != 0 {
		t.Error("forge should have no private tools")
	}
	if len(r.PrivateTools("judge")) != 0 {
		t.Error("judge should have no private tools")
	}
}

// ---------------------------------------------------------------------------
// ToolRegistry — ForPermissions with empty registry
// ---------------------------------------------------------------------------

func TestForPermissionsEmptyRegistry(t *testing.T) {
	r := NewToolRegistry()
	perm, _ := ParsePermissions("rwxrwxrwx")

	tools := r.ForPermissions("chancellor", perm)
	if len(tools) != 0 {
		t.Errorf("empty registry should return no tools, got %d", len(tools))
	}
}

// ---------------------------------------------------------------------------
// ToolRegistry — ForPermissions matching logic
// ---------------------------------------------------------------------------

func TestForPermissionsMatchByRead(t *testing.T) {
	r := NewToolRegistry()
	r.Register(mockTool{name: "read_file"}, Permissions{Earth: Access{Read: true}})

	// Minister with earth Read should see read_file
	perm := Permissions{Earth: Access{Read: true}}
	tools := r.ForPermissions("forge", perm)
	names := toolNames(tools)
	assertHas(t, names, "read_file")
}

func TestForPermissionsMatchByWrite(t *testing.T) {
	r := NewToolRegistry()
	r.Register(mockTool{name: "create_manifest"}, Permissions{Intent: Access{Write: true}})

	// Minister with intent Write should see create_manifest
	perm := Permissions{Intent: Access{Write: true}}
	tools := r.ForPermissions("forge", perm)
	names := toolNames(tools)
	assertHas(t, names, "create_manifest")
}

func TestForPermissionsMatchByExecute(t *testing.T) {
	r := NewToolRegistry()
	r.Register(mockTool{name: "run_shell_command"}, Permissions{Earth: Access{Execute: true}})

	// Minister with earth Execute should see run_shell_command
	perm := Permissions{Earth: Access{Execute: true}}
	tools := r.ForPermissions("forge", perm)
	names := toolNames(tools)
	assertHas(t, names, "run_shell_command")
}

func TestForPermissionsNoMatch(t *testing.T) {
	r := NewToolRegistry()
	r.Register(mockTool{name: "run_shell_command"}, Permissions{Earth: Access{Execute: true}})

	// Strategist: r-----rw- (no earth Execute)
	strategistPerm, _ := ParsePermissions("r-----rw-")
	tools := r.ForPermissions("strategist", strategistPerm)
	names := toolNames(tools)
	assertNotHas(t, names, "run_shell_command")
}

func TestForPermissionsMultiToolFiltering(t *testing.T) {
	r := NewToolRegistry()

	readFile := mockTool{name: "read_file"}
	writeFile := mockTool{name: "write_file"}
	shellCmd := mockTool{name: "run_shell_command"}
	createManifest := mockTool{name: "create_manifest"}

	r.Register(readFile, Permissions{Earth: Access{Read: true}})
	r.Register(writeFile, Permissions{Earth: Access{Read: true, Write: true}})
	r.Register(shellCmd, Permissions{Earth: Access{Execute: true}})
	r.Register(createManifest, Permissions{Intent: Access{Write: true}})

	// Strategist: r-----rw- (earth: r--, intent: rw-)
	strategistPerm, _ := ParsePermissions("r-----rw-")
	strategistTools := r.ForPermissions("strategist", strategistPerm)
	strategistNames := toolNames(strategistTools)
	assertHas(t, strategistNames, "read_file")          // earth Read matches
	assertHas(t, strategistNames, "write_file")         // earth Read matches (shared Read)
	assertHas(t, strategistNames, "create_manifest")   // intent Write matches
	assertNotHas(t, strategistNames, "run_shell_command") // strategist has no earth Execute

	// Forge: rwxr---w- (earth: rwx, intent: -w-)
	forgePerm, _ := ParsePermissions("rwxr---w-")
	forgeTools := r.ForPermissions("forge", forgePerm)
	forgeNames := toolNames(forgeTools)
	assertHas(t, forgeNames, "read_file")
	assertHas(t, forgeNames, "write_file")
	assertHas(t, forgeNames, "run_shell_command")
	assertHas(t, forgeNames, "create_manifest")
}

// ---------------------------------------------------------------------------
// ToolRegistry — ForPermissions with private tools
// ---------------------------------------------------------------------------

func TestForPermissionsWithPrivateTools(t *testing.T) {
	r := NewToolRegistry()
	r.Register(mockTool{name: "read_file"}, Permissions{Earth: Access{Read: true}})
	r.RegisterPrivate("chancellor", mockTool{name: "invoke_minister"})

	chancellorPerm, _ := ParsePermissions("rwxr--rwx")
	tools := r.ForPermissions("chancellor", chancellorPerm)
	names := toolNames(tools)
	assertHas(t, names, "read_file")
	assertHas(t, names, "invoke_minister")

	// Forge should NOT get chancellor's private tool
	forgePerm, _ := ParsePermissions("rwxr---w-")
	forgeTools := r.ForPermissions("forge", forgePerm)
	forgeNames := toolNames(forgeTools)
	assertHas(t, forgeNames, "read_file")
	assertNotHas(t, forgeNames, "invoke_minister")
}

func TestForPermissionsPrivatePrecedence(t *testing.T) {
	// If a private tool has the same name as a public tool,
	// the private one wins (no duplicate).
	r := NewToolRegistry()
	r.Register(mockTool{name: "shared_tool"}, Permissions{Earth: Access{Read: true}})
	r.RegisterPrivate("chancellor", mockTool{name: "shared_tool"})

	chancellorPerm, _ := ParsePermissions("r-----rw-")
	tools := r.ForPermissions("chancellor", chancellorPerm)

	count := 0
	for _, t := range tools {
		if t.Name() == "shared_tool" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 shared_tool (private wins), got %d", count)
	}
}

func TestForPermissionsMultiplePrivateTools(t *testing.T) {
	r := NewToolRegistry()
	r.RegisterPrivate("chancellor", mockTool{name: "invoke_minister"})
	r.RegisterPrivate("chancellor", mockTool{name: "enact_ritual"})

	perm, _ := ParsePermissions("rwxr--rwx")
	tools := r.ForPermissions("chancellor", perm)
	names := toolNames(tools)
	assertHas(t, names, "invoke_minister")
	assertHas(t, names, "enact_ritual")
}

func TestForPermissionsPrivateNotInPublicEntries(t *testing.T) {
	// Private tools are separate from public entries — PrivateTools returns a copy
	r := NewToolRegistry()
	r.RegisterPrivate("chancellor", mockTool{name: "secret"})

	// Public tools should be empty
	if len(r.Tools()) != 0 {
		t.Error("private tools should not appear in public Tools()")
	}
}

// ---------------------------------------------------------------------------
// Minister permission string verification — all six ministers
// ---------------------------------------------------------------------------

func TestChancellorPermissions(t *testing.T) {
	p, err := ParsePermissions("rwxr--rwx")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Earth: rwx — full access to source code
	if !p.Earth.Read || !p.Earth.Write || !p.Earth.Execute {
		t.Error("chancellor should have full earth access (rwx)")
	}
	// Heaven: r-- — read test results only
	if !p.Heaven.Read {
		t.Error("chancellor should have heaven Read")
	}
	if p.Heaven.Write || p.Heaven.Execute {
		t.Error("chancellor should NOT have heaven Write or Execute")
	}
	// Intent: rwx — full control of edicts, zhengming, rituals
	if !p.Intent.Read || !p.Intent.Write || !p.Intent.Execute {
		t.Error("chancellor should have full intent access (rwx)")
	}
}

func TestForgePermissions(t *testing.T) {
	p, err := ParsePermissions("rwxr---w-")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Earth: rwx — full access to source code
	if !p.Earth.Read || !p.Earth.Write || !p.Earth.Execute {
		t.Error("forge should have full earth access (rwx)")
	}
	// Heaven: r-- — read test results only
	if !p.Heaven.Read {
		t.Error("forge should have heaven Read")
	}
	if p.Heaven.Write || p.Heaven.Execute {
		t.Error("forge should NOT have heaven Write or Execute")
	}
	// Intent: -w- — write manifests only (no read/execute of intent)
	if p.Intent.Read || p.Intent.Execute {
		t.Error("forge should NOT have intent Read or Execute")
	}
	if !p.Intent.Write {
		t.Error("forge should have intent Write (create manifests)")
	}
}

func TestJudgePermissions(t *testing.T) {
	p, err := ParsePermissions("rwxrwxr--")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Earth: rwx — read, write, execute code
	if !p.Earth.Read || !p.Earth.Write || !p.Earth.Execute {
		t.Error("judge should have full earth access (rwx)")
	}
	// Heaven: rwx — full access to test results, run tests, write verdicts
	if !p.Heaven.Read || !p.Heaven.Write || !p.Heaven.Execute {
		t.Error("judge should have full heaven access (rwx)")
	}
	// Intent: r-- — read edicts, zhengming (no write/execute)
	if !p.Intent.Read {
		t.Error("judge should have intent Read")
	}
	if p.Intent.Write || p.Intent.Execute {
		t.Error("judge should NOT have intent Write or Execute")
	}
}

func TestSagePermissions(t *testing.T) {
	p, err := ParsePermissions("r--r--rwx")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Earth: r-- — read-only source code access
	if !p.Earth.Read {
		t.Error("sage should have earth Read")
	}
	if p.Earth.Write || p.Earth.Execute {
		t.Error("sage should NOT have earth Write or Execute")
	}
	// Heaven: r-- — read test results only
	if !p.Heaven.Read {
		t.Error("sage should have heaven Read")
	}
	if p.Heaven.Write || p.Heaven.Execute {
		t.Error("sage should NOT have heaven Write or Execute")
	}
	// Intent: rwx — suggest edicts, zhengming, record precedents
	if !p.Intent.Read || !p.Intent.Write || !p.Intent.Execute {
		t.Error("sage should have full intent access (rwx)")
	}
}

func TestStrategistPermissions(t *testing.T) {
	p, err := ParsePermissions("r-----rwx")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// Earth: r-- — read-only source code access
	if !p.Earth.Read {
		t.Error("strategist should have earth Read")
	}
	if p.Earth.Write || p.Earth.Execute {
		t.Error("strategist should NOT have earth Write or Execute")
	}
	// Heaven: --- — no access to test results
	if p.Heaven.Read || p.Heaven.Write || p.Heaven.Execute {
		t.Error("strategist should have NO heaven access")
	}
	// Intent: rwx — read/write lings + execute zhengming
	if !p.Intent.Read || !p.Intent.Write || !p.Intent.Execute {
		t.Error("strategist should have intent Read, Write, and Execute (request_zhengming)")
	}
}

// ---------------------------------------------------------------------------
// ToolRegistry.String — smoke test
// ---------------------------------------------------------------------------

func TestToolRegistryString(t *testing.T) {
	r := NewToolRegistry()
	r.Register(mockTool{name: "read_file"}, Permissions{Earth: Access{Read: true}})
	r.RegisterPrivate("chancellor", mockTool{name: "invoke_minister"})

	s := r.String()
	if s == "" {
		t.Error("String() should not be empty")
	}
	// Should mention the public tool with its permission string
	if !contains(s, "r--") || !contains(s, "read_file") {
		t.Errorf("String() should mention read_file with permission, got:\n%s", s)
	}
	// Should mention the private tool
	if !contains(s, "chancellor") || !contains(s, "invoke_minister") {
		t.Errorf("String() should mention chancellor private tool, got:\n%s", s)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func toolNames(tools []Tool) map[string]bool {
	m := make(map[string]bool)
	for _, t := range tools {
		m[t.Name()] = true
	}
	return m
}

func assertHas(t *testing.T, names map[string]bool, name string) {
	t.Helper()
	if !names[name] {
		t.Errorf("expected tool %q to be present", name)
	}
}

func assertNotHas(t *testing.T, names map[string]bool, name string) {
	t.Helper()
	if names[name] {
		t.Errorf("expected tool %q to NOT be present", name)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && stringContains(s, sub)))
}

func stringContains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
