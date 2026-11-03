---
paths:
  - "cmd/**/*.go"
---

# Layout of cmd/aigude

One file per command, holding the command and its functions. Shared helpers such as settings loading get their own file. `main.go` only wires the root command.

```
bad:  main.go         main, newRootCommand, newServeCommand, runServe,
                      newValidateCommand, runValidate, loadSettings, ...

good: main.go         main, newRootCommand
      serve.go        newServeCommand, runServe
      validate.go     newValidateCommand, runValidate
      settings.go     addSettingsFlags, loadSettings, settingsFile
```

## Checks of the environment

A binary on PATH, an API key and the like are checked in the command, as early as possible, so the process fails before anything is built. They are not checked in `pkg/`.

```go
// bad: found out in pkg/service.New, after settings and clients were built
// good: first statement of runServe
if !audio.Available() {
	return errors.New("ffmpeg was not found on PATH ...")
}
```
