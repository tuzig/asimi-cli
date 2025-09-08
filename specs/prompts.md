
## Engineering Specification: Dynamic Prompt Construction System

### 1. Overview

This document specifies the logic for dynamically constructing system prompts for Asimi CLI agent. The system is designed to be configurable and context-aware, tailoring the agent's instructions based on the user's environment and memory.

The primary component is the `getCoreSystemPrompt` function, which assembles the main operational prompt. A secondary component, `getCompressionPrompt`, provides instructions for summarizing conversation history.

### 2. Core Component: `getCoreSystemPrompt` Function

This function constructs the main system prompt by orchestrating several stages of content assembly.

#### 2.1. Prompt Assembly Stages

The final prompt is constructed in the following sequence:

1.  **Base Prompt Determination**: A foundational prompt template is selected.
2.  **Dynamic Content Injection**: Context-specific sections (Sandbox and Git status) are conditionally appended.
3.  **User Memory Appending**: User-specific memories are appended to provide long-term context.

#### 2.2. Stage 1: Base Prompt Determination

The system first determines the base prompt. This can be either the default hardcoded prompt or a custom prompt loaded from an external file. This behavior is controlled by the `ASSIMI_SYSTEM_MD` environment variable.

**Logic Flow:**

1.  **Check `ASIMI_SYSTEM_MD`**:
    *   If the variable is **unset**, **`'0'`**, or **`'false'`** (case-insensitive), the default, hardcoded system prompt is used.
    *   If the variable is set to any other value, the system attempts to load a custom prompt from a Markdown file.

2.  **Custom Prompt Path Resolution**:
    *   If `ASSIMI_SYSTEM_MD` is **`'1'`** or **`'true'`** (case-insensitive), the system looks for the prompt at a default path: `~/.gemini/system.md`.
    *   If `ASSIMI_SYSTEM_MD` is set to a **custom path string**, that path is used.
        *   The system correctly resolves `~` to the user's home directory.
        *   Relative paths are resolved relative to the current working directory.
    *   **Error Handling**: If a custom prompt is enabled but the specified file does not exist, the application will throw an error and terminate.

#### 2.3. Stage 2: Dynamic Content Injection

After establishing the base prompt, the system appends specialized sections based on the current execution environment.

**2.3.2. Git Repository Block**

The system checks if the current working directory is a Git repository.

*   **Condition**: `isGitRepository(process.cwd())` returns `true`.
    *   **Action**: Appends the `# Git Repository` section, providing detailed instructions for interacting with Git, including gathering status, reviewing diffs, matching commit message style, and avoiding `git push` unless explicitly asked.

*   **Condition**: The CWD is not a Git repository.
    *   **Action**: No content is appended.

**2.3.3. Project-Specific Context**

The system can be configured to inject project-specific context into the prompt. This is useful for providing the agent with project conventions, commands, and ongoing tasks.

*   **Condition**: A project-specific context file is found (e.g., `AGENTS.md` in the project root).
    *   **Action**: Appends the content of the context file to the prompt, wrapped in markers indicating the source file.
*   **Condition**: No project-specific context file is found.
    *   **Action**: No content is appended.

For the Asimi CLI project, the following context would be appended from `AGENTS.md`:

--- Context from: AGENTS.md ---
# Asimi CLI Project Context

This file provides context for the Asimi CLI project, a CLI coding agent written in Go.
Our mission is to have a tool that is safe, fun and produces high quality code

IMPORTANT: Keep the directory tree flat. Try and add your changes to existing files and if that does not makes sense - create as little new files as possible and always get your consent when creating directories and files

- to test: `just test`
- to build:`just build`
- to update dependecies: `just modules`
- use present progressive in commit messages

## Libraries
- bubbletea for the UI
- koanf for configuration management
- kong for CLI
- langchaingo for llm communications, tools, chains and more

--- End of Context from: AGENTS.md ---

#### 2.4. Stage 3: User Memory Appending

Finally, the system appends personalized user facts and preferences.

*   **Input**: An optional `userMemory` string argument.
*   **Logic**:
    *   If `userMemory` is provided and is not empty after trimming whitespace, it is appended to the prompt.
    *   The memory content is preceded by a `---` separator to clearly delineate it from the main system prompt.
    *   If `userMemory` is null or empty, nothing is appended.

### 3. Auxiliary Feature: Prompt Exporting

The system includes a feature to write the generated base prompt to a file, which is useful for customization and debugging.

*   **Control Variable**: `ASSIMI_WRITE_SYSTEM_MD`
*   **Logic**:
    *   If the variable is **unset**, **`'0'`**, or **`'false'`**, this feature is disabled.
    *   If the variable is **`'1'`** or **`'true'`**, the generated base prompt is written to the default path (`~/.gemini/system.md`).
    *   If the variable is set to a **custom path**, the prompt is written to that path. Directory structures are created recursively if they do not exist.

### 4. History Compression Component: `getCompressionPrompt`

This function provides a static, specialized prompt for a different task: conversation history compression.

*   **Purpose**: To instruct the LLM to act as a state manager and distill a long conversation into a structured XML format. This snapshot becomes the agent's sole memory for subsequent turns.
*   **Structure**: The prompt commands the model to produce a `<state_snapshot>` XML object containing the following mandatory child elements:
    *   `<overall_goal>`: The user's high-level objective.
    *   `<key_knowledge>`: Crucial facts, conventions, and constraints.
    *   `<file_system_state>`: A summary of file operations and learnings.
    *   `<recent_actions>`: A factual summary of the last few tool uses and outcomes.
    *   `<current_plan>`: A step-by-step plan with status markers (`DONE`, `IN PROGRESS`, `TODO`).
*   **Process**: The prompt instructs the model to first reason within a `<scratchpad>` before generating the final XML output.

### 5. Implementation Details

The code will be utilize go templates to generate the prompt. 

#### 1. Go Data Structure

First, you would define a Go struct to hold the dynamic data that the template will need. This struct serves as the context for the template execution.

```go
package main

// PromptContext holds all the dynamic data needed to render the system prompt.
type PromptContext struct {
	// IsGitRepository is true if the current working directory is a git repository.
	IsGitRepository bool
	// SandboxStatus indicates the type of sandbox environment.
	// Possible values: "macos", "generic", "none".
	SandboxStatus string
	// UserMemory contains user-specific facts and preferences.
	UserMemory string
	// ProjectContext contains project-specific context.
	ProjectContext string
	// ProjectContextFile is the name of the file from which project context was loaded.
	ProjectContextFile string

	// ToolNames contains the names of the available tools used as placeholders in the template.
	ToolNames struct {
		ReadFile      string
		WriteFile     string
		Grep          string
		Glob          string
		Edit          string
		Shell         string
		ReadManyFiles string
		Memory        string
		LS            string
	}
}
```

### 2. Go Template for Dynamic Sections

The template includes all the logic for conditionally adding sections like the sandbox status, Git repository information, and user memory. It uses placeholders for tool names, which will be populated from the `PromptContext` struct.

You would load your base prompt from a file or a string and then append the output of executing a per-model template. Please start with a naive one.



