// Command lewnotes is the human and agent interface to a lewnotes server.
//
// The TUI lives behind `lewnotes tui` so that CLI and TUI share one config,
// one credential, and one HTTP client.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
)

// version is overridden at build time with -ldflags "-X main.version=<version>".
var version = "dev"

type commandInfo struct {
	Name    string   `json:"name"`
	Summary string   `json:"summary"`
	Args    []string `json:"args,omitempty"`
	Flags   []string `json:"flags,omitempty"`
	Mutable bool     `json:"mutable"`
}

// commands is the single source of truth for `--help` and `--help --json`.
var commands = []commandInfo{
	{Name: "version", Summary: "Print the CLI version.", Flags: []string{"--json"}, Mutable: false},
	{Name: "help", Summary: "Print this command catalogue (add --json for machine-readable output).", Flags: []string{"--json"}, Mutable: false},
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	jsonOut := false
	filtered := make([]string, 0, len(args))
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		default:
			filtered = append(filtered, a)
		}
	}

	if len(filtered) == 0 || filtered[0] == "help" || filtered[0] == "--help" || filtered[0] == "-h" {
		if jsonOut {
			writeJSON(stdout, map[string]any{
				"ok":       true,
				"data":     commands,
				"schema":   "lewnotes.cli.v1",
				"version":  version,
				"readonly": true,
			})
			return 0
		}
		printUsage(stdout)
		return 0
	}

	switch filtered[0] {
	case "version":
		if jsonOut {
			writeJSON(stdout, map[string]any{
				"ok":   true,
				"data": map[string]string{"version": version, "os": runtime.GOOS, "arch": runtime.GOARCH},
			})
			return 0
		}
		fmt.Fprintln(stdout, version)
		return 0
	default:
		fmt.Fprintf(stderr, "lewnotes: unknown command %q\n\n", filtered[0])
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `lewnotes — notes, tasks, and whiteboards on your own server

Usage:
  lewnotes <command> [flags]

Commands:
  version    Print the CLI version
  help       Print this command catalogue

Planned, landing slice by slice (see .agents/plans/2026-11-24-build-slices.md):
  login, logout, whoami, config
  note list|show|new|append|edit|rm|mv|links|backlinks
  search, todo, attach, board, index, tui, skills

Global flags:
  --json     Machine-readable output (stable envelope: {ok, data, warnings, error})
  --help     Show this text; combine with --json for the command catalogue

Exit codes: 0 ok, 1 failure, 2 usage, 3 ambiguous ref, 4 auth, 5 conflict, 6 unreachable
`)
}

func writeJSON(w io.Writer, v any) {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
