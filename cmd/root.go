// Package cmd implements the webhookd command-line interface.
//
// The root command carries the shared flags every provider subcommand
// inherits. Each provider contributes one file in this package
// (cmd/<name>.go) that declares its subcommand and adds it to the root
// during init(). This package is the only place in the codebase that
// knows which providers exist; the core packages — server, output,
// providers — never do.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/0xProgress/webhookd/providers"
)

// buildVersion is set by Execute from the value the linker injected
// into main.version. It defaults to "dev" so a plain `go build` still
// produces a runnable binary with a sensible version string.
var buildVersion = "dev"

// rootCmd is the top-level command. It carries the shared persistent
// flags every provider subcommand inherits, and handles the two
// root-only operations: --list and --version.
var rootCmd = &cobra.Command{
	Use:   "webhookd",
	Short: "The Unix pipe for webhooks",
	Long: `webhookd listens for webhook HTTP requests, verifies their signatures,
and writes one JSONL line per verified event to stdout.

Pipe the output anywhere: webhookd github | jq, webhookd stripe | grep,
webhookd shopify | tee events.jsonl.`,
	Args:          cobra.NoArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if ver, _ := cmd.Flags().GetBool("version"); ver {
			return runVersion(cmd)
		}
		if list, _ := cmd.Flags().GetBool("list"); list {
			return runList(cmd)
		}
		return cmd.Help()
	},
}

func init() {
	// Shared flags. Declared on PersistentFlags so every provider
	// subcommand inherits them; a subcommand's RunE reads them via
	// cmd.Flags() or cmd.InheritedFlags().
	pf := rootCmd.PersistentFlags()
	pf.String("secret-env", "", "Name of the environment variable holding the signing secret")
	pf.Int("port", 8080, "HTTP port")
	pf.String("host", "127.0.0.1", "Bind address")
	pf.String("path", "", "Endpoint path (default /<provider>)")
	pf.Bool("pretty", false, "Human-readable output instead of JSONL")
	pf.Int64("max-body", 2097152, "Max request body in bytes (2MB)")
	pf.Int("timeout", 10, "Read and write timeout, seconds")

	// Root-only flags. Declared on Flags, not PersistentFlags, because
	// --list and --version are meaningful only when no subcommand was
	// given. webhookd mock --list is a user error, and cobra rejects
	// it with "unknown flag" rather than silently ignoring it.
	rootCmd.Flags().Bool("list", false, "List all registered providers and exit")
	rootCmd.Flags().Bool("version", false, "Print version and exit")
}

// Execute runs the root command.
//
// version is the build version, injected into main by the linker via
// -X main.version=... The spec's Makefile targets main.version, so
// main.go owns the variable and passes it here; Execute stashes it so
// subcommands can include it in the startup banner.
//
// The return value is the process exit code: 0 for success, 1 for any
// error. main.go passes it to os.Exit.
func Execute(version string) int {
	buildVersion = version
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "webhookd: %v\n", err)
		return 1
	}
	return 0
}

// runList prints the name of every registered provider, one per line,
// to stdout. The list comes from providers.All(), which sees every
// provider whose cmd/<name>.go is compiled into this binary.
//
// Output goes to stdout, not stderr: --list is a data query, matching
// the same stdout-is-data contract the JSONL stream follows. A caller
// can pipe it: `webhookd --list | xargs -n1 webhookd`.
func runList(cmd *cobra.Command) error {
	out := cmd.OutOrStdout()
	for _, name := range providers.All() {
		if _, err := fmt.Fprintln(out, name); err != nil {
			return err
		}
	}
	return nil
}

// runVersion prints the version string to stdout.
//
// The format is fixed by docs/webhookd-core.md §"--version":
// "webhookd <version>", one line, exit 0.
func runVersion(cmd *cobra.Command) error {
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "webhookd %s\n", buildVersion)
	return err
}