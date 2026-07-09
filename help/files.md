# File Operations

Asimi provides several ways to work with files in your project.

## File References with @

Use @ to reference files in your prompts. This loads the file content into
the conversation context.

  @filename         - Reference a file
  @path/to/file     - Reference file in subdirectory

When you type @, a completion dialog appears showing available files:
  ↓/↑              - Navigate through files
  Enter            - Select file
  ESC              - Cancel

Example:
  Can you review @main.go and suggest improvements?

## File Completion

The file completion dialog shows:
  - All files in your project
  - Filtered by your search query
  - Sorted by relevance

Type to filter:
  @mai             - Shows files matching "mai" (e.g., main.go)
  @src/            - Shows files in src/ directory

## Context Management

Files you reference are added to the conversation context. Use :context to
see what's currently in context:

  :context         - Show context usage and loaded files

## File Tools

Asimi has built-in tools for file operations:
  - read_file      - Read file contents
  - write_file     - Write or update files
  - glob           - Find files by pattern

These tools are used automatically by the AI when needed.

## Best Practices

1. Reference only the files you need for the current task
2. Use :context to monitor token usage
3. Start a :new session if context gets too large
4. Use specific file paths to avoid ambiguity
