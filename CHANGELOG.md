# Changelog

All [Semantic Versions](https://semver.org/spec/v2.0.0.html) of this project and their notable changes are documented in the file. 

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), with 
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).


## [Unreleased]

### To be Planned
- Adding the "task" tool for sub agent tasks. supports both `role` and `model` for asimi. Add the 
- the asimi system prompt, support multi roles, including "Assistant", "Coder", "Tester" and reviewer

### To be Implemented
- TODO list: adding a todo list and the todo write tool. branch `todo`

### Implementing
### Done
- Move the log file in `~/.local/share/asimi/log` and prevent it from exploding by adding `gopkg.in/natefinch/lumberjack.v2`
- Fix the /new command so it'll clear the context and not just the screen
