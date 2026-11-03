---
paths:
  - "**/*.go"
---

# Parameters and constructors

## Mandatory parameters are plain parameters

Mandatory parameters are never fields of a struct. Only optional parameters go into an `Opts` struct; its zero value means "not set".

```go
// bad: system, style and contents are all required, but hidden in a struct
WriteScript(request ScriptRequest) (Script, error)

// good
WriteScript(system, style string, contents []Content) (Script, error)

// good: Limit and Since are optional
List(opts ListOpts) ([]Episode, error)
```

## Rely on the constructor contract

A mandatory parameter is not checked again: no `nil` or empty checks, and no godoc that repeats the contract. If a value may be empty, it is an optional parameter in an `Opts` struct.

```go
// bad: the constructor guards its own contract
func New(s Settings, writer ScriptWriter) (*Service, error) {
	if writer == nil {
		return nil, errors.New("no script writer")
	}

// good: the caller passes a writer, the constructor uses it
func New(s Settings, writer ScriptWriter) (*Service, error) {
```

## Results are returned, not written through a pointer

What a function produces comes back as a return value, also next to an error. A pointer parameter that the function adds to is not used for it; the caller combines the values.

```go
// bad: speak adds to the caller's Usage
func (e *Episodes) speak(script Script, usage *Usage) (joinedSpeech, error)

// good: like collect and GenerateScript, the Usage is returned, also when the call failed
func (e *Episodes) speak(script Script) (joinedSpeech, Usage, error)
episode.Usage = episode.Usage.Add(usage)
```
