package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caleblewallen/lewnotes/server/internal/notes"
)

type noteResponse struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
	ETag  string `json:"etag"`
	Body  string `json:"body"`
	Links []struct {
		Target     string `json:"target"`
		Resolution string `json:"resolution"`
		Alias      string `json:"alias"`
	} `json:"links"`
	Backlinks []struct {
		Path string `json:"path"`
	} `json:"backlinks"`
}

func createNoteAPI(t *testing.T, client *testClient, path, content string) noteResponse {
	t.Helper()
	rec := client.do(http.MethodPost, "/api/v1/notes",
		`{"path":"`+path+`","content":`+mustJSON(t, content)+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %s = %d, body = %s", path, rec.Code, rec.Body.String())
	}
	if rec.Header().Get("ETag") == "" {
		t.Error("create response has no ETag header")
	}
	var note noteResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &note); err != nil {
		t.Fatal(err)
	}
	return note
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestNotesAPIFlow(t *testing.T) {
	env := newTestEnv(t, 10)
	client := env.anon()
	client.setup(t)

	hello := createNoteAPI(t, client, "inbox/Hello", "# Hello\n\nSee [[World]] and [[Missing]].\n")
	world := createNoteAPI(t, client, "World.md", "# World\n")

	// Create is idempotent only by conflict, never overwrite.
	if rec := client.do(http.MethodPost, "/api/v1/notes",
		`{"path":"World.md","content":"nope"}`); rec.Code != http.StatusConflict {
		t.Errorf("create existing = %d, want 409", rec.Code)
	}

	list := client.do(http.MethodGet, "/api/v1/notes", "")
	if list.Code != http.StatusOK {
		t.Fatalf("list = %d, body = %s", list.Code, list.Body.String())
	}
	var listed struct {
		Notes []struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		} `json:"notes"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Total != 2 || len(listed.Notes) != 2 {
		t.Errorf("list = %+v", listed)
	}

	got := client.do(http.MethodGet, "/api/v1/notes/"+hello.ID, "")
	if got.Code != http.StatusOK || got.Header().Get("ETag") != hello.ETag {
		t.Fatalf("get = %d, etag %q; body %s", got.Code, got.Header().Get("ETag"), got.Body.String())
	}
	var fetched noteResponse
	if err := json.Unmarshal(got.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	resolutions := map[string]string{}
	for _, link := range fetched.Links {
		resolutions[link.Target] = link.Resolution
	}
	if len(fetched.Links) != 2 || resolutions["World"] != "exact" || resolutions["Missing"] != "unresolved" {
		t.Errorf("links = %+v", fetched.Links)
	}

	byPath := client.do(http.MethodGet, "/api/v1/notes/by-path?path=inbox/Hello.md", "")
	if byPath.Code != http.StatusOK {
		t.Errorf("by-path = %d, body = %s", byPath.Code, byPath.Body.String())
	}
	markdown := client.do(http.MethodGet, "/api/v1/notes/"+hello.ID+"?format=markdown", "")
	if markdown.Code != http.StatusOK || !strings.HasPrefix(markdown.Header().Get("Content-Type"), "text/markdown") {
		t.Errorf("markdown = %d, %q", markdown.Code, markdown.Header().Get("Content-Type"))
	}

	backlinks := client.do(http.MethodGet, "/api/v1/notes/"+world.ID+"/backlinks", "")
	if backlinks.Code != http.StatusOK || !strings.Contains(backlinks.Body.String(), "inbox/Hello.md") {
		t.Errorf("backlinks = %d, %s", backlinks.Code, backlinks.Body.String())
	}

	// If-Match is mandatory and enforced.
	updateBody := `{"content":` + mustJSON(t, "---\nid: "+hello.ID+"\n---\n\n# Hello v2\n\nSee [[World]].\n") + `}`
	if rec := client.do(http.MethodPut, "/api/v1/notes/"+hello.ID, updateBody); rec.Code != http.StatusPreconditionRequired {
		t.Errorf("PUT without if-match = %d, want 428", rec.Code)
	}
	stale := client.doRaw(http.MethodPut, "/api/v1/notes/"+hello.ID, updateBody, map[string]string{"If-Match": `"deadbeef"`})
	if stale.Code != http.StatusPreconditionFailed || stale.Header().Get("ETag") != hello.ETag {
		t.Errorf("PUT stale = %d, etag %q", stale.Code, stale.Header().Get("ETag"))
	}
	ok := client.doRaw(http.MethodPut, "/api/v1/notes/"+hello.ID, updateBody, map[string]string{"If-Match": hello.ETag})
	if ok.Code != http.StatusOK {
		t.Fatalf("PUT = %d, body = %s", ok.Code, ok.Body.String())
	}
	var updated noteResponse
	if err := json.Unmarshal(ok.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ETag == hello.ETag {
		t.Error("ETag did not change")
	}

	patched := client.doRaw(http.MethodPatch, "/api/v1/notes/"+hello.ID,
		`{"ops":[{"type":"append","text":"patched"}]}`, map[string]string{"If-Match": updated.ETag})
	if patched.Code != http.StatusOK {
		t.Fatalf("PATCH = %d, body = %s", patched.Code, patched.Body.String())
	}
	var patchedNote noteResponse
	if err := json.Unmarshal(patched.Body.Bytes(), &patchedNote); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(patchedNote.Body, "patched") {
		t.Errorf("patched body = %q", patchedNote.Body)
	}

	moved := client.do(http.MethodPost, "/api/v1/notes/"+world.ID+"/move",
		`{"path":"archive/Renamed.md","rewrite_links":true}`)
	if moved.Code != http.StatusOK {
		t.Fatalf("move = %d, body = %s", moved.Code, moved.Body.String())
	}
	var moveResult notes.MoveResult
	if err := json.Unmarshal(moved.Body.Bytes(), &moveResult); err != nil {
		t.Fatal(err)
	}
	if !moveResult.Moved || moveResult.LinksRewritten != 1 || moveResult.Note.Path != "archive/Renamed.md" {
		t.Errorf("move result = %+v", moveResult)
	}
	afterMove := client.do(http.MethodGet, "/api/v1/notes/"+hello.ID, "")
	if !strings.Contains(afterMove.Body.String(), "[[Renamed]]") {
		t.Errorf("hello was not rewritten: %s", afterMove.Body.String())
	}

	tree := client.do(http.MethodGet, "/api/v1/tree?depth=5", "")
	if tree.Code != http.StatusOK || !strings.Contains(tree.Body.String(), `"inbox"`) {
		t.Errorf("tree = %d, %s", tree.Code, tree.Body.String())
	}

	search := client.do(http.MethodGet, "/api/v1/search?q=world", "")
	if search.Code != http.StatusOK || !strings.Contains(search.Body.String(), `"path":"archive/Renamed.md"`) {
		t.Errorf("search = %d, %s", search.Code, search.Body.String())
	}
	if bad := client.do(http.MethodGet, "/api/v1/search?q=bogus:value", ""); bad.Code != http.StatusBadRequest {
		t.Errorf("search unknown filter = %d, want 400", bad.Code)
	}

	deleted := client.doRaw(http.MethodDelete, "/api/v1/notes/"+world.ID, "",
		map[string]string{"If-Match": moveResult.Note.ETag})
	if deleted.Code != http.StatusOK || !strings.Contains(deleted.Body.String(), ".lewnotes/trash/") {
		t.Fatalf("delete = %d, %s", deleted.Code, deleted.Body.String())
	}
	if rec := client.do(http.MethodGet, "/api/v1/notes/"+world.ID, ""); rec.Code != http.StatusNotFound {
		t.Errorf("get deleted = %d, want 404", rec.Code)
	}
}

func TestDirectoriesAPIFlow(t *testing.T) {
	env := newTestEnv(t, 10)
	client := env.anon()
	client.setup(t)

	created := client.do(http.MethodPost, "/api/v1/dirs", `{"path":"projects"}`)
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), `"path":"projects"`) {
		t.Fatalf("create dir = %d, %s", created.Code, created.Body.String())
	}
	if duplicate := client.do(http.MethodPost, "/api/v1/dirs", `{"path":"projects"}`); duplicate.Code != http.StatusConflict {
		t.Errorf("duplicate dir = %d, want 409", duplicate.Code)
	}
	if invalid := client.do(http.MethodPost, "/api/v1/dirs", `{"path":"../escape"}`); invalid.Code != http.StatusBadRequest {
		t.Errorf("invalid dir = %d, want 400", invalid.Code)
	}

	a := createNoteAPI(t, client, "projects/A.md", "# A\n")
	client.do(http.MethodPost, "/api/v1/notes",
		`{"path":"outside.md","content":`+mustJSON(t, "# Outside\n\nSee [[projects/A]].\n")+`}`)

	moved := client.do(http.MethodPost, "/api/v1/dirs/move",
		`{"path":"projects","new_path":"archive/projects","rewrite_links":true}`)
	if moved.Code != http.StatusOK {
		t.Fatalf("move dir = %d, %s", moved.Code, moved.Body.String())
	}
	var result notes.DirMoveResult
	if err := json.Unmarshal(moved.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Moved || result.NotesMoved != 1 || result.LinksRewritten != 1 || result.Path != "archive/projects" {
		t.Errorf("move dir result = %+v", result)
	}

	afterMove := client.do(http.MethodGet, "/api/v1/notes/"+a.ID, "")
	if afterMove.Code != http.StatusOK || !strings.Contains(afterMove.Body.String(), `"path":"archive/projects/A.md"`) {
		t.Errorf("moved note = %d, %s", afterMove.Code, afterMove.Body.String())
	}
	outside := client.do(http.MethodGet, "/api/v1/notes/by-path?path=outside.md", "")
	if outside.Code != http.StatusOK || !strings.Contains(outside.Body.String(), "[[archive/projects/A]]") {
		t.Errorf("outside rewrite = %d, %s", outside.Code, outside.Body.String())
	}

	tree := client.do(http.MethodGet, "/api/v1/tree?depth=5", "")
	if tree.Code != http.StatusOK || !strings.Contains(tree.Body.String(), `"archive"`) || strings.Contains(tree.Body.String(), `"path":"projects"`) {
		t.Errorf("tree after dir move = %d, %s", tree.Code, tree.Body.String())
	}

	intoItself := client.do(http.MethodPost, "/api/v1/dirs/move",
		`{"path":"archive/projects","new_path":"archive/projects/nested"}`)
	if intoItself.Code != http.StatusBadRequest {
		t.Errorf("move dir into itself = %d, want 400", intoItself.Code)
	}
}

func TestUnknownAPIRouteAfterAuthIsProblemJSON(t *testing.T) {
	env := newTestEnv(t, 10)
	client := env.anon()
	client.setup(t)

	rec := client.do(http.MethodGet, "/api/v1/no-such-route", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown API = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("content type = %q, want problem+json", ct)
	}
	if !strings.Contains(rec.Body.String(), `"status":404`) {
		t.Fatalf("body = %s, want a 404 problem", rec.Body.String())
	}
}

func TestEventsReplayOverHTTP(t *testing.T) {
	env := newTestEnv(t, 10)
	client := env.anon()
	client.setup(t)

	first := env.hub.Publish("note.created", map[string]string{"id": "a"})
	second := env.hub.Publish("note.updated", map[string]string{"id": "a"})

	server := httptest.NewServer(env.handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(client.session)
	req.Header.Set("Last-Event-ID", first.ID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("SSE = %d, %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	reader := bufio.NewReader(resp.Body)
	seen := ""
	for i := 0; i < 6; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read SSE: %v (saw %q)", err, seen)
		}
		seen += line
		if strings.Contains(seen, "event: note.updated") && strings.Contains(seen, `"id":"a"`) {
			break
		}
	}
	if !strings.Contains(seen, "id: "+second.ID) || !strings.Contains(seen, `"id":"a"`) {
		t.Fatalf("replayed SSE = %q", seen)
	}
}
