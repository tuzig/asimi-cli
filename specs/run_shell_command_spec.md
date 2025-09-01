# Engineering Specification: `run_shell_command` Tool

## 1. Overview

This document specifies the behavior, API, and execution environment of the
`run_shell_command` tool. This tool is made available to the Gemini model to
execute shell commands on the user's local system. The specification is written
from the perspective of the tool's consumer (the LLM).

## 2. Tool Definition (Function Declaration)

The `run_shell_command` tool is implemented as a `langchaingo.Tool` in Go.

```go
// RunShellCommand is a tool for running shell commands
type RunShellCommand struct{}

func (t RunShellCommand) Name() string {
	return "run_shell_command"
}

func (t RunShellCommand) Description() string {
	return "This tool executes a given shell command. On Windows, it uses `cmd.exe /c`. On other platforms, it uses `bash -c`. It can start background processes."
}

func (t RunShellCommand) Call(ctx context.Context, input string) (string, error) {
	var params RunShellCommandInput
	err := json.Unmarshal([]byte(input), &params)
	if err != nil {
		return "", fmt.Errorf("invalid input: %w", err)
	}

	// ... implementation to execute the command ...

	output := RunShellCommandOutput{
		// ... populate fields ...
	}

	outputBytes, err := json.Marshal(output)
	if err != nil {
		return "", fmt.Errorf("failed to marshal output: %w", err)
	}

	return string(outputBytes), nil
}

// RunShellCommandInput is the input for the RunShellCommand tool
type RunShellCommandInput struct {
	Command   string `json:"command"`
	Description string `json:"description"`
	Directory string `json:"directory"`
}
```

## 3. Parameters

### `command`
*   **Type:** `string`
*   **Required:** Yes
*   **Description:** The literal command string to be executed by the shell.
*   **Platform-specific shells:**
    *   **Windows:** The command is executed as `cmd.exe /c <command>`.
    *   **Linux/macOS:** The command is executed as `bash -c <command>`.
*   **Behavior:** The command string should be a valid command for the target platform's shell. It can include pipes, redirection, and command chaining (e.g., `&&`, `||`, `;`).

### `description`
*   **Type:** `string`
*   **Required:** No
*   **Description:** A concise, human-readable explanation of what the command intends to do. This description will be shown to the user in a confirmation dialog if the command is not already on a pre-approved list. It is crucial for obtaining user consent for potentially impactful operations.

### `directory`
*   **Type:** `string`
*   **Required:** No
*   **Description:** The directory where the command should be executed, relative to the project's root. If omitted, the command runs in the project's root directory. Absolute paths are not permitted.

## 4. Return Schema

The tool call will return a single JSON object containing the results of the execution. This is achieved by marshaling the following Go struct into a JSON string:

```go
// RunShellCommandOutput is the output of the RunShellCommand tool
type RunShellCommandOutput struct {
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
	ExitCode       int    `json:"exitCode"`
	Signal         int    `json:"signal,omitempty"`
	Error          string `json:"error,omitempty"`
	PID            int    `json:"pid,omitempty"`
	BackgroundPids []int  `json:"backgroundPids,omitempty"`
}
```

### Fields:
*   **`stdout`:** The complete standard output of the command as a string.
*   **`stderr`:** The complete standard error of the command as a string.
*   **`exitCode`:** The integer exit code of the process. `0` typically indicates success. `null` if the process was terminated by a signal.
*   **`signal`:** The numeric signal that terminated the process (e.g., `15` for `SIGTERM`), if applicable. `null` otherwise.
*   **`error`:** A string containing an error message if the tool failed to spawn the process itself (e.g., command not found). `null` if the process spawned successfully, even if it later exited with an error.
*   **`pid`:** The process ID of the main command process.
*   **`backgroundPids`:** An array of process IDs for any background tasks spawned by the command. This is not supported on Windows and will be an empty array.

## 5. Execution Environment

*   **Shell:** As noted, commands are wrapped in either `bash -c` or `cmd.exe /c`. This means shell-specific syntax and features are available.
*   **Working Directory:** The command's working directory is resolved from the `directory` parameter, relative to the project root.
*   **Environment Variables:** The executed command inherits the environment variables of the Gemini CLI process, with one addition:
    *   `ASIMI_CLI=1`: This variable is set, allowing scripts to detect if they are being run from within the Gemini CLI.

## 6. Security and Execution Control

The LLM should be aware that not all `run_shell_command` calls will be executed. The execution is subject to the following controls:

*   **Configuration-based Filtering:**
    *   **Denylist (`excludeTools`):** The user can configure a list of commands that are explicitly forbidden (e.g., `run_shell_command(rm)`). Any attempt to run a denied command will fail.
    *   **Allowlist (`coreTools`):** The user can configure a strict list of allowed commands (e.g., `run_shell_command(git)`, `run_shell_command(npm)`). If this is configured, any command *not* on the list will be rejected or require confirmation.
*   **User Confirmation:** If a command is not explicitly allowed by the configuration or a previous approval in the current session, the CLI will pause and prompt the user for confirmation. The `description` parameter is vital for this step. If the user denies the request, the tool call will fail.

## 7. Key Behaviors & Usage Patterns

*   **Background Processes:**
    *   On **Linux/macOS**, a command can be run in the background by appending `&`. The `backgroundPids` array in the return value will be populated.
    *   On **Windows**, background processes can be launched using `start /b`. The `backgroundPids` array will not be populated.
*   **Statelessness:** Each `run_shell_command` call is independent. Changes to the environment (like `cd` or setting a shell variable) in one call will **not** persist to the next.
*   **Command Substitution:** For security reasons, command substitution patterns like `$(...)`, `<(...)`, `>(...)`, and backticks (`` `...` ``) are **disallowed** and will cause the tool call to fail before execution.
*   **Error Handling:** A non-zero `exitCode` or a non-empty `stderr` should be interpreted as a potential failure. The LLM should analyze these fields to understand the outcome of the command.
