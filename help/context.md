# Context and Token Usage

Asimi tracks token usage to help you stay within model limits and manage
conversation context effectively.

## Viewing Context

  :context         - Show detailed context information

The context view shows:
  - Current token usage
  - Maximum token limit
  - Percentage used
  - Number of messages
  - Loaded files and their sizes

## Token Limits

Different models have different context limits:
  - Claude Sonnet: 200K tokens
  - GPT-4: 128K tokens
  - Gemini Pro: 1M tokens

Asimi automatically tracks usage and warns when approaching limits.

## Managing Context

When context gets too large:

1. Start a new session:
   :new

2. Export current conversation:
   :export

3. Reference only essential files:
   Use @ selectively

4. Use shorter prompts:
   Be concise in your requests

## Context Files

Files referenced with @ are loaded into context:
  @main.go         - Adds main.go to context

View loaded files:
  :context         - Shows all files in context

## Token Counting

Asimi counts tokens for:
  - Your prompts
  - AI responses
  - System messages
  - File contents
  - Tool calls and results

## Best Practices

1. Monitor context usage regularly with :context
2. Start fresh sessions for new tasks
3. Reference files only when needed
4. Export important conversations before starting new sessions
