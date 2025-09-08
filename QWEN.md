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
[] Add a theme - a collection of functions and colors that render given text, editing highlights, borders, etc. Example functions: RenderAI, RenderUser, RenderTool
[] Handle the <thought> llm output tag
[] Add the local time at the bottom right of the status line

[] Add a theme - a collection of functions that render given text, editing highlights, borders, etc. Example functions: RenderAI, RenderUser, RenderTool
[] Handle the <thought> llm output tag as toast messages
[] Add the local time at the bottom right of the status line
