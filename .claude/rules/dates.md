---
paths:
  - "**/*.go"
---

# Dates

A date that goes into a prompt, a log or a file name is ISO 8601: `2026-10-29`, formatted with `time.DateOnly`. Localized, spoken dates are the language model's job.

```go
// bad
fmt.Sprintf("%s, der %d. %s %d", germanDays[t.Weekday()], t.Day(), germanMonths[t.Month()-1], t.Year())

// good
t.Format(time.DateOnly)
```
