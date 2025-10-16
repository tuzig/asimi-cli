# Changelog

All [Semantic Versions](https://semver.org/spec/v2.0.0.html) of this project and their notable changes are documented in the file. 

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), with 
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [Unreleased]

### To be Planned

### To be Implemented
- Adding the "task" internal tool in `tools.go`. See `specs/task_tool.md` for detailed implementation plan
- Add a `/resume` command that lists last X sessions and let's the user choose which session to resume. See `specs/resume_command.md` for detailed implementation plan
- Gracefully handle HTTP 429 errors from the model. See `specs/http_429_handling.md` for detailed implementation plan

### Implementing
### Done
- Bug fix: when the user scrolls the chat window stop autoscrolling
- Feature: display thinking
- replace the color scheme with Terminal7's colors:

```css
:root {
    --prompt-border: #F952F9;
    --chat-border: #F4DB53;
    --text-color: #01FAFA;
    --warning: #F4DB53;
    --error: #F54545;
    --prompt-background: #271D30;
    --chat-background: #11051E;
    --text-error: #004444;
    --pane-background: black;
    --dark-border: #373702;
}
```

- bug fix: Status bar should not overflow so complete refactoring:
-- left: 🪾<branch_name> main branch should be collored yellow, green otherwise
-- mid: <shorten git status> e.g, `[$!?]`
-- right: <provider status icon><shorten provider and model. e.g. "Claude-4"
- 
- Support touch gesture for scrolling the chat
- bug fix: status bar shows distinct labels for Claude 4 and Claude 4.5 models
- Move the log file in `~/.local/share/asimi/log` and prevent it from exploding by adding `gopkg.in/natefinch/lumberjack.v2`
- Fix the /new command so it'll clear the context and not just the screen
- Implement the `/context` command with token breakdown, bar visualization, and command output per `specs/context_command.md`
- Integrate langchaingo's model context size database for more accurate OpenAI model support
- Use langchaingo's tiktoken-based token counting for OpenAI models when available
