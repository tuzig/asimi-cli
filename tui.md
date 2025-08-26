
## TUI Architecture Specification

Asimi's TUI is built using the `bubbletea` framework, which adheres to The Elm Architecture (TEA). This architecture is composed of three core parts: **Model**, **View**, and **Update**.

### 1. Model

The `Model` represents the entire state of the TUI application at any given moment. It is a single source of truth from which the UI is rendered. The state is primarily composed of two major structs: `Model` for the UI-specific state and `App` for the core application logic and data.

#### `Model` (UI State)

This is the root model for the `bubbletea` program. It holds the state related to the terminal UI itself.

*   **`app *App`**: An instance of the core application state. This is the most significant part of the model, containing all non-UI data.
*   **`width, height int`**: The current dimensions of the terminal window.
*   **UI Components (Sub-Models)**: The TUI is composed of several stateful components, each with its own model:
    *   **`status StatusComponent`**: The model for the bottom status bar, which displays the current agent, working directory, and Git branch.
    *   **`editor EditorComponent`**: The model for the user input text area, managing text, attachments, cursor position, and input history.
    *   **`messages MessagesComponent`**: The model for the main chat view, managing a viewport for scrolling through the conversation history.
    *   **`fileViewer *fileviewer.Model`**: The model for displaying file content or diffs in a dedicated pane.
    *   **`completions CompletionDialog`**: The model for the autocompletion pop-up that appears for slash commands (`/`) or file references (`@`).
    *   **`toastManager *ToastManager`**: Manages the state of transient toast notifications.
*   **`modal Modal`**: A field to hold the currently active modal dialog (e.g., Help, Sessions, Models). When `nil`, no modal is active.
*   **UI Flags & State**:
    *   **`showCompletionDialog bool`**: A flag to determine if the completion dialog should be visible.
    *   **`interruptKeyState InterruptKeyState`**: Tracks the state for debouncing the interrupt command (`esc`).
    *   **`exitKeyState ExitKeyState`**: Tracks the state for debouncing the exit command (`ctrl+c`).
    *   **`messagesRight bool`**: A flag to toggle the layout of the messages pane.

#### `App` (Core Application State)

This struct is embedded within the `Model` and holds the core data and logic, independent of the UI rendering.

*   **`Info AppInfo`**: Static information about the application environment, including file paths.
*   **`Config *Config`**: The user's application configuration, including keybindings and theme settings.
*   **`llm *llms.Model`**: The HTTP client for communicating with the OpenCode server.
*   **`State *State`**: The persistent TUI state loaded from a file, including the selected theme, agent, and recently used models.
*   **Session & Conversation**:
    *   **`Session *Session`**: The currently active chat session.
    *   **`Messages []Message`**: The list of all messages (user and assistant) in the current session. Each message contains structured parts like text, tool calls, and reasoning steps.
*   **Agents & Models**:
    *   **`Agents []Agent`**: A list of all available agents.
    *   **`Providers []Provider`**: A list of available model providers (e.g., OpenAI, Anthropic).
    *   **`AgentIndex int`**: The index of the currently selected agent.
*   **Permissions & Commands**:
    *   **`Permissions []Permission`**: A queue of pending permission requests from the agent that require user approval.
    *   **`Commands CommandRegistry`**: A registry of all built-in slash commands.

### 2. View

The `View` function is responsible for rendering the application's `Model` into a string, which `bubbletea` then displays in the terminal. The rendering is deterministic; for a given state, the view will always be the same.

The root `View` function is `View()` from @main.go.  It constructs the UI by composing the views of its sub-components.

*   **Overall Layout**: The UI is divided into two main layouts:
    1.  **Home View** (when `app.Session.ID` is empty): Displays a welcome screen with the OpenCode logo and a list of helpful commands. The editor is centered.
    2.  **Chat View** (when a session is active): A more traditional chat interface.
*   **Component Composition**:
    *   The main layout is a vertical stack: `Messages` pane on top, `Editor` at the bottom.
    *   **`messagesView()`**: Renders the scrollable conversation history. It iterates through `app.Messages` and renders each user prompt and assistant response. It handles complex rendering for tool calls, reasoning steps, diffs, and markdown.
    *   **`editorView()`**: Renders the user input text area, including the prompt, placeholder text, attachments, and cursor.
    *   **`statusView()`**: Renders the status bar at the very bottom of the screen, which is always visible.
*   **Conditional Rendering**:
    *   **Modals**: If `modal` is not `nil`, its `Render()` method is called to draw it as an overlay on top of the main view.
    *   **Completions**: If `showCompletionDialog` is true, the `completionsView()` is rendered as an overlay just above or below the editor.
    *   **File Viewer**: If a file is active in `app.fileViewer`, its view is rendered, often splitting the screen with the messages pane.
    *   **Toasts**: The `toastView()` method draws notifications on top of the final view.
*   **Styling**: All styling is handled by `lipgloss` and driven by the currently active `theme`. The `theme.CurrentTheme()` function provides the color palette for all components.

### 3. Update

The `Update` function is the state transition logic. It takes a message (`tea.Msg`) and the current `Model` and returns a new `Model` and a command (`tea.Cmd`) to be executed. Messages are the only way to change the application's state.

The root `Update` function is in @main.go, It acts as a central dispatcher, routing messages to the appropriate sub-components or handling global events.

#### Message Sources:

1.  **User Input**: `tea.KeyPressMsg`, `tea.MouseMsg`.
2.  **Terminal Events**: `tea.WindowSizeMsg`, `tea.BackgroundColorMsg`.
3.  **Backend Events**: `Event...` messages received from `app.llm`. These drive the real-time updates of the assistant's response.
5.  **Commands (`tea.Cmd`)**: Functions that perform I/O (like file access) and return a `tea.Msg` on completion.

#### Key State Transitions:

*   **User Typing**: `tea.KeyPressMsg` with printable characters are sent to the `editorUpdate()` function, which updates its internal text buffer.
*   **Command Execution**:
    *   A `/` keypress triggers the `CompletionDialog` with the `CommandCompletionProvider`.
    *   Selecting a command or pressing `enter` on a command-like input triggers a `commands.ExecuteCommandMsg`.
    *   The `executeCommand()` function handles this message, performing actions like opening a dialog (`model_list`), creating a new session (`session_new`), or sending a prompt (`input_submit`).
*   **Sending a Prompt**:
    *   The `editorSubmit()` method creates an `app.SendPrompt` message.
    *   `SendPrompt()` handles this, making an API call to the backend. This is a `tea.Cmd` that will eventually result in `Event...` messages coming back from the server.
*   **Receiving Assistant Responses**:
    *   As `Event...` messages arrive, they are dispatched by the root `Update` function.
    *   `EventMessagePartUpdated` messages update the `app.Messages` slice with new text, tool calls, or reasoning steps.
    *   This change in the triggers a re-render of the `messagesView()` on the next `View()` call, showing the response streaming in.
*   **Handling Permissions**:
    *   An `EventPermissionUpdated` message adds a permission request to the `app.Permissions` queue.
    *   The UI displays the request, and a `tea.KeyPressMsg` ('enter', 'a', or 'esc') triggers an API call to respond to the permission.
*   **Switching Models/Agents/Themes**:
    *   User selects from a dialog (e.g., `ModelDialog`).
    *   The dialog sends a message like `ModelSelectedMsg`.
    *   The root `Update` function catches this, updates the `app.App` state (`app.Provider`, `app.Model`), and saves the choice to the persistent `app.State`.
