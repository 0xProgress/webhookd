// Command webhookd is the entry point for the webhookd binary.
//
// webhookd listens for webhook HTTP requests, verifies their
// signatures, and writes one JSONL line per verified event to stdout.
// The CLI surface and provider dispatch live in the cmd package; this
// file exists only to inject the build version and pass the exit code
// from cobra to the operating system.
package main

import (
	"os"

	"github.com/0xProgress/webhookd/cmd"
)

// version is the build version.
//
// The linker injects it via -X main.version=... The Makefile's build
// target sets it to `git describe --tags --always`; GoReleaser sets it
// to the release tag. When neither is used — a plain `go build` with
// no ldflags — the value stays "dev", and both `webhookd --version`
// and the startup banner report "dev".
//
// This variable is the sole target of the -X main.version linker flag
// in the entire codebase. Do not rename it without updating the
// Makefile and .goreleaser.yaml in the same change.
var version = "dev"

func main() {
	os.Exit(cmd.Execute(version))
}
