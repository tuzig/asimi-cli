package tools

import (
	"fmt"
	"strings"
	"sync"
)

// Realm represents one of the three kingdoms (三界).
type Realm string

const (
	Earth  Realm = "earth"
	Heaven Realm = "heaven"
	Intent Realm = "intent"
)

// Access represents read/write/execute permissions within a single realm.
type Access struct {
	Read    bool
	Write   bool
	Execute bool
}

// Permissions holds per-realm access for a minister or a tool classification.
type Permissions struct {
	Earth  Access
	Heaven Access
	Intent Access
}

// ToolPermission pairs a tool with its permission classification.
type ToolPermission struct {
	Tool        Tool
	Permissions Permissions
}

// ParsePermissions parses a 9-character permission string of the form
// "rwxrwxrwx" where positions [0-2] = earth, [3-5] = heaven, [6-8] = intent.
// Each triple is (Read, Write, Execute); '-' means denied.
func ParsePermissions(s string) (Permissions, error) {
	if len(s) != 9 {
		return Permissions{}, fmt.Errorf("invalid permission string %q: must be 9 characters", s)
	}

	parse := func(rwx string) (Access, error) {
		if len(rwx) != 3 {
			return Access{}, fmt.Errorf("invalid rwx triple %q", rwx)
		}
		read := rwx[0] == 'r'
		write := rwx[1] == 'w'
		exec := rwx[2] == 'x'
		// Validate: each position is either the expected letter or '-'
		for i, ch := range rwx {
			expected := []byte{'r', 'w', 'x'}[i]
			if ch != '-' && ch != rune(expected) {
				return Access{}, fmt.Errorf("invalid character %q in position %d of %q", ch, i, rwx)
			}
		}
		return Access{Read: read, Write: write, Execute: exec}, nil
	}

	earth, err := parse(s[0:3])
	if err != nil {
		return Permissions{}, err
	}
	heaven, err := parse(s[3:6])
	if err != nil {
		return Permissions{}, err
	}
	intent, err := parse(s[6:9])
	if err != nil {
		return Permissions{}, err
	}

	return Permissions{Earth: earth, Heaven: heaven, Intent: intent}, nil
}

// accessMatch returns true when both accesses share at least one true flag.
func accessMatch(minister, tool Access) bool {
	return (minister.Read && tool.Read) ||
		(minister.Write && tool.Write) ||
		(minister.Execute && tool.Execute)
}

// Match returns true when the minister's permissions match the tool's
// permissions in at least one realm. A realm matches when both the
// minister and the tool have true for at least one shared access flag.
func (p Permissions) Match(tool Permissions) bool {
	return accessMatch(p.Earth, tool.Earth) ||
		accessMatch(p.Heaven, tool.Heaven) ||
		accessMatch(p.Intent, tool.Intent)
}

// String returns the 9-character permission string representation.
func (p Permissions) String() string {
	formatAccess := func(a Access) string {
		r, w, x := '-', '-', '-'
		if a.Read {
			r = 'r'
		}
		if a.Write {
			w = 'w'
		}
		if a.Execute {
			x = 'x'
		}
		return string([]byte{byte(r), byte(w), byte(x)})
	}
	return formatAccess(p.Earth) + formatAccess(p.Heaven) + formatAccess(p.Intent)
}

// ToolRegistry holds tool registrations with permission classifications
// and private tool mappings per minister.
type ToolRegistry struct {
	mu      sync.RWMutex
	entries map[string]ToolPermission // key = tool name
	private map[string][]Tool         // key = minister ID
}

// NewToolRegistry creates an empty tool registry.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		entries: make(map[string]ToolPermission),
		private: make(map[string][]Tool),
	}
}

// Register adds a tool with its permission classification to the registry.
// Panics if a tool with the same name is already registered.
func (r *ToolRegistry) Register(tool Tool, perm Permissions) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if _, exists := r.entries[name]; exists {
		panic(fmt.Sprintf("tool %q already registered", name))
	}
	r.entries[name] = ToolPermission{Tool: tool, Permissions: perm}
}

// Update replaces an existing tool registration with a new instance.
// If the tool is not registered, this is a no-op.
// This is useful when a tool's configuration changes after initial registration.
func (r *ToolRegistry) Update(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := tool.Name()
	if existing, ok := r.entries[name]; ok {
		r.entries[name] = ToolPermission{Tool: tool, Permissions: existing.Permissions}
	}
}

// RegisterPrivate adds a tool exclusively for a specific minister.
// Private tools are returned by ForPermissions in addition to
// any permission-matched public tools.
// TODO: Replace this with "extra_tools" array on the minister's yaml
func (r *ToolRegistry) RegisterPrivate(ministerID string, tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.private[ministerID] = append(r.private[ministerID], tool)
}

// ForPermissions returns all tools the given minister may access:
//   - public tools whose permissions match the minister's permissions
//   - private tools registered for the given ministerID
//
// Duplicates (by tool name) are removed; private tools take precedence.
func (r *ToolRegistry) ForPermissions(ministerID string, perm Permissions) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var result []Tool

	// Add private tools first (they take precedence on duplicates)
	for _, t := range r.private[ministerID] {
		name := t.Name()
		if !seen[name] {
			seen[name] = true
			result = append(result, t)
		}
	}

	// Add permission-matched public tools
	for name, tp := range r.entries {
		if seen[name] {
			continue // private tool already covers this name
		}
		if perm.Match(tp.Permissions) {
			seen[name] = true
			result = append(result, tp.Tool)
		}
	}

	return result
}

// Tools returns all publicly registered tools regardless of permissions.
// Useful for debugging and introspection.
func (r *ToolRegistry) Tools() []ToolPermission {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ToolPermission, 0, len(r.entries))
	for _, tp := range r.entries {
		result = append(result, tp)
	}
	return result
}

// PrivateTools returns all private tools registered for a given minister.
func (r *ToolRegistry) PrivateTools(ministerID string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return append([]Tool{}, r.private[ministerID]...)
}

// String returns a human-readable summary of the registry contents.
func (r *ToolRegistry) String() string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var b strings.Builder
	b.WriteString("ToolRegistry:\n")
	for name, tp := range r.entries {
		fmt.Fprintf(&b, "  %s  %s\n", tp.Permissions.String(), name)
	}
	if len(r.private) > 0 {
		b.WriteString("  private:\n")
		for ministerID, tools := range r.private {
			for _, t := range tools {
				fmt.Fprintf(&b, "    [%s] %s\n", ministerID, t.Name())
			}
		}
	}
	return b.String()
}
