package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/caleblewallen/lewnotes/cli/client"
	"github.com/caleblewallen/lewnotes/cli/internal/config"
	"github.com/caleblewallen/lewnotes/cli/skills"
)

// parseCommandFlags allows flags before or after positional arguments, which
// is what humans and agents expect from a modern CLI.
func parseCommandFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	ordered := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		name := strings.TrimLeft(arg, "-")
		name, _, hasValue := strings.Cut(name, "=")
		defined := fs.Lookup(name)
		if defined == nil {
			return nil, &usageError{message: fmt.Sprintf("unknown flag %s", arg)}
		}
		ordered = append(ordered, arg)
		if hasValue {
			continue
		}
		if boolFlag, ok := defined.Value.(interface{ IsBoolFlag() bool }); ok && boolFlag.IsBoolFlag() {
			continue
		}
		if i+1 >= len(args) {
			return nil, &usageError{message: fmt.Sprintf("--%s needs a value", name)}
		}
		i++
		ordered = append(ordered, args[i])
	}
	return positionalsAfterParse(fs, ordered, positional)
}

func positionalsAfterParse(fs *flag.FlagSet, ordered, positional []string) ([]string, error) {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(ordered); err != nil {
		return nil, &usageError{message: err.Error()}
	}
	return positional, nil
}

func requireArgs(args []string, count int, names ...string) error {
	if len(args) < count {
		return &usageError{message: fmt.Sprintf("missing %s", strings.Join(names[len(args):], ", "))}
	}
	return nil
}

func runConfigGet(a *app, args []string) int {
	rest, err := parseCommandFlags(flag.NewFlagSet("config get", flag.ContinueOnError), args)
	if err != nil {
		return a.out.fail(err)
	}
	if len(rest) > 1 {
		return a.out.fail(&usageError{message: "config get accepts at most one key"})
	}
	key := ""
	if len(rest) == 1 {
		key = rest[0]
	}

	cfg, path, err := config.Load(a.opts.config)
	if err != nil {
		return a.out.fail(err)
	}
	values := map[string]any{
		"server":      cfg.Server,
		"device_name": cfg.DeviceName,
		"token_set":   cfg.Token != "",
		"config_path": path,
	}
	if key != "" {
		if key == "token" {
			return a.out.fail(&usageError{message: "config get token is disabled; tokens are managed by `lewnotes login`"})
		}
		value, ok := values[key]
		if !ok {
			return a.out.fail(&usageError{message: fmt.Sprintf("unknown config key %q (server, device_name)", key)})
		}
		return a.out.ok(map[string]any{key: value}, func() {
			fmt.Fprintf(a.out.out, "%s = %v\n", key, value)
		})
	}
	return a.out.ok(values, func() {
		fmt.Fprintf(a.out.out, "server = %s\n", cfg.Server)
		fmt.Fprintf(a.out.out, "device_name = %s\n", cfg.DeviceName)
		if cfg.Token == "" {
			fmt.Fprintln(a.out.out, "token = not stored")
		} else {
			fmt.Fprintln(a.out.out, "token = stored")
		}
		fmt.Fprintf(a.out.out, "config = %s\n", path)
	})
}

func runConfigSet(a *app, args []string) int {
	rest, err := parseCommandFlags(flag.NewFlagSet("config set", flag.ContinueOnError), args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 2, "key", "value"); err != nil {
		return a.out.fail(err)
	}
	if len(rest) > 2 {
		return a.out.fail(&usageError{message: "config set accepts exactly a key and a value"})
	}
	cfg, path, err := config.Load(a.opts.config)
	if err != nil {
		return a.out.fail(err)
	}
	key, value := rest[0], rest[1]
	switch key {
	case "server":
		origin, err := client.ValidateServerURL(value)
		if err != nil {
			return a.out.fail(&usageError{message: err.Error()})
		}
		cfg.Server = origin
	case "device_name":
		if strings.TrimSpace(value) == "" {
			return a.out.fail(&usageError{message: "device_name cannot be empty"})
		}
		cfg.DeviceName = strings.TrimSpace(value)
	case "token":
		return a.out.fail(&usageError{message: "config set token is disabled; use `lewnotes login`"})
	default:
		return a.out.fail(&usageError{message: fmt.Sprintf("unknown config key %q (server, device_name)", key)})
	}
	if err := config.Save(path, cfg); err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(map[string]any{"key": key, "value": value, "config_path": path}, func() {
		fmt.Fprintf(a.out.out, "%s = %s\n", key, value)
	})
}

func runNoteList(a *app, args []string) int {
	fs := flag.NewFlagSet("note list", flag.ContinueOnError)
	limit := fs.Int("limit", 50, "maximum notes")
	cursor := fs.String("cursor", "", "pagination cursor")
	prefix := fs.String("path-prefix", "", "path prefix")
	tag := fs.String("tag", "", "tag filter")
	sortBy := fs.String("sort", "updated", "sort order")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if len(rest) != 0 {
		return a.out.fail(&usageError{message: "note list takes no positional arguments"})
	}
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	result, err := a.c.ListNotes(ctx, client.ListOptions{
		Limit: *limit, Cursor: *cursor, PathPrefix: *prefix, Tag: *tag, Sort: *sortBy,
	})
	if err != nil {
		return a.out.fail(err)
	}
	items := make([]any, 0, len(result.Notes))
	for _, note := range result.Notes {
		items = append(items, note)
	}
	return a.out.list(items, func() {
		for _, note := range result.Notes {
			fmt.Fprintf(a.out.out, "%s\t%s\t%s\n", note.Path, note.Title, note.ID)
		}
	})
}

func runNoteShow(a *app, args []string) int {
	rest, err := parseCommandFlags(flag.NewFlagSet("note show", flag.ContinueOnError), args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 1, "ref"); err != nil {
		return a.out.fail(err)
	}
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	note, err := resolveNote(ctx, a.c, rest[0])
	if err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(note, func() {
		if note.Content != "" {
			fmt.Fprint(a.out.out, note.Content)
			if !strings.HasSuffix(note.Content, "\n") {
				fmt.Fprintln(a.out.out)
			}
		} else {
			fmt.Fprintln(a.out.out, note.Body)
		}
	})
}

// defaultNoteFolder is the root for notes. Every note `note new` creates lives
// inside it; subfolders there are free-form.
const defaultNoteFolder = "inbox"

// noteDestination resolves --path or --folder into a path inside the inbox.
func noteDestination(title, folder, path string) (string, error) {
	if path != "" {
		clean := strings.Trim(strings.TrimSpace(path), "/")
		if clean != defaultNoteFolder && !strings.HasPrefix(clean, defaultNoteFolder+"/") {
			return "", &usageError{message: fmt.Sprintf("notes must live inside %s/", defaultNoteFolder)}
		}
		return clean, nil
	}
	parent := strings.Trim(strings.TrimSpace(folder), "/")
	switch {
	case parent == "" || parent == defaultNoteFolder:
		parent = defaultNoteFolder
	case !strings.HasPrefix(parent, defaultNoteFolder+"/"):
		parent = defaultNoteFolder + "/" + parent
	}
	return parent + "/" + noteFilename(title), nil
}

func runNoteNew(a *app, args []string) int {
	fs := flag.NewFlagSet("note new", flag.ContinueOnError)
	folder := fs.String("folder", defaultNoteFolder, "folder inside the inbox")
	pathFlag := fs.String("path", "", "exact path inside the inbox")
	body := fs.String("body", "", "note body")
	stdin := fs.Bool("stdin", false, "read body from stdin")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 1, "title"); err != nil {
		return a.out.fail(err)
	}
	if len(rest) > 1 {
		return a.out.fail(&usageError{message: "note new accepts one title; quote it if it contains spaces"})
	}
	if *stdin && *body != "" {
		return a.out.fail(&usageError{message: "use either --body or --stdin, not both"})
	}
	title := strings.TrimSpace(rest[0])
	if title == "" {
		return a.out.fail(&usageError{message: "title cannot be empty"})
	}

	notePath, err := noteDestination(title, *folder, *pathFlag)
	if err != nil {
		return a.out.fail(err)
	}
	content := *body
	if *stdin {
		data, readErr := io.ReadAll(io.LimitReader(os.Stdin, 10<<20))
		if readErr != nil {
			return a.out.fail(fmt.Errorf("read stdin: %w", readErr))
		}
		content = string(data)
	}
	if content == "" {
		content = "# " + title + "\n"
	} else if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	note, err := a.c.CreateNote(ctx, client.CreateNoteInput{Path: notePath, Content: content})
	if err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(note, func() {
		fmt.Fprintf(a.out.out, "Created %s (%s)\n", note.Path, note.ID)
	})
}

func runNoteAppend(a *app, args []string) int {
	fs := flag.NewFlagSet("note append", flag.ContinueOnError)
	stdin := fs.Bool("stdin", false, "read text from stdin")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 1, "ref"); err != nil {
		return a.out.fail(err)
	}
	text := strings.Join(rest[1:], " ")
	if *stdin {
		if text != "" {
			return a.out.fail(&usageError{message: "use either positional text or --stdin, not both"})
		}
		data, readErr := io.ReadAll(io.LimitReader(os.Stdin, 10<<20))
		if readErr != nil {
			return a.out.fail(fmt.Errorf("read stdin: %w", readErr))
		}
		text = string(data)
	}
	if strings.TrimSpace(text) == "" {
		return a.out.fail(&usageError{message: "nothing to append; pass text or --stdin"})
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	note, err := resolveNote(ctx, a.c, rest[0])
	if err != nil {
		return a.out.fail(err)
	}
	updated, err := a.c.PatchNote(ctx, note.ID, []client.PatchOp{{Type: "append", Text: text}}, note.ETag)
	if err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(updated, func() {
		fmt.Fprintf(a.out.out, "Appended to %s\n", updated.Path)
	})
}

func runNoteEdit(a *app, args []string) int {
	rest, err := parseCommandFlags(flag.NewFlagSet("note edit", flag.ContinueOnError), args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 1, "ref"); err != nil {
		return a.out.fail(err)
	}
	editor := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
	if editor == "" {
		return a.out.fail(&usageError{message: "set $VISUAL or $EDITOR to edit notes"})
	}

	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	note, err := resolveNote(ctx, a.c, rest[0])
	if err != nil {
		return a.out.fail(err)
	}

	temp, err := os.CreateTemp("", "lewnotes-*.md")
	if err != nil {
		return a.out.fail(fmt.Errorf("create temp file: %w", err))
	}
	tempPath := temp.Name()
	removeTemp := true
	defer func() {
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()
	if _, err := temp.WriteString(note.Content); err != nil {
		temp.Close()
		return a.out.fail(fmt.Errorf("write temp file: %w", err))
	}
	if err := temp.Close(); err != nil {
		return a.out.fail(fmt.Errorf("close temp file: %w", err))
	}

	parts := strings.Fields(editor)
	command := exec.Command(parts[0], append(parts[1:], tempPath)...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := command.Run(); err != nil {
		return a.out.fail(fmt.Errorf("editor exited: %w", err))
	}
	updatedBytes, err := os.ReadFile(tempPath)
	if err != nil {
		return a.out.fail(fmt.Errorf("read edited note: %w", err))
	}
	if string(updatedBytes) == note.Content {
		return a.out.ok(note, func() { fmt.Fprintln(a.out.out, "No changes.") })
	}

	updated, err := a.c.UpdateNote(ctx, note.ID, string(updatedBytes), note.ETag)
	if err != nil {
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.IsConflict() {
			removeTemp = false
			return a.out.fail(fmt.Errorf("%s; your edited copy is at %s", err, tempPath))
		}
		return a.out.fail(err)
	}
	return a.out.ok(updated, func() {
		fmt.Fprintf(a.out.out, "Saved %s\n", updated.Path)
	})
}

func runNoteRemove(a *app, args []string) int {
	fs := flag.NewFlagSet("note rm", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "confirm deletion")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 1, "ref"); err != nil {
		return a.out.fail(err)
	}
	if !*yes && !stdinIsTerminal() {
		return a.out.fail(&usageError{message: "refusing to delete without --yes when stdin is not a terminal"})
	}
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	note, err := resolveNote(ctx, a.c, rest[0])
	if err != nil {
		return a.out.fail(err)
	}
	result, err := a.c.DeleteNote(ctx, note.ID, note.ETag)
	if err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(result, func() {
		fmt.Fprintf(a.out.out, "Moved %s to %s\n", note.Path, result.TrashPath)
	})
}

func runNoteMove(a *app, args []string) int {
	fs := flag.NewFlagSet("note mv", flag.ContinueOnError)
	noRewrite := fs.Bool("no-rewrite", false, "do not rewrite inbound links")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 2, "ref", "new-path"); err != nil {
		return a.out.fail(err)
	}
	newPath := strings.TrimPrefix(strings.TrimSpace(rest[1]), "/")
	if newPath == "" {
		return a.out.fail(&usageError{message: "new path cannot be empty"})
	}
	if !strings.HasSuffix(strings.ToLower(newPath), ".md") {
		newPath += ".md"
	}
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	note, err := resolveNote(ctx, a.c, rest[0])
	if err != nil {
		return a.out.fail(err)
	}
	result, err := a.c.MoveNote(ctx, note.ID, newPath, !*noRewrite)
	if err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(result, func() {
		fmt.Fprintf(a.out.out, "Moved to %s; %d link(s) rewritten\n", result.Note.Path, result.LinksRewritten)
	})
}

func runNoteLinks(a *app, args []string) int {
	rest, err := parseCommandFlags(flag.NewFlagSet("note links", flag.ContinueOnError), args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 1, "ref"); err != nil {
		return a.out.fail(err)
	}
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	note, err := resolveNote(ctx, a.c, rest[0])
	if err != nil {
		return a.out.fail(err)
	}
	links, err := a.c.Links(ctx, note.ID)
	if err != nil {
		return a.out.fail(err)
	}
	items := make([]any, 0, len(links))
	for _, link := range links {
		items = append(items, link)
	}
	return a.out.list(items, func() {
		for _, link := range links {
			fmt.Fprintf(a.out.out, "%s\t%s\t%s:%d\n", link.Resolution, link.Target, note.Path, link.Line)
		}
	})
}

func runNoteBacklinks(a *app, args []string) int {
	rest, err := parseCommandFlags(flag.NewFlagSet("note backlinks", flag.ContinueOnError), args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 1, "ref"); err != nil {
		return a.out.fail(err)
	}
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	note, err := resolveNote(ctx, a.c, rest[0])
	if err != nil {
		return a.out.fail(err)
	}
	backlinks, err := a.c.Backlinks(ctx, note.ID)
	if err != nil {
		return a.out.fail(err)
	}
	items := make([]any, 0, len(backlinks))
	for _, link := range backlinks {
		items = append(items, link)
	}
	return a.out.list(items, func() {
		for _, link := range backlinks {
			fmt.Fprintf(a.out.out, "%s:%d\n", link.Path, link.Line)
		}
	})
}

func runSearch(a *app, args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	limit := fs.Int("limit", 50, "maximum hits")
	cursor := fs.String("cursor", "", "pagination cursor")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 1, "query"); err != nil {
		return a.out.fail(err)
	}
	query := strings.Join(rest, " ")
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	result, err := a.c.Search(ctx, query, *limit, *cursor)
	if err != nil {
		return a.out.fail(err)
	}
	items := make([]any, 0, len(result.Hits))
	for _, hit := range result.Hits {
		items = append(items, hit)
	}
	return a.out.list(items, func() {
		for _, hit := range result.Hits {
			fmt.Fprintf(a.out.out, "%s\t%s\t%s\n", hit.Path, hit.Title, stripMarks(hit.Snippet))
		}
	})
}

func runIndexStatus(a *app, args []string) int {
	rest, err := parseCommandFlags(flag.NewFlagSet("index status", flag.ContinueOnError), args)
	if err != nil {
		return a.out.fail(err)
	}
	if len(rest) != 0 {
		return a.out.fail(&usageError{message: "index status takes no arguments"})
	}
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	status, err := a.c.IndexStatus(ctx)
	if err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(status, func() {
		fmt.Fprintf(a.out.out, "%s\t%s\t%d/%d files\t%d notes\n",
			status.State, status.Mode, status.FilesDone, status.FilesTotal, status.Notes)
		for _, warning := range status.Warnings {
			fmt.Fprintf(a.out.err, "warning: %s\n", warning)
		}
	})
}

func runIndexRescan(a *app, args []string) int {
	fs := flag.NewFlagSet("index rescan", flag.ContinueOnError)
	full := fs.Bool("full", false, "full rescan")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if len(rest) != 0 {
		return a.out.fail(&usageError{message: "index rescan takes no positional arguments"})
	}
	if err := a.connect(true); err != nil {
		return a.out.fail(err)
	}
	ctx, cancel := contextWithTimeout(a.opts)
	defer cancel()
	result, err := a.c.Rescan(ctx, *full)
	if err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(result, func() {
		fmt.Fprintf(a.out.out, "Started %s rescan\n", map[bool]string{true: "full", false: "incremental"}[*full])
	})
}

func runSkillsPrint(a *app) int {
	content := skills.Content
	return a.out.ok(map[string]string{"name": "lewnotes-cli", "content": content}, func() {
		fmt.Fprint(a.out.out, content)
	})
}

func runSkillsInstall(a *app, args []string) int {
	fs := flag.NewFlagSet("skills install", flag.ContinueOnError)
	dir := fs.String("dir", ".agents/skills", "skills directory")
	rest, err := parseCommandFlags(fs, args)
	if err != nil {
		return a.out.fail(err)
	}
	if len(rest) != 0 {
		return a.out.fail(&usageError{message: "skills install takes no positional arguments"})
	}
	target := filepath.Join(*dir, "lewnotes-cli", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return a.out.fail(fmt.Errorf("create skill directory: %w", err))
	}
	if err := os.WriteFile(target, []byte(skills.Content), 0o644); err != nil {
		return a.out.fail(fmt.Errorf("write skill: %w", err))
	}
	return a.out.ok(map[string]string{"path": target}, func() {
		fmt.Fprintf(a.out.out, "Installed %s\n", target)
	})
}

func runCompletion(a *app, args []string) int {
	rest, err := parseCommandFlags(flag.NewFlagSet("completion", flag.ContinueOnError), args)
	if err != nil {
		return a.out.fail(err)
	}
	if err := requireArgs(rest, 1, "shell"); err != nil {
		return a.out.fail(err)
	}
	script, err := completionScript(rest[0])
	if err != nil {
		return a.out.fail(err)
	}
	return a.out.ok(map[string]string{"shell": rest[0], "script": script}, func() {
		fmt.Fprint(a.out.out, script)
	})
}

var nonFilename = regexp.MustCompile(`[\\/:*?"<>|\x00-\x1f]`)

func noteFilename(title string) string {
	clean := strings.TrimSpace(nonFilename.ReplaceAllString(title, "-"))
	clean = regexp.MustCompile(`\s+`).ReplaceAllString(clean, " ")
	clean = strings.Trim(clean, ". ")
	if clean == "" {
		clean = "Untitled"
	}
	if !strings.HasSuffix(strings.ToLower(clean), ".md") {
		clean += ".md"
	}
	return clean
}

func stripMarks(snippet string) string {
	replacer := strings.NewReplacer("<mark>", "", "</mark>", "")
	return replacer.Replace(snippet)
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func completionScript(shell string) (string, error) {
	names := []string{"version", "login", "logout", "whoami", "config", "note", "search", "index", "skills", "completion", "help"}
	words := strings.Join(names, " ")
	switch shell {
	case "bash":
		return fmt.Sprintf("_lewnotes() {\n  local cur=\"${COMP_WORDS[COMP_CWORD]}\"\n  COMPREPLY=( $(compgen -W %q -- \"$cur\") )\n}\ncomplete -F _lewnotes lewnotes\n", words), nil
	case "zsh":
		return fmt.Sprintf("#compdef lewnotes\n_lewnotes() {\n  compadd -- %s\n}\ncompdef _lewnotes lewnotes\n", words), nil
	case "fish":
		return fmt.Sprintf("complete -c lewnotes -f -a %q\n", words), nil
	default:
		return "", &usageError{message: "shell must be bash, zsh, or fish"}
	}
}
