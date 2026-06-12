# Init Command Flow

## Overview

The init command analyzes a codebase and generates infrastructure files for working with Asimi. It creates:
- **AGENTS.md** - Build commands, test commands, and code style guidelines
- **Justfile** - Task runner with common development tasks
- **.agents/asimi.conf** - Project-specific configuration
- **.agents/sandbox/Dockerfile** - Container image for sandboxed shell execution
- **.agents/sandbox/bashrc** - Bash configuration for the container

These files become part of the system prompt for all future AI sessions in the project.

## Trigger Points

The init command can be triggered via:
- Command mode: `:init` (normal initialization)
- Command mode: `:init clear` (remove and regenerate all files)

## Command Execution Flow

### 1. Entry Point: `handleInitCommand`

Located in `commands.go`, this function:
- Validates that a session exists (requires `:login` first)
- Checks for `clear` argument to determine mode
- Creates `.agents/sandbox` directory structure
- In clear mode: removes all infrastructure files first

### 2. Embedded File Initialization

Two files are written directly from embedded content:
- **.agents/asimi.conf** - From `config.DefaultConfContent()` (embedded in `internal/config/default.conf`)
- **.agents/sandbox/bashrc** - From `dotagents/sandbox/bashrc` embed

These are always written in clear mode, or if they don't exist.

### 3. Missing File Detection

`checkMissingInfraFiles()` checks for:
- AGENTS.md
- Justfile
- .agents/asimi.conf
- .agents/sandbox/Dockerfile
- .agents/sandbox/bashrc

If all files exist and not in clear mode, initialization is skipped with a message suggesting `:init clear`.

### 4. Project Name Extraction

From `GetRepoInfo().Slug`:
- Full slug example: `github.com/owner/repo`
- Extracted project name: `repo` (last segment after `/`)
- Used for container naming: `asimi-sandbox-repo:latest`

### 5. Template Preparation

`InitTemplateData` structure contains:
- `ProjectName` - Extracted project name for container naming
- `ProjectSlug` - Full repository slug
- `MissingFiles` - List of files that need creation
- `ClearMode` - Whether running in clear mode

The template (`prompts/init.tmpl`) is parsed and executed with this data.

### 6. Shell Runner Switching

Before sending the prompt:
- Captures the current (container) shell runner
- Switches to host shell runner for init execution
- This prevents container issues during infrastructure setup
- Original runner is passed to verification for testing

### 7. AI Prompt Execution

Sends `startConversationMsg` with:
- `prompt` - Rendered template with project-specific data
- `clearHistory: true` - Starts fresh conversation
- `RunOnHost: true` - Forces host shell execution
- `onStreamComplete` - Callback to `verifyInit` after completion

## AI Analysis Phase

The AI agent (via the template prompt) is instructed to:

1. **Explore the codebase** using tools:
   - Read package manifests (package.json, go.mod, Cargo.toml, etc.)
   - List directory structure
   - Analyze existing scripts and build systems
   - Check for existing infrastructure files

2. **Generate/Update Files**:

   **Justfile:**
   - Starts with `PROJECT_NAME := "{{.ProjectSlug}}"`
   - Common recipes: install, test, run, lint, build, clean, bootstrap
   - Transpiles existing scripts to just recipes
   - Ends with `build-sandbox` and `clean-sandbox` recipes using `{{PROJECT_NAME}}`

   **AGENTS.md:**
   - Language specification
   - Build commands using just
   - Test commands (all tests + single test)
   - Lint/format commands
   - Code style guidelines (imports, formatting, types, naming, errors)
   - Project-specific conventions
   - Note about container configuration

   **.agents/asimi.conf:**
   - `[run_shell_command]` section at top
   - `image_name = "localhost/asimi-sandbox-{{.ProjectName}}:latest"`
   - Ensures project-specific container is used

   **.agents/sandbox/Dockerfile:**
   - Base image: project's primary language runtime (over debian)
   - Common tools: git, curl, just, ripgrep
   - No vim, no user creation, no WORKDIR
   - Ends with: `COPY .agents/sandbox/bashrc /root/.bashrc` and `CMD ["/bin/bash"]`

3. **Write files** without waiting for approval (guardrails will verify)

## Verification Phase: `verifyInit`

After the AI completes, automatic verification runs with retry logic (max 5 attempts).

### Verification Steps

1. **Configuration Reload**
   - Reloads `.agents/asimi.conf` to pick up any LLM changes
   - Ensures latest configuration is used

2. **File Existence Checks**
   - Verifies AGENTS.md exists
   - Verifies Justfile exists
   - Reports failures and triggers retry if missing

3. **Build Sandbox Container**
   - Runs `just build-sandbox` on host
   - Timeout: 30 seconds
   - Builds the project-specific container image
   - Reports success/failure with exit code

4. **Shell Runner Reinitialization**
   - After successful build, reinitializes shell runner
   - Gets fresh container with newly built image
   - Critical for subsequent container tests

5. **Smoke Test in Container**
   - Runs `uname` in the container
   - Verifies output contains "Linux"
   - Confirms container is functional
   - Timeout: 30 seconds

6. **Host Tests**
   - Runs `just test` on host
   - Timeout: 30 seconds
   - Validates recipes work on host system

7. **Container Tests**
   - Runs `just test` in container
   - Timeout: 30 seconds
   - Validates recipes work in sandboxed environment

### Retry Logic: `verifyInitWithRetry`

On verification failure:
1. Closes the container (if exists)
2. Builds error message with all failures
3. Sends message back to AI session
4. AI attempts to fix the issues
5. Verification runs again (up to 5 total attempts)
6. After max retries, reports failure to user

### Success Path

When all verifications pass:
1. Stages files with git:
   - `git add AGENTS.md`
   - `git add Justfile`
   - `git add .agents/`
2. Reports success message
3. Suggests reviewing with `:!just` or starting fresh with `:new`

## Data Flow Diagram

```
User: :init [clear]
    ↓
handleInitCommand
    ├─ Create .agents/sandbox/
    ├─ [clear mode] Remove all infrastructure files
    ├─ Write embedded files (asimi.conf, bashrc)
    ├─ Check missing files
    ├─ Extract project name from RepoInfo.Slug
    ├─ Prepare InitTemplateData
    ├─ Parse & execute init.tmpl
    ├─ Capture container shell runner
    └─ Switch to host shell runner
    ↓
startConversationMsg (RunOnHost: true)
    ↓
AI Analysis & File Generation
    ├─ Explore codebase (Read, List, Glob tools)
    ├─ Analyze build system & conventions
    ├─ Generate/Update Justfile
    ├─ Generate/Update AGENTS.md
    ├─ Generate/Update .agents/asimi.conf
    └─ Generate/Update .agents/sandbox/Dockerfile
    ↓
onStreamComplete → verifyInit
    ↓
verifyInitWithRetry (attempt 1-5)
    ├─ Reload configuration
    ├─ Check AGENTS.md exists
    ├─ Check Justfile exists
    ├─ Run: just build-sandbox (host)
    ├─ Reinitialize shell runner
    ├─ Run: uname (container smoke test)
    ├─ Run: just test (host)
    └─ Run: just test (container)
    ↓
    ├─ [FAIL] → Close container
    │           Build error message
    │           Send to AI for fixes
    │           Retry (if < max attempts)
    │
    └─ [PASS] → Stage files with git
                Report success
                Suggest next steps
```

## Key Design Principles

1. **Template-Driven**: Uses Go templates for flexible, project-specific prompts
2. **Project-Specific Containers**: Each project gets its own named container image
3. **Automatic Verification**: Guardrails validate all generated files
4. **Self-Healing**: AI automatically fixes issues up to 5 retry attempts
5. **Host Execution**: Init runs on host to avoid container bootstrapping issues
6. **Embedded Defaults**: Simple config files embedded in binary for reliability
7. **Git Integration**: Automatically stages successful infrastructure files
8. **Clear Mode**: Allows complete regeneration when needed

## Configuration Impact

After init completes, the project has:

**Local Configuration** (`.agents/asimi.conf`):
```toml
[run_shell_command]
image_name = "localhost/asimi-sandbox-{project-name}:latest"
```

**Build Recipe** (Justfile):
```just
PROJECT_NAME := "{project-slug}"

build-sandbox:
    podman machine init --disk-size 30 || true
    podman machine start || true
    podman build -t localhost/asimi-sandbox-{{PROJECT_NAME}}:latest -f .agents/sandbox/Dockerfile .
```

This ensures:
- Each project uses its own container
- Container name matches project
- Multiple projects can coexist
- Configuration is version-controlled

## Subsequent Usage

Once initialized:
- All future AI sessions automatically load AGENTS.md in system prompt
- Shell commands run in project-specific container
- No need to re-explain build/test/style guidelines
- Container persists across sessions (until rebuilt)

## Error Handling

**No Session**: "No model connection. Use :login to configure a provider and start chatting."

**Directory Creation Fails**: "Error creating .agents directory: {error}"

**Template Parse Error**: "Error parsing initialization template: {error}"

**Max Retries Exceeded**: 
```
❌ Initialization failed after 5 attempts.
The following issues could not be resolved:
{list of failures}

Please review the errors and try running ':init' again, or manually fix the issues.
```

**Container Close Failure**: Logged as warning, doesn't block retry

## Related Files

- `commands.go` - Command handlers and verification logic
- `prompts/init.tmpl` - AI prompt template
- `internal/config/default.conf` - Embedded default config
- `dotagents/sandbox/bashrc` - Embedded bashrc
- `context.go` - `GetRepoInfo()` for project detection
- `podman_runner.go` - Container shell runner
- `host_shell_runner.go` - Host shell runner
