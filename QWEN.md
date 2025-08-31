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

## TODOs
[x] Add a pre-commit hook to `just test`
[x] Add E2E tests for @<file> including auto completion and asserating the file content is included. The TUI is probably borken so fix ALL the errors that conme up 
[x] Add E2e tests for slash commands including auto completion. The TUI is probably borken so fix ALL the errors that conme up .
[x] @specs/todo.md Please complete the specification by Adding a section: "integration" detailing all the required changes in the existing code base
[] file inclusion shouldn't add the file content to the user's prompt on the UI. Embedd only when sending to the LLM
[] Change completion to use the up & down arrows and the tab or enter to select and esc to abort
