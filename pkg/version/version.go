// Package version tells which version of AI Gude is running: the version of the Git tag it was released from, or, for
// any other build, "dev" and the Git commit it was built from.
package version

import "runtime/debug"

// tag is the version of the Git tag. The release build sets it (`-X` in .goreleaser.yaml); any other build is "dev".
var tag = "dev"

// current is what String returns.
var current = describe(tag, readBuildSettings())

// String returns the running version: "0.1.0" for a release, "dev+e2cf3b2" for a build inside a Git checkout ("-dirty"
// after the commit when it has uncommitted changes), and "dev" when the commit is not known, as for `go run` and tests.
func String() string {
	return current
}

// describe is the version for the tag; "dev" gets the commit of the build settings, if they have one.
func describe(tag string, settings []debug.BuildSetting) string {
	if tag != "dev" {
		return tag
	}
	var revision string
	var modified bool
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return "dev"
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if modified {
		revision += "-dirty"
	}
	return "dev+" + revision
}

// readBuildSettings returns what Go stored in the binary about its build, among it the Git commit of a `go build`
// inside a checkout.
func readBuildSettings() []debug.BuildSetting {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return nil
	}
	return info.Settings
}
