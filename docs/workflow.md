# Workflow Framework Guide

The Asimi workflow framework provides a way to build multi-step, AI-driven operations with automatic retry, persistence, and progress tracking. This guide explains how to create custom workflows.

## Overview

A workflow consists of:
- **Steps**: Sequential operations with prepare, prompt, and verify phases
- **Data**: Shared state that accumulates across steps
- **Navigation**: Control flow via verify return values
- **Persistence**: SQLite storage for resume after interruption

## Core Concepts

### Step Structure

Each step has three optional phases:

```go
Step{
    Name: "step-name",
    Prepare: func(w *Workflow) (map[string]interface{}, error) {
        // Gather data, set up state
        // Return data for template rendering
    },
    Prompt: "Go template string with {{.Variables}}",
    Verify: func(w *Workflow, response string) StepResult {
        // Check results, determine next action
        // response contains AI output (empty if no Prompt)
    },
}
```

### Navigation Control

The `StepResult` controls workflow navigation using either step names or relative offsets:

#### Using Step Names (Recommended)

Use `w.GoTo(stepName, message)` for explicit, maintainable navigation:

```go
// Jump to a specific step by name
return w.GoTo("build-step", "Prerequisites met, starting build")

// This works for forward jumps, backward jumps, or retrying the same step
return w.GoTo("validation", "Going back to re-validate")
```

#### Using Relative Offsets (Legacy)

The `NextOffset` field provides relative navigation:

| Value | Meaning |
|-------|---------|
| `+1` | Proceed to next step |
| `+n` | Skip n-1 steps (jump forward) |
| `0` | Retry current step |
| `-1` | Go back one step |
| `-n` | Go back n steps |

### StepResult Helpers

All StepResult helpers are methods on `*Workflow`. They automatically call `ReportProgress()` to notify the user of progress updates:

```go
// Proceed to next step
return w.Next("✓ Analysis complete")

// Retry current step
return w.Retry("No response, retrying")

// Jump to a specific step by name (recommended for non-sequential flow)
return w.GoTo("build-step", "Skipping to build")

// Skip forward n steps
return w.Skip(2, "Skipping optional steps")

// Go back one step (deprecated: use GoTo for clarity)
return w.Back("Dependency not met, going back")

// Go back n steps (deprecated: use GoTo for clarity)
return w.BackN(2, "Going back two steps")
```

### Workflow Data

Data is shared across steps using `map[string]interface{}`:

```go
// Store any type of value
w.Set("key", "string value")
w.Set("files", []string{"a.go", "b.go"})
w.Set("count", 42)
w.Set("enabled", true)

// Retrieve string values
value := w.Get("key") // Returns "" if not found or not a string

// Retrieve any value
files := w.GetValue("files").([]string)
```

### Progress Reporting

Steps can report ad-hoc progress messages during execution:

```go
w.Add(Step{
    Name: "long-operation",
    Verify: func(w *Workflow, response string) StepResult {
        w.ReportProgress("Starting download...")
        // ... do work ...
        w.ReportProgress("Processing files...")
        // ... do more work ...
        return w.Next("✓ Done")
    },
})
```

The messages are sent via the `onProgress` callback with a non-empty message parameter. Note that all StepResult helpers (`Next`, `Retry`, `GoTo`, etc.) also call `ReportProgress()` automatically.

## Creating a Workflow

### Basic Example

```go
func NewMyWorkflow(db *storage.DB, repoInfo RepoInfo) *Workflow {
    return NewWorkflow("my-workflow", db, repoInfo, WithMaxRetries(3)).
        Add(Step{
            Name: "check-prereqs",
            Prepare: func(w *Workflow) (map[string]interface{}, error) {
                if _, err := exec.LookPath("go"); err != nil {
                    return nil, fmt.Errorf("go not found in PATH")
                }
                return nil, nil
            },
            Verify: func(w *Workflow, response string) StepResult {
                return w.Next("✓ Prerequisites met")
            },
        }).
        Add(Step{
            Name: "analyze",
            Prepare: func(w *Workflow) (map[string]interface{}, error) {
                files, _ := filepath.Glob("*.go")
                return map[string]interface{}{
                    "GoFiles": files,
                    "Count":   len(files),
                }, nil
            },
            Prompt: `Analyze these {{.Count}} Go files:
{{range .GoFiles}}- {{.}}
{{end}}
Suggest improvements.`,
            Verify: func(w *Workflow, response string) StepResult {
                if response == "" {
                    return w.Retry("No response, retrying")
                }
                w.Set("analysis", response)
                return w.Next("✓ Analysis complete")
            },
        })
}
```

### Using Common Step Helpers

For common patterns, use the built-in step helpers. All helpers return `*Workflow` for method chaining:

```go
func NewDeployWorkflow(db *storage.DB, repoInfo RepoInfo) *Workflow {
    return NewWorkflow("deploy", db, repoInfo).
        AddGate("check-clean", func(w *Workflow) bool {
            output, _ := exec.Command("git", "status", "--porcelain").Output()
            return len(output) == 0
        }, "Working directory must be clean").
        AddCmd("test", "go test ./...", "test_output").
        AddCmd("build", "go build -o dist/app ./...", "").
        AddPrompt("review", `Review the test output: {{.test_output}}`).
        AddConfirm("confirm", "Deploy to production?").
        AddCmd("deploy", "./scripts/deploy.sh", "")
}
```

### Running a Workflow

```go
// Create workflow
w := NewMyWorkflow(db, repoInfo)

// Set up AI prompt handler
w.sendPrompt = func(ctx context.Context, prompt string) <-chan string {
    ch := make(chan string, 1)
    go func() {
        response, _ := session.Ask(ctx, prompt)
        ch <- response
        close(ch)
    }()
    return ch
}

// Set up progress callback
w.onProgress = func(stepIndex int, state StepState, message string) {
    if message != "" {
        fmt.Println("Progress:", message)
    } else {
        fmt.Printf("Step %d (%s): %s\n", stepIndex, state.Name, state.Status)
    }
}

// Run
ctx := context.Background()
err := w.Run(ctx)
```

## Common Step Helpers

### Add

Adds a full step definition (for complex steps):

```go
w.Add(Step{
    Name:    "complex-step",
    Prepare: func(w *Workflow) (map[string]interface{}, error) { ... },
    Prompt:  "...",
    Verify:  func(w *Workflow, response string) StepResult { ... },
})
```

### AddPrompt

Sends a prompt to the AI and proceeds on any response:

```go
w.AddPrompt("analyze", "Analyze the codebase and suggest improvements.")
```

### AddCmd

Runs a shell command and optionally stores the output:

```go
// Run command, store output in "build_output"
w.AddCmd("build", "go build ./...", "build_output")

// Run command, don't store output (empty string)
w.AddCmd("clean", "rm -rf ./dist", "")
```

The step retries if the command returns a non-zero exit code.

### AddGate

Blocks workflow until a condition is met:

```go
w.AddGate("wait-for-ci", func(w *Workflow) bool {
    return checkCIStatus() == "passed"
}, "Waiting for CI to pass")
```

### AddConfirm

Requires user confirmation to proceed:

```go
w.AddConfirm("confirm", "Apply these changes?")
```

If the user declines, the step is marked as skipped and the workflow proceeds.

### AddIf

Wraps a step to only execute when a condition is true:

```go
w.AddIf(
    func(w *Workflow) bool { return w.Get("runTests") == "true" },
    Step{
        Name: "run-tests",
        // ... full step definition
    },
)
```

### AddRun

Executes a function and proceeds on success (retries on error):

```go
w.AddRun("setup", func(w *Workflow) error {
    if err := os.MkdirAll(".config", 0755); err != nil {
        return err
    }
    w.Set("configDir", ".config")
    return nil
})
```

### AddCheck

Adds a step with only a verify function (for custom verification logic):

```go
w.AddCheck("verify-build", func(w *Workflow) StepResult {
    if _, err := os.Stat("dist/app"); os.IsNotExist(err) {
        return w.Retry("Build artifact not found")
    }
    return w.Next("✓ Build verified")
})
```

## Step Patterns

### Skip Step Conditionally

```go
w.AddIf(
    func(w *Workflow) bool { return w.Get("skipOptional") != "true" },
    Step{Name: "optional-step", ...},
)
```

### Retry with Backoff

```go
w.Add(Step{
    Name: "flaky-operation",
    Verify: func(w *Workflow, response string) StepResult {
        result, err := doFlakyThing()
        if err != nil {
            states := w.GetStepStates()
            retries := states[w.CurrentStep].RetryCount
            return w.Retry(fmt.Sprintf("Failed (attempt %d): %v", retries+1, err))
        }
        return w.Next("Success")
    },
})
```

### Go Back on Failure

```go
w.Add(Step{
    Name: "dependent-step",
    Verify: func(w *Workflow, response string) StepResult {
        if !checkDependency() {
            // Use GoTo for explicit navigation to a named step
            return w.GoTo("setup-dependency", "Dependency not met, going back to setup")
        }
        return w.Next("Done")
    },
})
```

## Template Syntax

Prompts use Go's `text/template` package:

```go
Prompt: `
{{if .ClearMode}}Starting fresh...{{end}}

Files to process:
{{range .Files}}- {{.}}
{{end}}

Project: {{.ProjectName}}
Count: {{.Count}}
`
```

Data from `Prepare` and accumulated workflow `Data` are available in templates.

## Workflow Options

```go
// Set maximum retries per step (default: 3)
NewWorkflow("name", db, repoInfo, WithMaxRetries(5))

// Set AI prompt handler
NewWorkflow("name", db, repoInfo, WithSendPrompt(promptFunc))

// Set progress callback (for step state changes and ad-hoc messages)
// The message parameter is non-empty for ReportProgress calls, empty for state changes
NewWorkflow("name", db, repoInfo, WithOnProgress(func(stepIndex int, state StepState, message string) {
    if message != "" {
        fmt.Println("Progress:", message)
    } else {
        fmt.Printf("Step %d (%s): %s\n", stepIndex, state.Name, state.Status)
    }
}))

// Set yes/no approval handler
NewWorkflow("name", db, repoInfo, WithRequestYesNo(yesNoFunc))

// Combine options
NewWorkflow("name", db, repoInfo,
    WithMaxRetries(5),
    WithOnProgress(progressFunc),
)
```

## Persistence

Workflows are automatically saved to SQLite after each step. To resume:

```go
// Load existing workflow
w, err := LoadWorkflow(db, workflowID)

// List active (pending/running) workflows
workflows, err := ListActiveWorkflows(db, repoInfo)

// Continue execution
err = w.Run(ctx)
```

## Error Handling

### Prepare Errors

If `Prepare` returns an error, the step fails and the workflow stops:

```go
Prepare: func(w *Workflow) (map[string]interface{}, error) {
    if criticalError {
        return nil, fmt.Errorf("cannot proceed: %v", err)
    }
    return nil, nil
}
```

### Verify Failures

Use `w.Retry()` to retry, or return an error message:

```go
Verify: func(w *Workflow, response string) StepResult {
    if failed {
        return w.Retry("❌ Step failed: " + reason)
    }
    return w.Next("✓ Success")
}
```

### Abort Workflow

```go
// From within a step
Verify: func(w *Workflow, response string) StepResult {
    if criticalFailure {
        w.Abort()
    }
    return w.Next("")
}

// From outside
w.Abort()
```

## Best Practices

1. **Idempotent Steps**: Design steps to be safely re-runnable
2. **Clear Messages**: Use emoji prefixes (✓, ❌, ⚠️) for status clarity
3. **Granular Steps**: Prefer many small steps over few large ones
4. **Use Helpers**: Prefer `AddPrompt`, `AddCmd`, etc. for common patterns
5. **Method Chaining**: Use fluent API for cleaner workflow definitions
6. **Data Validation**: Validate data in `Prepare` before proceeding
7. **Timeout Contexts**: Use `context.WithTimeout` for external operations

## Example: Lint Fix Workflow

A workflow that fixes lint errors one at a time with fresh AI context:

```go
func NewLintFixWorkflow(db *storage.DB, repoInfo RepoInfo, errors []LintError) *Workflow {
    w := NewWorkflow("lint-fix", db, repoInfo, WithMaxRetries(3))
    
    for i, lintErr := range errors {
        err := lintErr // Capture for closure
        w.Add(Step{
            Name: fmt.Sprintf("fix-%d", i),
            Prepare: func(w *Workflow) (map[string]interface{}, error) {
                return map[string]interface{}{
                    "File":    err.File,
                    "Line":    err.Line,
                    "Message": err.Message,
                }, nil
            },
            Prompt: `Fix this lint error:
File: {{.File}}
Line: {{.Line}}
Error: {{.Message}}

Read the file, understand the context, and fix the error.`,
            Verify: func(w *Workflow, response string) StepResult {
                if stillHasError(err.File, err.Line) {
                    return w.Retry("Error not fixed")
                }
                return w.Next("✓ Fixed")
            },
        })
    }
    
    return w
}
```

Each step gets fresh AI context, preventing context explosion with many errors.

## Database Schema

Workflows are stored in two tables:

```sql
-- Workflow metadata
CREATE TABLE workflows (
    id TEXT PRIMARY KEY,
    branch_id INTEGER NOT NULL,
    name TEXT NOT NULL,
    current_step INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'pending',
    max_retries INTEGER NOT NULL DEFAULT 3,
    data TEXT NOT NULL DEFAULT '{}',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Step states
CREATE TABLE workflow_steps (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    workflow_id TEXT NOT NULL,
    step_index INTEGER NOT NULL,
    name TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    retry_count INTEGER NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT ''
);
```

States: `pending`, `running`, `completed`, `failed`, `aborted`, `skipped`
