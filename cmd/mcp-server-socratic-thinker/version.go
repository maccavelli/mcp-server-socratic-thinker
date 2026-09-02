package main

import (
	"fmt"
	"os"
	"strings"
)

// releaseBuildKind is the only stamped value that marks a release build.
const releaseBuildKind = "release"

// RawVersion is the stamped release identity. It defaults to "dev" so a binary
// built outside a tag release can never be mistaken for one: the previous
// default was a hard-coded "v4.4.4" that outranked every real tag, which would
// have made self-update believe it was permanently up to date.
var RawVersion = "dev"

// RawBuildKind is "release" only for a tag build. A bool cannot be set with
// the Go linker's -X flag, so this is a string and only that exact value
// counts; everything else is a local build that update refuses to replace
// without --force.
var RawBuildKind = "local"

// Version is the trimmed display value only. Ordering uses RawVersion.
var Version = strings.TrimPrefix(RawVersion, "v")

func printVersion() {
	fmt.Fprintf(os.Stderr, "mcp-server-socratic-thinker version %s\n", Version)
}
