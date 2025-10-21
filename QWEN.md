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

## Release Management

- We follow SemVER
- We keep a CHANGELOG.md where each version has a section with subsections for all user notable changes broken into: Fixed, Changed & Added
- We use git tags to sync the code and the changelog

## Kanaban

The tasks are spread in 2 places:
1. in progress work - `git worktree list`
1. the `Unreleased` section of CHANGELOG.md 
2. github issues, available using `gh` cli

Our tasks can be in one of 3 bins: To Be Planned aka TBP, To Be Implemented aka TBI and Done. TBI are pretty simple. They start with a git worktree of a new branch and ends when the user approves the change.
Commits are then squashed and if it's a notable change the changelog is updated: A new line is added to the `Fixed`, `Changed` or `Added` sections of `Unreleased` version. 

IMPORTANT: Keep the changelog top notch accurately and succinctly covering all user notable changes

When a task is in progress it should have a git worktree with all the changes. When working on a branch we are liberal in our commits as they will be squashed after approval.
