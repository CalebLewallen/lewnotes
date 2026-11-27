package main

// FileInfo is one argument or flag in the machine-readable command catalogue.
type ArgInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

// FlagInfo is one flag in the machine-readable command catalogue.
type FlagInfo struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Default     string `json:"default,omitempty"`
	Description string `json:"description"`
	Required    bool   `json:"required,omitempty"`
}

// CommandInfo is one node in the command tree. It is the single source of
// truth for human help and `--help --json`.
type CommandInfo struct {
	Name     string        `json:"name"`
	Summary  string        `json:"summary"`
	Usage    string        `json:"usage,omitempty"`
	Args     []ArgInfo     `json:"args,omitempty"`
	Flags    []FlagInfo    `json:"flags,omitempty"`
	Output   []string      `json:"output,omitempty"`
	Examples []string      `json:"examples,omitempty"`
	Mutating bool          `json:"mutating"`
	Children []CommandInfo `json:"children,omitempty"`
}

// Catalogue is the stable schema returned by `lewnotes help --json`.
type Catalogue struct {
	Schema      string         `json:"schema"`
	Version     string         `json:"version"`
	GlobalFlags []FlagInfo     `json:"global_flags"`
	ExitCodes   map[string]int `json:"exit_codes"`
	Commands    []CommandInfo  `json:"commands"`
}

var globalFlags = []FlagInfo{
	{Name: "--server", Type: "string", Description: "Server origin, overriding LEWNOTES_SERVER and the config file."},
	{Name: "--token", Type: "string", Description: "Device token, overriding LEWNOTES_TOKEN and the config file. Prefer env/stored credentials."},
	{Name: "--config", Type: "string", Description: "Config file path. Defaults to the user config directory."},
	{Name: "--json", Type: "bool|lines", Default: "false", Description: "Emit the stable JSON envelope; --json=lines emits one JSON object per list item."},
	{Name: "--timeout", Type: "duration", Default: "30s", Description: "HTTP timeout."},
	{Name: "--quiet", Type: "bool", Default: "false", Description: "Suppress human-readable success output."},
	{Name: "--verbose", Type: "bool", Default: "false", Description: "Include extra detail in human-readable errors."},
	{Name: "--no-color", Type: "bool", Default: "false", Description: "Disable color. Output is uncolored today; accepted for scripts."},
}

var exitCodes = map[string]int{
	"ok":            0,
	"error":         1,
	"usage":         2,
	"ambiguous_ref": 3,
	"auth":          4,
	"conflict":      5,
	"unreachable":   6,
}

var catalog = []CommandInfo{
	{
		Name:    "version",
		Summary: "Print the CLI version.",
		Usage:   "lewnotes version [--json]",
		Output:  []string{"version", "os", "arch"},
	},
	{
		Name:    "login",
		Summary: "Approve this device in the browser and store the issued token.",
		Usage:   "lewnotes login [server] [--no-browser] [--device-name NAME] [--token-stdin] [--json]",
		Args:    []ArgInfo{{Name: "server", Description: "Server origin. Needed only on the first login from this machine."}},
		Flags: []FlagInfo{
			{Name: "--no-browser", Type: "bool", Default: "false", Description: "Print a short code and poll instead of opening a local browser."},
			{Name: "--device-name", Type: "string", Description: "Name shown on the approval page. Defaults to the hostname."},
			{Name: "--token-stdin", Type: "bool", Default: "false", Description: "Read an existing device token from stdin (CI and agent path)."},
			{Name: "--open", Type: "bool", Default: "false", Description: "Open the approval page even in a non-interactive shell."},
			{Name: "--no-open", Type: "bool", Default: "false", Description: "Alias for --no-browser."},
		},
		Output: []string{"server", "username", "instance_name", "device_name", "token_name", "auth"},
		Examples: []string{
			"lewnotes login https://notes.example.com",
			"lewnotes login --no-browser",
			"printf '%s' \"$LEWNOTES_TOKEN\" | lewnotes login --token-stdin --json",
		},
	},
	{
		Name:    "logout",
		Summary: "Forget the local device token, optionally revoking it server-side.",
		Usage:   "lewnotes logout [--revoke] [--json]",
		Flags: []FlagInfo{
			{Name: "--revoke", Type: "bool", Default: "false", Description: "Also revoke the token through the API."},
		},
		Output:   []string{"logged_out", "revoked"},
		Mutating: true,
	},
	{
		Name:    "whoami",
		Summary: "Show the authenticated account and token.",
		Usage:   "lewnotes whoami [--json]",
		Output:  []string{"username", "auth", "instance_name", "token"},
	},
	{
		Name:    "config",
		Summary: "Read or change local CLI settings.",
		Children: []CommandInfo{
			{
				Name:    "get",
				Summary: "Show local configuration (the token is never printed).",
				Usage:   "lewnotes config get [server|device_name] [--json]",
				Args:    []ArgInfo{{Name: "key", Description: "Setting to show; omit for all settings."}},
				Output:  []string{"server", "device_name", "token_set", "config_path"},
			},
			{
				Name:    "set",
				Summary: "Change a local setting.",
				Usage:   "lewnotes config set <server|device_name> <value> [--json]",
				Args: []ArgInfo{
					{Name: "key", Description: "Setting name.", Required: true},
					{Name: "value", Description: "New value.", Required: true},
				},
				Output:   []string{"key", "value", "config_path"},
				Mutating: true,
				Examples: []string{"lewnotes config set server https://notes.example.com"},
			},
		},
	},
	{
		Name:    "note",
		Summary: "List, read, create, edit, move, and delete notes.",
		Children: []CommandInfo{
			{
				Name:    "list",
				Summary: "List note summaries.",
				Usage:   "lewnotes note list [--limit N] [--sort updated|created|path|title] [--json[=lines]]",
				Flags: []FlagInfo{
					{Name: "--limit", Type: "int", Default: "50", Description: "Maximum notes to return."},
					{Name: "--cursor", Type: "string", Description: "Pagination cursor from a previous page."},
					{Name: "--path-prefix", Type: "string", Description: "Only notes whose path starts with this prefix."},
					{Name: "--tag", Type: "string", Description: "Only notes carrying this tag."},
					{Name: "--sort", Type: "string", Default: "updated", Description: "updated, created, path, or title."},
				},
				Output: []string{"notes", "total", "next_cursor"},
			},
			{
				Name:    "show",
				Summary: "Print a note by id, path, path suffix, or unique title.",
				Usage:   "lewnotes note show <ref> [--json]",
				Args:    []ArgInfo{{Name: "ref", Description: "Note reference.", Required: true}},
				Output:  []string{"note"},
				Examples: []string{
					"lewnotes note show Kitchen",
					"lewnotes note show notes/Kitchen.md --json",
				},
			},
			{
				Name:    "new",
				Summary: "Create a note.",
				Usage:   "lewnotes note new <title> [--folder DIR] [--body TEXT] [--stdin] [--json]",
				Args:    []ArgInfo{{Name: "title", Description: "Note title; used for the filename unless --path is set.", Required: true}},
				Flags: []FlagInfo{
					{Name: "--folder", Type: "string", Default: "inbox", Description: "Folder inside the inbox. Relative paths are under inbox/."},
					{Name: "--path", Type: "string", Description: "Exact note path; must be inside inbox/."},
					{Name: "--body", Type: "string", Description: "Note body. Defaults to a level-one heading."},
					{Name: "--stdin", Type: "bool", Default: "false", Description: "Read the note body from stdin."},
				},
				Output:   []string{"id", "path", "title", "etag"},
				Mutating: true,
				Examples: []string{
					"lewnotes note new Hello --body \"first note\" --json",
					"lewnotes note new Hello --folder projects --json",
				},
			},
			{
				Name:    "append",
				Summary: "Append text to a note using a conflict-checked patch.",
				Usage:   "lewnotes note append <ref> [text] [--stdin] [--json]",
				Args: []ArgInfo{
					{Name: "ref", Description: "Note reference.", Required: true},
					{Name: "text", Description: "Text to append; use --stdin for multiline input."},
				},
				Flags: []FlagInfo{
					{Name: "--stdin", Type: "bool", Default: "false", Description: "Read the appended text from stdin."},
				},
				Output:   []string{"note"},
				Mutating: true,
			},
			{
				Name:     "edit",
				Summary:  "Open a note in $VISUAL or $EDITOR and save it with If-Match.",
				Usage:    "lewnotes note edit <ref> [--json]",
				Args:     []ArgInfo{{Name: "ref", Description: "Note reference.", Required: true}},
				Output:   []string{"note"},
				Mutating: true,
			},
			{
				Name:    "rm",
				Summary: "Soft-delete a note into .lewnotes/trash.",
				Usage:   "lewnotes note rm <ref> --yes [--json]",
				Args:    []ArgInfo{{Name: "ref", Description: "Note reference.", Required: true}},
				Flags: []FlagInfo{
					{Name: "--yes", Type: "bool", Default: "false", Description: "Confirm deletion when stdin is not a terminal."},
				},
				Output:   []string{"deleted", "trash_path"},
				Mutating: true,
				Examples: []string{"lewnotes note rm 'Old idea' --yes --json"},
			},
			{
				Name:    "mv",
				Summary: "Move or rename a note, rewriting inbound links by default.",
				Usage:   "lewnotes note mv <ref> <new-path> [--no-rewrite] [--json]",
				Args: []ArgInfo{
					{Name: "ref", Description: "Note reference.", Required: true},
					{Name: "new-path", Description: "Destination vault-relative path.", Required: true},
				},
				Flags: []FlagInfo{
					{Name: "--no-rewrite", Type: "bool", Default: "false", Description: "Do not rewrite inbound wikilinks."},
				},
				Output:   []string{"moved", "note", "links_rewritten", "warnings"},
				Mutating: true,
			},
			{
				Name:    "links",
				Summary: "List outgoing links and their resolution state.",
				Usage:   "lewnotes note links <ref> [--json[=lines]]",
				Args:    []ArgInfo{{Name: "ref", Description: "Note reference.", Required: true}},
				Output:  []string{"links"},
			},
			{
				Name:    "backlinks",
				Summary: "List notes that link to this note.",
				Usage:   "lewnotes note backlinks <ref> [--json[=lines]]",
				Args:    []ArgInfo{{Name: "ref", Description: "Note reference.", Required: true}},
				Output:  []string{"backlinks"},
			},
		},
	},
	{
		Name:    "search",
		Summary: "Search with the shared query grammar.",
		Usage:   "lewnotes search <query> [--limit N] [--json[=lines]]",
		Args:    []ArgInfo{{Name: "query", Description: "Free text plus tag:, path:, is:, has:, and -negation.", Required: true}},
		Flags: []FlagInfo{
			{Name: "--limit", Type: "int", Default: "50", Description: "Maximum hits to return."},
			{Name: "--cursor", Type: "string", Description: "Pagination cursor from a previous page."},
		},
		Output:   []string{"hits", "total", "next_cursor"},
		Examples: []string{"lewnotes search 'tag:project is:unresolved' --json"},
	},
	{
		Name:    "index",
		Summary: "Inspect or rebuild the server's SQLite index.",
		Children: []CommandInfo{
			{
				Name:    "status",
				Summary: "Show scan state and counts.",
				Usage:   "lewnotes index status [--json]",
				Output:  []string{"state", "mode", "files_total", "files_done", "notes", "warnings"},
			},
			{
				Name:    "rescan",
				Summary: "Start a background index scan.",
				Usage:   "lewnotes index rescan [--full] [--json]",
				Flags: []FlagInfo{
					{Name: "--full", Type: "bool", Default: "false", Description: "Rehash and reindex every file."},
				},
				Output:   []string{"state", "full"},
				Mutating: true,
			},
		},
	},
	{
		Name:    "skills",
		Summary: "Print or install the lewnotes agent skill.",
		Children: []CommandInfo{
			{
				Name:    "print",
				Summary: "Write the lewnotes-cli skill to stdout.",
				Usage:   "lewnotes skills print [--json]",
				Output:  []string{"name", "content"},
			},
			{
				Name:    "install",
				Summary: "Install the skill under <dir>/lewnotes-cli/SKILL.md.",
				Usage:   "lewnotes skills install [--dir .agents/skills] [--json]",
				Flags: []FlagInfo{
					{Name: "--dir", Type: "string", Default: ".agents/skills", Description: "Skills directory."},
				},
				Output:   []string{"path"},
				Mutating: true,
			},
		},
	},
	{
		Name:    "completion",
		Summary: "Print a shell completion script.",
		Usage:   "lewnotes completion <bash|zsh|fish>",
		Args:    []ArgInfo{{Name: "shell", Description: "bash, zsh, or fish.", Required: true}},
		Output:  []string{"shell", "script"},
	},
	{
		Name:    "help",
		Summary: "Show human help or the machine-readable command catalogue.",
		Usage:   "lewnotes help [command ...] [--json]",
		Args:    []ArgInfo{{Name: "command", Description: "Optional command path."}},
		Output:  []string{"schema", "version", "commands", "global_flags", "exit_codes"},
	},
}
