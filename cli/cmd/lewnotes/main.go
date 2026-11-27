// Command lewnotes is the human and agent interface to a lewnotes server.
//
// The TUI lives behind `lewnotes tui` so that CLI and TUI share one config,
// one credential, and one HTTP client.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/caleblewallen/lewnotes/cli/client"
	"github.com/caleblewallen/lewnotes/cli/internal/config"
)

// version is overridden at build time with -ldflags "-X main.version=<version>".
var version = "dev"

var errNotLoggedIn = errors.New("not logged in; run `lewnotes login` or set LEWNOTES_TOKEN")

type usageError struct{ message string }

func (e *usageError) Error() string { return e.message }

type ambiguousError struct {
	Ref        string
	Candidates []string
}

func (e *ambiguousError) Error() string {
	return fmt.Sprintf("%q matches more than one note: %s", e.Ref, strings.Join(e.Candidates, ", "))
}

type globalOptions struct {
	server  string
	token   string
	config  string
	timeout time.Duration
	json    bool
	lines   bool
	quiet   bool
	verbose bool
	help    bool
}

type app struct {
	opts      globalOptions
	out       *printer
	c         *client.Client
	cfg       config.File
	cfgPath   string
	cfgLoaded bool
}

type envelope struct {
	OK       bool       `json:"ok"`
	Data     any        `json:"data,omitempty"`
	Warnings []string   `json:"warnings"`
	Error    *errorBody `json:"error,omitempty"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"status,omitempty"`
}

type printer struct {
	out   io.Writer
	err   io.Writer
	json  bool
	lines bool
	quiet bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, rest, err := parseGlobals(args)
	p := &printer{out: stdout, err: stderr, json: opts.json, lines: opts.lines, quiet: opts.quiet}
	if err != nil {
		return p.fail(err)
	}
	a := &app{opts: opts, out: p}

	path, cmd := resolveCommand(rest)
	if opts.help {
		printHelp(stdout, path, cmd)
		return 0
	}
	if cmd == nil {
		if len(rest) == 0 {
			printRootHelp(stdout)
			return 0
		}
		return p.fail(&usageError{message: fmt.Sprintf("unknown command %q", rest[0])})
	}
	if len(cmd.Children) > 0 && len(rest) == len(path) {
		printHelp(stdout, path, cmd)
		return 0
	}

	commandArgs := rest[len(path):]
	switch strings.Join(path, " ") {
	case "version":
		return runVersion(a)
	case "login":
		return runLogin(a, commandArgs)
	case "logout":
		return runLogout(a, commandArgs)
	case "whoami":
		return runWhoami(a)
	case "config get":
		return runConfigGet(a, commandArgs)
	case "config set":
		return runConfigSet(a, commandArgs)
	case "note list":
		return runNoteList(a, commandArgs)
	case "note show":
		return runNoteShow(a, commandArgs)
	case "note new":
		return runNoteNew(a, commandArgs)
	case "note append":
		return runNoteAppend(a, commandArgs)
	case "note edit":
		return runNoteEdit(a, commandArgs)
	case "note rm":
		return runNoteRemove(a, commandArgs)
	case "note mv":
		return runNoteMove(a, commandArgs)
	case "note links":
		return runNoteLinks(a, commandArgs)
	case "note backlinks":
		return runNoteBacklinks(a, commandArgs)
	case "search":
		return runSearch(a, commandArgs)
	case "index status":
		return runIndexStatus(a, commandArgs)
	case "index rescan":
		return runIndexRescan(a, commandArgs)
	case "skills print":
		return runSkillsPrint(a)
	case "skills install":
		return runSkillsInstall(a, commandArgs)
	case "completion":
		return runCompletion(a, commandArgs)
	case "help":
		return runHelp(a, commandArgs)
	default:
		return p.fail(&usageError{message: "unknown command"})
	}
}

func parseGlobals(args []string) (globalOptions, []string, error) {
	opts := globalOptions{timeout: 30 * time.Second}
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, value, hasValue := strings.Cut(arg, "=")
		takeValue := func() (string, error) {
			if hasValue {
				return value, nil
			}
			i++
			if i >= len(args) {
				return "", &usageError{message: fmt.Sprintf("%s needs a value", name)}
			}
			return args[i], nil
		}
		switch name {
		case "--json":
			if !hasValue || value == "" || value == "true" {
				opts.json = true
			} else if value == "lines" {
				opts.json = true
				opts.lines = true
			} else if value == "false" {
				opts.json = false
			} else {
				return opts, rest, &usageError{message: "--json accepts no value, =lines, or =false"}
			}
		case "--server":
			v, err := takeValue()
			if err != nil {
				return opts, rest, err
			}
			opts.server = v
		case "--token":
			v, err := takeValue()
			if err != nil {
				return opts, rest, err
			}
			opts.token = v
		case "--config":
			v, err := takeValue()
			if err != nil {
				return opts, rest, err
			}
			opts.config = v
		case "--timeout":
			v, err := takeValue()
			if err != nil {
				return opts, rest, err
			}
			parsed, parseErr := time.ParseDuration(v)
			if parseErr != nil || parsed <= 0 {
				return opts, rest, &usageError{message: "--timeout must be a positive duration such as 30s"}
			}
			opts.timeout = parsed
		case "--quiet":
			opts.quiet = true
		case "--verbose":
			opts.verbose = true
		case "--no-color":
			// Output is uncolored; accepting this keeps scripts portable.
		case "--help", "-h":
			opts.help = true
		default:
			rest = append(rest, arg)
		}
	}
	return opts, rest, nil
}

func resolveCommand(words []string) ([]string, *CommandInfo) {
	var path []string
	nodes := catalog
	var found *CommandInfo
	for _, word := range words {
		if strings.HasPrefix(word, "-") {
			break
		}
		var next *CommandInfo
		for i := range nodes {
			if nodes[i].Name == word {
				next = &nodes[i]
				break
			}
		}
		if next == nil {
			break
		}
		path = append(path, word)
		found = next
		nodes = next.Children
	}
	return path, found
}

func (a *app) connect(requireToken bool) error {
	if !a.cfgLoaded {
		cfg, path, err := config.Load(a.opts.config)
		if err != nil {
			return err
		}
		a.cfg = cfg
		a.cfgPath = path
		a.cfgLoaded = true
	}

	server := firstNonEmpty(a.opts.server, os.Getenv("LEWNOTES_SERVER"), a.cfg.Server)
	token := firstNonEmpty(a.opts.token, os.Getenv("LEWNOTES_TOKEN"), a.cfg.Token)
	if requireToken && token == "" {
		return errNotLoggedIn
	}
	if server == "" && token != "" {
		// A device token already embeds the origin, which makes
		// `LEWNOTES_TOKEN` alone enough on a fresh machine or in CI.
		if parsed, err := client.ParseToken(token); err == nil {
			server = parsed.ServerURL
		}
	}
	if server == "" {
		return &usageError{message: "no server configured; pass --server or run `lewnotes login`"}
	}
	origin, err := client.ValidateServerURL(server)
	if err != nil {
		return &usageError{message: err.Error()}
	}

	c := client.New(origin)
	c.Token = token
	c.HTTP.Timeout = a.opts.timeout
	a.c = c
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (p *printer) ok(data any, render func()) int {
	if p.json {
		p.writeJSON(envelope{OK: true, Data: data, Warnings: []string{}})
		return 0
	}
	if !p.quiet && render != nil {
		render()
	}
	return 0
}

func (p *printer) list(items []any, render func()) int {
	if p.lines {
		encoder := json.NewEncoder(p.out)
		for _, item := range items {
			_ = encoder.Encode(item)
		}
		return 0
	}
	return p.ok(items, render)
}

func (p *printer) fail(err error) int {
	code, status := classify(err)
	if p.json {
		p.writeJSON(envelope{
			OK:       false,
			Warnings: []string{},
			Error:    &errorBody{Code: code, Message: err.Error(), Status: status},
		})
	} else if !p.quiet {
		fmt.Fprintf(p.err, "lewnotes: %s\n", err.Error())
		if code == "usage" {
			fmt.Fprintln(p.err, "Run `lewnotes help` for usage.")
		}
	}
	if status, ok := exitCodes[code]; ok {
		return status
	}
	// Login has its own error codes; none of them means success.
	return 1
}

func (p *printer) writeJSON(value any) {
	encoder := json.NewEncoder(p.out)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(value)
}

func classify(err error) (string, int) {
	var usage *usageError
	var ambiguous *ambiguousError
	var apiErr *client.APIError
	var login *loginError
	switch {
	case errors.As(err, &login):
		return login.code, 0
	case errors.As(err, &usage):
		return "usage", 0
	case errors.As(err, &ambiguous):
		return "ambiguous_ref", 0
	case errors.Is(err, errNotLoggedIn):
		return "auth", 0
	case errors.As(err, &apiErr) && apiErr.IsAuth():
		return "auth", apiErr.Status
	case errors.As(err, &apiErr) && apiErr.IsConflict():
		return "conflict", apiErr.Status
	case errors.Is(err, client.ErrUnreachable):
		return "unreachable", 0
	case errors.As(err, &apiErr):
		return "error", apiErr.Status
	default:
		return "error", 0
	}
}

func writeCatalogue(w io.Writer, data any) {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	_ = encoder.Encode(envelope{OK: true, Data: data, Warnings: []string{}})
}

func runVersion(a *app) int {
	data := map[string]string{"version": version, "os": runtime.GOOS, "arch": runtime.GOARCH}
	return a.out.ok(data, func() { fmt.Fprintln(a.out.out, version) })
}

func runHelp(a *app, args []string) int {
	path, cmd := resolveCommand(args)
	if a.out.json {
		if cmd == nil {
			writeCatalogue(a.out.out, Catalogue{
				Schema:      "lewnotes.cli.v1",
				Version:     version,
				GlobalFlags: globalFlags,
				ExitCodes:   exitCodes,
				Commands:    catalog,
			})
			return 0
		}
		writeCatalogue(a.out.out, cmd)
		return 0
	}
	if cmd == nil {
		printRootHelp(a.out.out)
	} else {
		printHelp(a.out.out, path, cmd)
	}
	return 0
}

func printRootHelp(w io.Writer) {
	fmt.Fprint(w, "lewnotes — notes, tasks, and whiteboards on your own server\n\nUsage:\n  lewnotes <command> [flags]\n\nCommands:\n")
	for _, command := range catalog {
		fmt.Fprintf(w, "  %-16s %s\n", command.Name, command.Summary)
	}
	fmt.Fprint(w, "\nGlobal flags:\n")
	for _, flag := range globalFlags {
		fmt.Fprintf(w, "  %-24s %s\n", flag.Name, flag.Description)
	}
	fmt.Fprint(w, "\nExit codes: 0 ok, 1 failure, 2 usage, 3 ambiguous ref, 4 auth, 5 conflict, 6 unreachable\n")
	fmt.Fprint(w, "Run `lewnotes help <command>` for details, or `lewnotes help --json` for the full catalogue.\n")
}

func printHelp(w io.Writer, path []string, cmd *CommandInfo) {
	if cmd == nil {
		printRootHelp(w)
		return
	}
	fmt.Fprintf(w, "lewnotes %s — %s\n\n", strings.Join(path, " "), cmd.Summary)
	if cmd.Usage != "" {
		fmt.Fprintf(w, "Usage:\n  %s\n", cmd.Usage)
	}
	if len(cmd.Args) > 0 {
		fmt.Fprint(w, "\nArguments:\n")
		for _, arg := range cmd.Args {
			optional := ""
			if !arg.Required {
				optional = " (optional)"
			}
			fmt.Fprintf(w, "  %-16s %s%s\n", arg.Name, arg.Description, optional)
		}
	}
	if len(cmd.Flags) > 0 {
		fmt.Fprint(w, "\nFlags:\n")
		for _, flag := range cmd.Flags {
			def := ""
			if flag.Default != "" {
				def = " (default " + flag.Default + ")"
			}
			fmt.Fprintf(w, "  %-20s %s%s\n", flag.Name, flag.Description, def)
		}
	}
	if len(cmd.Children) > 0 {
		fmt.Fprint(w, "\nCommands:\n")
		for _, child := range cmd.Children {
			fmt.Fprintf(w, "  %-16s %s\n", child.Name, child.Summary)
		}
	}
	if len(cmd.Output) > 0 {
		fmt.Fprintf(w, "\nJSON data keys: %s\n", strings.Join(cmd.Output, ", "))
	}
	if len(cmd.Examples) > 0 {
		fmt.Fprint(w, "\nExamples:\n")
		for _, example := range cmd.Examples {
			fmt.Fprintf(w, "  %s\n", example)
		}
	}
	if cmd.Mutating {
		fmt.Fprint(w, "\nThis command mutates the vault.\n")
	}
}

func parseIntFlag(value, name string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, &usageError{message: fmt.Sprintf("%s must be an integer", name)}
	}
	return parsed, nil
}

func contextWithTimeout(opts globalOptions) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opts.timeout)
}
