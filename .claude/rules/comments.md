---
paths:
  - "**/*.go"
---

# Comments

Comments say what is. They do not say what is not, what was, or what the author doubts.

```go
// bad: states a doubt instead of what the type is
// Not sure if that makes sense if we use langchain
type LLM interface { SinglePrompt(prompt string) (string, error) }

// good
// LLM turns a prompt into text.
type LLM interface { SinglePrompt(prompt string) (string, error) }
```
