# HTTP 429 Error Handling Specification

## Overview
Implement graceful handling of HTTP 429 (Too Many Requests) rate limit errors from LLM provider APIs. This ensures the application responds intelligently to rate limiting rather than failing abruptly.

## Status
**Planning** - Moving from "To be Planned" to implementation phase

## Motivation
LLM APIs implement rate limiting to prevent abuse and ensure fair usage. When users hit these limits, the application should:
- Inform the user clearly about the rate limit
- Automatically retry with exponential backoff when appropriate
- Respect Retry-After headers from the API
- Provide actionable guidance to the user
- Maintain conversation state without data loss
- Track rate limit occurrences for debugging

This prevents poor user experience from cryptic error messages and unexpected failures.

## Requirements

### Functional Requirements

1. **Error Detection**
   - Detect HTTP 429 responses from all supported providers (Anthropic, OpenAI, Google AI)
   - Parse rate limit information from response headers
   - Distinguish between different rate limit types (requests/min, tokens/min, tokens/day)

2. **Retry Logic**
   - Implement exponential backoff with jitter
   - Respect `Retry-After` header when present
   - Configurable max retry attempts (default: 3)
   - Configurable base delay (default: 1 second)
   - Maximum delay cap (default: 60 seconds)

3. **User Communication**
   - Display clear, user-friendly error messages
   - Show retry countdown when retrying automatically
   - Inform user about rate limit type and when it will reset (if known)
   - Provide suggestions (e.g., "Try a smaller model", "Wait X minutes")
   - Option to cancel retry and return to prompt

4. **State Management**
   - Preserve user's message in history
   - Don't count failed attempts against conversation
   - Allow user to retry manually if auto-retry fails
   - Log rate limit events for debugging

### Non-Functional Requirements

1. **Reliability**
   - Don't lose user input on rate limit errors
   - Handle concurrent rate limits gracefully
   - Work correctly with streaming responses

2. **User Experience**
   - Clear, non-technical error messages
   - Show progress during retry wait
   - Don't block UI during retry delays
   - Maintain responsive interface

3. **Configuration**
   - Allow disabling auto-retry
   - Configurable retry parameters
   - Per-provider settings if needed

## Design

### Error Response Structures

**Anthropic API**
```
HTTP/1.1 429 Too Many Requests
retry-after: 60
x-ratelimit-limit-requests: 1000
x-ratelimit-remaining-requests: 0
x-ratelimit-reset-requests: 2024-10-16T15:30:00Z

{
  "type": "error",
  "error": {
    "type": "rate_limit_error",
    "message": "Rate limit exceeded"
  }
}
```

**OpenAI API**
```
HTTP/1.1 429 Too Many Requests  
retry-after: 30
x-ratelimit-limit-requests: 500
x-ratelimit-remaining-requests: 0

{
  "error": {
    "message": "Rate limit reached for requests",
    "type": "rate_limit_error",
    "code": "rate_limit_exceeded"
  }
}
```

**Google AI API**
```
HTTP/1.1 429 Too Many Requests
{
  "error": {
    "code": 429,
    "message": "Resource has been exhausted (e.g. check quota).",
    "status": "RESOURCE_EXHAUSTED"
  }
}
```

### Data Structures

```go
// RateLimitError represents a rate limit error with retry information
type RateLimitError struct {
    Provider     string
    RetryAfter   time.Duration  // From Retry-After header
    LimitType    string         // "requests", "tokens", "daily_quota"
    ResetTime    time.Time      // When the limit resets (if known)
    OriginalErr  error
}

// RateLimitConfig holds configuration for rate limit handling
type RateLimitConfig struct {
    EnableAutoRetry   bool          `koanf:"enable_auto_retry"`
    MaxRetries        int           `koanf:"max_retries"`
    BaseDelay         time.Duration `koanf:"base_delay_ms"`
    MaxDelay          time.Duration `koanf:"max_delay_ms"`
    JitterFactor      float64       `koanf:"jitter_factor"`
    ShowRetryProgress bool          `koanf:"show_retry_progress"`
}

// RetryState tracks the current retry attempt
type RetryState struct {
    Attempt       int
    NextRetryAt   time.Time
    Cancelled     bool
}
```

### Implementation Plan

#### Phase 1: Error Detection & Parsing
1. **HTTP Error Interceptor**
   - Add middleware to detect 429 responses
   - Works with all HTTP clients (Anthropic, OpenAI, Google)
   - Extract rate limit headers
   - Parse provider-specific error bodies

2. **RateLimitError Type**
   - Implement `RateLimitError` struct
   - Add method to format user-friendly message
   - Parse `Retry-After` header (supports both seconds and HTTP-date)
   - Extract reset time from provider-specific headers

3. **Provider-Specific Handlers**
   ```go
   func parseAnthropicRateLimit(resp *http.Response) (*RateLimitError, error)
   func parseOpenAIRateLimit(resp *http.Response) (*RateLimitError, error)
   func parseGoogleAIRateLimit(resp *http.Response) (*RateLimitError, error)
   ```

#### Phase 2: Retry Logic
1. **Exponential Backoff Implementation**
   ```go
   func calculateRetryDelay(attempt int, config RateLimitConfig, retryAfter time.Duration) time.Duration {
       // If server specified retry-after, use that
       if retryAfter > 0 {
           return retryAfter
       }
       
       // Otherwise, exponential backoff: baseDelay * 2^attempt
       delay := config.BaseDelay * time.Duration(math.Pow(2, float64(attempt)))
       
       // Cap at max delay
       if delay > config.MaxDelay {
           delay = config.MaxDelay
       }
       
       // Add jitter: ±10% randomness
       jitter := time.Duration(float64(delay) * config.JitterFactor * (rand.Float64()*2 - 1))
       delay += jitter
       
       return delay
   }
   ```

2. **Retry Loop**
   ```go
   func (s *Session) callWithRetry(ctx context.Context, fn func() error) error {
       config := s.config.RateLimit
       
       for attempt := 0; attempt <= config.MaxRetries; attempt++ {
           err := fn()
           
           if err == nil {
               return nil // Success
           }
           
           // Check if it's a rate limit error
           rateLimitErr, ok := err.(*RateLimitError)
           if !ok {
               return err // Not a rate limit error, don't retry
           }
           
           if attempt == config.MaxRetries {
               return fmt.Errorf("max retries exceeded: %w", err)
           }
           
           // Calculate delay
           delay := calculateRetryDelay(attempt, config, rateLimitErr.RetryAfter)
           
           // Notify user
           s.notify(RetryStartMsg{
               Attempt:     attempt + 1,
               MaxAttempts: config.MaxRetries + 1,
               Delay:       delay,
               ResetTime:   rateLimitErr.ResetTime,
           })
           
           // Wait with cancellation support
           select {
           case <-time.After(delay):
               continue
           case <-ctx.Done():
               return ctx.Err()
           }
       }
       
       return fmt.Errorf("retry loop ended unexpectedly")
   }
   ```

#### Phase 3: User Interface
1. **Error Message Types**
   ```go
   type RateLimitErrorMsg struct {
       Provider  string
       LimitType string
       ResetTime time.Time
       Retrying  bool
   }
   
   type RetryStartMsg struct {
       Attempt     int
       MaxAttempts int
       Delay       time.Duration
       ResetTime   time.Time
   }
   
   type RetryProgressMsg struct {
       TimeRemaining time.Duration
   }
   
   type RetrySuccessMsg struct{}
   
   type RetryFailedMsg struct {
       Reason string
   }
   ```

2. **TUI Display**
   - Show rate limit error in chat
   - Display countdown timer during retry wait
   - Allow user to cancel retry (Ctrl+C or Esc)
   - Show success message when retry succeeds

3. **Error Message Formatting**
   ```go
   func formatRateLimitError(err *RateLimitError) string {
       var msg strings.Builder
       
       msg.WriteString("⚠ Rate limit reached")
       
       if err.LimitType != "" {
           msg.WriteString(fmt.Sprintf(" (%s)", err.LimitType))
       }
       
       if !err.ResetTime.IsZero() {
           duration := time.Until(err.ResetTime)
           msg.WriteString(fmt.Sprintf("\nLimit resets in %s", formatDuration(duration)))
       }
       
       msg.WriteString("\n\nSuggestions:")
       msg.WriteString("\n• Wait a moment and try again")
       msg.WriteString("\n• Use a different model (try /models)")
       msg.WriteString("\n• Check your API quota on the provider's website")
       
       return msg.String()
   }
   ```

#### Phase 4: Configuration & Integration
1. **Add Configuration**
   ```toml
   [rate_limit]
   enable_auto_retry = true
   max_retries = 3
   base_delay_ms = 1000
   max_delay_ms = 60000
   jitter_factor = 0.1
   show_retry_progress = true
   ```

2. **Integrate with Session**
   - Wrap LLM calls in retry logic
   - Works with both streaming and non-streaming
   - Preserve message state across retries

3. **Logging**
   - Log rate limit occurrences
   - Track retry attempts
   - Monitor retry success/failure rates

#### Phase 5: Testing
1. **Unit Tests**
   - Test exponential backoff calculation
   - Test Retry-After header parsing
   - Test provider-specific error parsing
   - Test retry loop with mock failures

2. **Integration Tests**
   - Mock 429 responses from providers
   - Test retry success after N attempts
   - Test max retries exceeded
   - Test cancellation during retry

3. **Manual Testing**
   - Trigger rate limit with actual API
   - Verify retry behavior
   - Test UI feedback
   - Test cancellation

### Example User Experience

**Scenario 1: Auto-retry succeeds**
```
User: Help me refactor this code

[System makes API call, receives 429]

⚠ Rate limit reached (requests per minute)
   Limit resets in 45 seconds

   Retrying automatically (attempt 1/3)...
   ⏳ Waiting 2 seconds...
   
[After delay, retry succeeds]

✓ Retry successful

[Assistant response appears normally]
```

**Scenario 2: Max retries exceeded**
```
User: Help me refactor this code

⚠ Rate limit reached (daily token quota)
   Limit resets in 4 hours 23 minutes

   Retrying automatically (attempt 1/3)...
   ⏳ Waiting 1 second...
   
   Still rate limited. Retrying (attempt 2/3)...
   ⏳ Waiting 2 seconds...
   
   Still rate limited. Retrying (attempt 3/3)...
   ⏳ Waiting 4 seconds...

✗ Max retries exceeded

Suggestions:
• Wait 4 hours 23 minutes for your quota to reset
• Switch to a different model with /models
• Check your API usage on the provider dashboard

Your message has been saved. You can try again later.
```

**Scenario 3: User cancels retry**
```
User: Help me refactor this code

⚠ Rate limit reached (requests per minute)
   Limit resets in 30 seconds

   Retrying automatically (attempt 1/3)...
   ⏳ Waiting 2 seconds... (Press Esc to cancel)

[User presses Esc]

⚠ Retry cancelled

You can try again when the rate limit resets.
```

## Configuration

Add to `config.go`:
```go
type RateLimitConfig struct {
    EnableAutoRetry   bool `koanf:"enable_auto_retry"`
    MaxRetries        int  `koanf:"max_retries"`
    BaseDelayMs       int  `koanf:"base_delay_ms"`
    MaxDelayMs        int  `koanf:"max_delay_ms"`
    JitterFactor      float64 `koanf:"jitter_factor"`
    ShowRetryProgress bool `koanf:"show_retry_progress"`
}
```

Add to default config:
```toml
[rate_limit]
enable_auto_retry = true
max_retries = 3
base_delay_ms = 1000
max_delay_ms = 60000
jitter_factor = 0.1
show_retry_progress = true
```

## Testing Strategy

### Unit Tests
1. Retry delay calculation (exponential backoff + jitter)
2. Retry-After header parsing (seconds and HTTP-date formats)
3. Provider-specific error parsing
4. Retry loop logic
5. Cancellation handling

### Integration Tests
1. Mock 429 response → successful retry
2. Mock 429 response → max retries exceeded
3. Retry-After header respected
4. User cancellation during retry
5. Streaming request with rate limit

### Manual Tests
1. Trigger actual rate limit with API
2. Verify retry countdown display
3. Test cancellation UX
4. Verify error messages are clear
5. Test with different providers

## Dependencies
- HTTP client configuration
- Session error handling
- TUI message system
- Configuration system
- Logging infrastructure

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Infinite retry loops | High | Strict max retry limit, timeout |
| Poor user experience with long waits | Medium | Show clear progress, allow cancellation |
| Retry during streaming breaks state | Medium | Test thoroughly, handle partial responses |
| Different provider APIs behave differently | Medium | Provider-specific handlers, test all |
| User confusion about rate limits | Low | Clear messaging, actionable suggestions |

## Success Criteria
- [ ] 429 errors detected and parsed correctly for all providers
- [ ] Automatic retry with exponential backoff works
- [ ] Retry-After header is respected
- [ ] User can cancel retry
- [ ] Clear error messages displayed
- [ ] Conversation state preserved across retries
- [ ] Unit test coverage >80%
- [ ] Manual testing with all providers successful
- [ ] Documentation complete

## Future Enhancements
1. **Rate Limit Dashboard**: Show current usage against limits
2. **Predictive Warnings**: Warn user when approaching limits
3. **Smart Model Switching**: Automatically suggest switching to model with available quota
4. **Usage Analytics**: Track and display API usage patterns
5. **Quota Pooling**: Share quota across multiple sessions (if provider supports)
6. **Local Rate Limiting**: Prevent hitting API limits proactively

## References
- [Anthropic API Rate Limits](https://docs.anthropic.com/claude/reference/rate-limits)
- [OpenAI API Rate Limits](https://platform.openai.com/docs/guides/rate-limits)
- [Google AI Rate Limits](https://ai.google.dev/docs/rate_limits)
- HTTP Retry patterns and best practices
- Exponential backoff algorithm
- Session error handling (`session.go`)
- TUI message system (`tui.go`)
