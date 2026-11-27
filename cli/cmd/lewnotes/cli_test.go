package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/caleblewallen/lewnotes/cli/client"
	"github.com/caleblewallen/lewnotes/cli/internal/config"
)

const (
	kitchenID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"
	gardenID  = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
)

type fakeServer struct {
	t             *testing.T
	server        *httptest.Server
	token         string
	mu            sync.Mutex
	notes         map[string]*client.Note
	conflictPatch bool
	etagCounter   int
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	f := &fakeServer{t: t, notes: map[string]*client.Note{}}
	f.notes["Kitchen.md"] = &client.Note{
		ID: kitchenID, Path: "Kitchen.md", Title: "Kitchen", ETag: `"etag-kitchen"`,
		Size: 24, UpdatedAt: "2026-11-20T12:00:00Z", Tags: []string{"home"},
		Frontmatter: map[string]any{"id": kitchenID},
		Content:     "# Kitchen\n\nSee [[Garden]]\n", Body: "See [[Garden]]\n",
		Links: []client.Link{{
			ID: gardenID, Path: "garden/Garden.md", Title: "Garden", Target: "Garden",
			Kind: "link", Resolution: "exact", Line: 3,
		}},
		Backlinks: []client.Backlink{},
	}
	f.notes["garden/Garden.md"] = &client.Note{
		ID: gardenID, Path: "garden/Garden.md", Title: "Garden", ETag: `"etag-garden"`,
		Size: 18, UpdatedAt: "2026-11-19T09:30:00Z", Tags: []string{},
		Frontmatter: map[string]any{"id": gardenID},
		Content:     "# Garden\n", Body: "Garden\n",
		Links: []client.Link{},
		Backlinks: []client.Backlink{{
			ID: kitchenID, Path: "Kitchen.md", Title: "Kitchen", Kind: "link", Line: 3,
		}},
	}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)

	secret := make([]byte, client.TokenSecretBytes)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	f.token = "lnt." + base64.RawURLEncoding.EncodeToString([]byte(f.server.URL)) + "." +
		kitchenID + "." + base64.RawURLEncoding.EncodeToString(secret)
	return f
}

func (f *fakeServer) url() string { return f.server.URL }

func (f *fakeServer) handle(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch path {
	case "/api/v1/health":
		writeTestJSON(w, http.StatusOK, map[string]any{"ok": true, "version": "test-1", "uptime_seconds": 1.5})
		return
	case "/api/v1/auth/status":
		writeTestJSON(w, http.StatusOK, map[string]any{"setup_required": false, "instance_name": "home", "version": "test-1"})
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+f.token {
		writeProblem(w, http.StatusUnauthorized, "unauthenticated", "Sign in or provide a valid device token.")
		return
	}

	switch {
	case path == "/api/v1/auth/me":
		writeTestJSON(w, http.StatusOK, map[string]any{
			"username": "caleb", "auth": "token", "instance_name": "home",
			"token": map[string]string{"id": kitchenID, "name": "cli", "scope": "full"},
		})
	case path == "/api/v1/notes" && r.Method == http.MethodGet:
		f.listNotes(w, r)
	case path == "/api/v1/notes" && r.Method == http.MethodPost:
		f.createNote(w, r)
	case path == "/api/v1/notes/by-path":
		f.getByPath(w, r)
	case strings.HasPrefix(path, "/api/v1/notes/"):
		f.noteAction(w, r)
	case path == "/api/v1/search":
		writeTestJSON(w, http.StatusOK, map[string]any{
			"hits": []client.SearchHit{
				{ID: kitchenID, Path: "Kitchen.md", Title: "Kitchen", Snippet: "See <mark>Garden</mark>", Tags: []string{"home"}, Type: "note"},
				{ID: gardenID, Path: "garden/Garden.md", Title: "Garden", Snippet: "A <mark>garden</mark>", Tags: []string{}, Type: "note"},
			},
			"total": 2,
		})
	case path == "/api/v1/index/status":
		writeTestJSON(w, http.StatusOK, client.IndexStatus{
			State: "idle", Mode: "incremental", FilesTotal: 3, FilesDone: 3,
			StartedAt: "2026-11-20T12:00:00Z", FinishedAt: "2026-11-20T12:00:01Z",
			Warnings: []string{}, Notes: 2,
		})
	case path == "/api/v1/index/rescan":
		writeTestJSON(w, http.StatusAccepted, map[string]any{"state": "scanning", "full": true})
	case path == "/api/v1/tokens" && r.Method == http.MethodGet:
		writeTestJSON(w, http.StatusOK, map[string]any{"tokens": []any{}})
	case strings.HasPrefix(path, "/api/v1/tokens/") && r.Method == http.MethodDelete:
		w.WriteHeader(http.StatusNoContent)
	default:
		writeProblem(w, http.StatusNotFound, "not_found", "No route matches this path.")
	}
}

func (f *fakeServer) listNotes(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	prefix := r.URL.Query().Get("path_prefix")
	paths := make([]string, 0, len(f.notes))
	for path := range f.notes {
		if prefix == "" || strings.HasPrefix(path, prefix) {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, _ = strconv.Atoi(raw)
	}
	summaries := make([]client.NoteSummary, 0, len(paths))
	for _, path := range paths {
		note := f.notes[path]
		summaries = append(summaries, client.NoteSummary{
			ID: note.ID, Path: note.Path, Title: note.Title, Excerpt: "Excerpt for " + note.Title,
			Tags: note.Tags, Size: note.Size, UpdatedAt: 1763200000,
		})
	}
	total := len(summaries)
	if limit >= 0 && total > limit {
		summaries = summaries[:limit]
	}
	writeTestJSON(w, http.StatusOK, client.NoteList{Notes: summaries, Total: total})
}

func (f *fakeServer) createNote(w http.ResponseWriter, r *http.Request) {
	var input client.CreateNoteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeProblem(w, http.StatusBadRequest, "invalid_body", "bad body")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, exists := f.notes[input.Path]; exists {
		writeProblem(w, http.StatusConflict, "note_exists", "A file already exists at that path.")
		return
	}
	id := fmt.Sprintf("01ARZ3NDEKTSV4RRFFQ69G5F%02d", len(f.notes))
	note := &client.Note{
		ID: id, Path: input.Path, Title: strings.TrimSuffix(filepath.Base(input.Path), ".md"),
		ETag: `"etag-new"`, Size: int64(len(input.Content)), UpdatedAt: "2026-11-21T12:00:00Z",
		Tags: []string{}, Frontmatter: map[string]any{"id": id}, Content: input.Content, Body: input.Content,
		Links: []client.Link{}, Backlinks: []client.Backlink{},
	}
	f.notes[input.Path] = note
	writeTestJSON(w, http.StatusCreated, note)
}

func (f *fakeServer) getByPath(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	note, ok := f.notes[r.URL.Query().Get("path")]
	if !ok {
		writeProblem(w, http.StatusNotFound, "note_not_found", "No note matches that path.")
		return
	}
	writeTestJSON(w, http.StatusOK, note)
}

func (f *fakeServer) noteAction(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/notes/")
	parts := strings.Split(rest, "/")
	id := parts[0]
	f.mu.Lock()
	defer f.mu.Unlock()
	var note *client.Note
	for _, candidate := range f.notes {
		if candidate.ID == id {
			note = candidate
			break
		}
	}
	if note == nil {
		writeProblem(w, http.StatusNotFound, "note_not_found", "No note matches that id.")
		return
	}

	if len(parts) == 2 {
		switch parts[1] {
		case "links":
			writeTestJSON(w, http.StatusOK, map[string]any{"links": note.Links})
			return
		case "backlinks":
			writeTestJSON(w, http.StatusOK, map[string]any{"backlinks": note.Backlinks})
			return
		case "move":
			var req struct {
				Path string `json:"path"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			delete(f.notes, note.Path)
			note.Path = req.Path
			f.notes[note.Path] = note
			writeTestJSON(w, http.StatusOK, client.MoveResult{Moved: true, Note: *note, LinksRewritten: 1})
			return
		}
	}

	switch r.Method {
	case http.MethodGet:
		writeTestJSON(w, http.StatusOK, note)
	case http.MethodPut, http.MethodPatch:
		if f.conflictPatch || r.Header.Get("If-Match") != note.ETag {
			writeProblem(w, http.StatusPreconditionFailed, "etag_mismatch", "The note changed since your ETag.")
			return
		}
		if r.Method == http.MethodPut {
			var req struct {
				Content string `json:"content"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			note.Content = req.Content
			note.Body = req.Content
		} else {
			var req struct {
				Ops []client.PatchOp `json:"ops"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			for _, op := range req.Ops {
				if op.Type == "append" {
					note.Content += op.Text
					note.Body += op.Text
				}
			}
		}
		f.etagCounter++
		note.ETag = fmt.Sprintf(`"etag-%d"`, f.etagCounter)
		note.Size = int64(len(note.Content))
		writeTestJSON(w, http.StatusOK, note)
	case http.MethodDelete:
		if r.Header.Get("If-Match") != note.ETag {
			writeProblem(w, http.StatusPreconditionFailed, "etag_mismatch", "The note changed since your ETag.")
			return
		}
		delete(f.notes, note.Path)
		writeTestJSON(w, http.StatusOK, client.DeleteResult{Deleted: true, TrashPath: ".lewnotes/trash/" + note.Path})
	default:
		writeProblem(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeProblem(w http.ResponseWriter, status int, kind, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": kind, "title": http.StatusText(status), "status": status, "detail": detail,
	})
}

func runCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(args, &stdout, &stderr)
	return stdout.String(), stderr.String(), code
}

func withStdin(t *testing.T, content string) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	previous := os.Stdin
	os.Stdin = file
	t.Cleanup(func() {
		os.Stdin = previous
		file.Close()
	})
}

func writeConfig(t *testing.T, f *fakeServer) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := config.Save(path, config.File{Server: f.url(), Token: f.token, DeviceName: "test-box"}); err != nil {
		t.Fatal(err)
	}
	return path
}

var updateGoldens = flag.Bool("update", false, "rewrite golden files")

func TestGoldenJSON(t *testing.T) {
	f := newFakeServer(t)
	cfgPath := writeConfig(t, f)
	previousVersion := version
	version = "test"
	t.Cleanup(func() { version = previousVersion })

	cases := []struct {
		name string
		args []string
	}{
		{"help", []string{"help", "--json"}},
		{"version", []string{"version", "--json"}},
		{"whoami", []string{"whoami", "--json", "--config", cfgPath}},
		{"config-get", []string{"config", "get", "--json", "--config", cfgPath}},
		{"note-list", []string{"note", "list", "--json", "--config", cfgPath}},
		{"note-show", []string{"note", "show", "Kitchen", "--json", "--config", cfgPath}},
		{"note-links", []string{"note", "links", "Kitchen", "--json", "--config", cfgPath}},
		{"note-backlinks", []string{"note", "backlinks", "Garden", "--json", "--config", cfgPath}},
		{"search", []string{"search", "garden", "--json", "--config", cfgPath}},
		{"index-status", []string{"index", "status", "--json", "--config", cfgPath}},
		{"skills-print", []string{"skills", "print", "--json"}},
		{"completion-bash", []string{"completion", "bash", "--json"}},
	}

	if *updateGoldens {
		if err := os.MkdirAll("testdata/golden", 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stdout, stderr, code := runCLI(t, tc.args...)
			if code != 0 {
				t.Fatalf("exit = %d, stderr = %s, stdout = %s", code, stderr, stdout)
			}
			got := normalizeGolden(stdout, cfgPath, f.url())
			path := filepath.Join("testdata", "golden", tc.name+".json")
			if *updateGoldens {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", tc.name, got, want)
			}
		})
	}
}

func normalizeGolden(out, cfgPath, serverURL string) string {
	out = strings.ReplaceAll(out, cfgPath, "<CONFIG_PATH>")
	out = strings.ReplaceAll(out, serverURL, "<SERVER>")
	out = strings.ReplaceAll(out, runtime.GOOS, "<OS>")
	out = strings.ReplaceAll(out, runtime.GOARCH, "<ARCH>")
	return out
}

func TestNonInteractiveLoginStoresCredential(t *testing.T) {
	f := newFakeServer(t)
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "nested", "config.toml")
	withStdin(t, f.token+"\n")

	stdout, stderr, code := runCLI(t, "login", "--server", f.url(), "--token-stdin", "--json", "--config", cfgPath)
	if code != 0 {
		t.Fatalf("login exit = %d, stderr = %s, stdout = %s", code, stderr, stdout)
	}
	cfg, _, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server != f.url() || cfg.Token != f.token {
		t.Fatalf("stored config = %+v", cfg)
	}
	info, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 600", info.Mode().Perm())
	}

	stdout, stderr, code = runCLI(t, "whoami", "--json", "--config", cfgPath)
	if code != 0 {
		t.Fatalf("whoami exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, `"username": "caleb"`) {
		t.Errorf("whoami stdout = %s", stdout)
	}
}

func TestAgentFlowAndConflict(t *testing.T) {
	f := newFakeServer(t)
	cfgPath := writeConfig(t, f)

	if _, stderr, code := runCLI(t, "search", "garden", "--json", "--config", cfgPath); code != 0 {
		t.Fatalf("search exit = %d, stderr = %s", code, stderr)
	}

	withStdin(t, "Follow up Friday\n")
	stdout, stderr, code := runCLI(t, "note", "append", "Kitchen", "--stdin", "--json", "--config", cfgPath)
	if code != 0 {
		t.Fatalf("append exit = %d, stderr = %s, stdout = %s", code, stderr, stdout)
	}
	stdout, stderr, code = runCLI(t, "note", "show", "Kitchen", "--json", "--config", cfgPath)
	if code != 0 {
		t.Fatalf("show exit = %d, stderr = %s", code, stderr)
	}
	if !strings.Contains(stdout, "Follow up Friday") {
		t.Errorf("show after append = %s", stdout)
	}

	f.mu.Lock()
	f.conflictPatch = true
	f.mu.Unlock()
	withStdin(t, "second write\n")
	_, _, code = runCLI(t, "note", "append", "Kitchen", "--stdin", "--json", "--config", cfgPath)
	if code != 5 {
		t.Errorf("conflicting append exit = %d, want 5", code)
	}
}

func TestNoteNewLivesInInbox(t *testing.T) {
	f := newFakeServer(t)
	cfgPath := writeConfig(t, f)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"default", []string{"note", "new", "Hello", "--body", "hi", "--json"}, "inbox/Hello.md"},
		{"relative folder", []string{"note", "new", "Trip", "--folder", "projects", "--body", "hi", "--json"}, "inbox/projects/Trip.md"},
		{"explicit folder", []string{"note", "new", "Plan", "--folder", "inbox/plans", "--body", "hi", "--json"}, "inbox/plans/Plan.md"},
		{"slash is inbox", []string{"note", "new", "Slash", "--folder", "/", "--body", "hi", "--json"}, "inbox/Slash.md"},
		{"explicit path", []string{"note", "new", "Deep", "--path", "inbox/deep/Deep.md", "--body", "hi", "--json"}, "inbox/deep/Deep.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := append(append([]string{}, tc.args...), "--config", cfgPath)
			stdout, stderr, code := runCLI(t, args...)
			if code != 0 {
				t.Fatalf("exit = %d, stderr = %s, stdout = %s", code, stderr, stdout)
			}
			f.mu.Lock()
			_, ok := f.notes[tc.want]
			f.mu.Unlock()
			if !ok {
				t.Fatalf("note was not created at %s", tc.want)
			}
		})
	}

	t.Run("path outside inbox", func(t *testing.T) {
		_, _, code := runCLI(t, "note", "new", "Root", "--path", "Root.md", "--json", "--config", cfgPath)
		if code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
		f.mu.Lock()
		_, ok := f.notes["Root.md"]
		f.mu.Unlock()
		if ok {
			t.Fatal("a note outside the inbox was created")
		}
	})
}

func TestAuthFailureExitCode(t *testing.T) {
	f := newFakeServer(t)
	_, _, code := runCLI(t, "whoami", "--json", "--server", f.url(), "--token", "lnt.invalid")
	if code != 4 {
		t.Errorf("whoami with bad token exit = %d, want 4", code)
	}
}

func TestAmbiguousRefExitsThree(t *testing.T) {
	f := newFakeServer(t)
	f.mu.Lock()
	f.notes["other/Kitchen.md"] = &client.Note{
		ID: "01ARZ3NDEKTSV4RRFFQ69G5FAX", Path: "other/Kitchen.md", Title: "Kitchen II",
		ETag: `"etag-other"`, Content: "# Kitchen II\n", Body: "Kitchen II\n",
		Tags: []string{}, Links: []client.Link{}, Backlinks: []client.Backlink{},
	}
	f.mu.Unlock()
	_, _, code := runCLI(t, "note", "show", "Kitchen", "--json",
		"--server", f.url(), "--token", f.token)
	if code != 3 {
		t.Errorf("ambiguous show exit = %d, want 3", code)
	}
}

func TestParseCommandFlagsAllowsFlagsAfterPositionals(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	limit := fs.Int("limit", 0, "")
	yes := fs.Bool("yes", false, "")
	args, err := parseCommandFlags(fs, []string{"ref", "--limit", "7", "--yes"})
	if err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 || args[0] != "ref" || *limit != 7 || !*yes {
		t.Fatalf("args = %v limit = %d yes = %v", args, *limit, *yes)
	}
}
