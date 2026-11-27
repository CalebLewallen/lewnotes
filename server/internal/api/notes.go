package api

import (
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"github.com/caleblewallen/lewnotes/server/internal/index"
	"github.com/caleblewallen/lewnotes/server/internal/notes"
	"github.com/caleblewallen/lewnotes/server/internal/search"
)

func notesAvailable(w http.ResponseWriter, r *http.Request, opts Options) bool {
	if opts.Notes == nil {
		writeProblem(w, r, http.StatusServiceUnavailable, "notes_unavailable", "The note service is not available.")
		return false
	}
	return true
}

func withOrigin(r *http.Request) *http.Request {
	origin := strings.TrimSpace(r.Header.Get("X-Lewnotes-Client"))
	return r.WithContext(notes.WithOrigin(r.Context(), origin))
}

func writeNote(w http.ResponseWriter, status int, note notes.Note) {
	w.Header().Set("ETag", note.ETag)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, note)
}

func writeNoteError(w http.ResponseWriter, r *http.Request, opts Options, err error) {
	var conflict *notes.ConflictError
	switch {
	case errors.Is(err, notes.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		writeProblem(w, r, http.StatusNotFound, "note_not_found", "No note or folder matches that id or path.")
	case errors.Is(err, notes.ErrExists):
		writeProblem(w, r, http.StatusConflict, "note_exists", "A file or folder already exists at that path.")
	case errors.Is(err, notes.ErrPreconditionRequired):
		writeProblem(w, r, http.StatusPreconditionRequired, "if_match_required",
			"Send If-Match with the note's current ETag.")
	case errors.As(err, &conflict):
		w.Header().Set("ETag", conflict.ETag)
		writeProblem(w, r, http.StatusPreconditionFailed, "etag_mismatch",
			"The note changed since your ETag. Refetch it and retry.")
	case errors.Is(err, notes.ErrNoteTooLarge):
		writeProblem(w, r, http.StatusRequestEntityTooLarge, "note_too_large", "The note exceeds the size limit.")
	case errors.Is(err, notes.ErrInvalidPath):
		writeProblem(w, r, http.StatusBadRequest, "invalid_path", err.Error())
	case errors.Is(err, notes.ErrInvalidNote):
		writeProblem(w, r, http.StatusBadRequest, "invalid_note", err.Error())
	default:
		opts.Logger.Error("note request failed", "error", err, "path", r.URL.Path)
		writeProblem(w, r, http.StatusInternalServerError, "note_failed", "The note operation failed.")
	}
}

func handleListNotes(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		query := r.URL.Query()
		limit, err := intParam(query.Get("limit"), 50)
		if err != nil || limit < 0 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_limit", "limit must be a non-negative integer.")
			return
		}
		offset := 0
		if cursor := query.Get("cursor"); cursor != "" {
			offset, err = notes.DecodeCursor(cursor)
			if err != nil {
				writeProblem(w, r, http.StatusBadRequest, "invalid_cursor", "cursor is not valid for this endpoint.")
				return
			}
		}
		result, err := opts.Notes.List(r.Context(), notes.ListOptions{
			PathPrefix: query.Get("path_prefix"),
			Tag:        query.Get("tag"),
			Sort:       query.Get("sort"),
			Limit:      limit,
			Offset:     offset,
		})
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleCreateNote(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		var req notes.CreateInput
		if err := decodeJSON(w, r, &req); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body",
				`Request body must be JSON like {"path":"note.md","content":"# Note"}.`)
			return
		}
		note, err := opts.Notes.Create(withOrigin(r).Context(), req)
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		w.Header().Set("Location", "/api/v1/notes/"+note.ID)
		writeNote(w, http.StatusCreated, note)
	}
}

func handleCreateDir(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Path) == "" {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body",
				`Request body must be JSON like {"path":"projects"}.`)
			return
		}
		created, err := opts.Notes.CreateDir(withOrigin(r).Context(), req.Path)
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]string{"path": created})
	}
}

func handleMoveDir(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		var req struct {
			Path         string `json:"path"`
			NewPath      string `json:"new_path"`
			RewriteLinks *bool  `json:"rewrite_links"`
		}
		if err := decodeJSON(w, r, &req); err != nil || strings.TrimSpace(req.Path) == "" || strings.TrimSpace(req.NewPath) == "" {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body",
				`Request body must be JSON like {"path":"projects","new_path":"archive/projects"}.`)
			return
		}
		rewrite := true
		if req.RewriteLinks != nil {
			rewrite = *req.RewriteLinks
		}
		result, err := opts.Notes.MoveDir(withOrigin(r).Context(), req.Path, req.NewPath, rewrite)
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleGetNote(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		note, err := opts.Notes.Read(r.Context(), r.PathValue("id"))
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		if r.URL.Query().Get("format") == "markdown" {
			if matchesETag(r.Header.Get("If-None-Match"), note.ETag) {
				w.Header().Set("ETag", note.ETag)
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Header().Set("ETag", note.ETag)
			w.Header().Set("Cache-Control", "no-store")
			_, _ = w.Write([]byte(note.Content))
			return
		}
		if matchesETag(r.Header.Get("If-None-Match"), note.ETag) {
			w.Header().Set("ETag", note.ETag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		writeNote(w, http.StatusOK, note)
	}
}

func handleNoteByPath(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			writeProblem(w, r, http.StatusBadRequest, "missing_path", "The path query parameter is required.")
			return
		}
		note, err := opts.Notes.ReadByPath(r.Context(), path)
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		if r.URL.Query().Get("format") == "markdown" {
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			w.Header().Set("ETag", note.ETag)
			_, _ = w.Write([]byte(note.Content))
			return
		}
		writeNote(w, http.StatusOK, note)
	}
}

func handleUpdateNote(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		var req struct {
			Content *string `json:"content"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body",
				`Request body must be JSON like {"content":"# Note"}.`)
			return
		}
		if req.Content == nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body", "content is required.")
			return
		}
		note, err := opts.Notes.Update(withOrigin(r).Context(), r.PathValue("id"),
			[]byte(*req.Content), r.Header.Get("If-Match"))
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		writeNote(w, http.StatusOK, note)
	}
}

func handlePatchNote(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		var req struct {
			Ops []notes.PatchOp `json:"ops"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body",
				`Request body must be JSON like {"ops":[{"type":"append","text":"more"}]}.`)
			return
		}
		note, err := opts.Notes.Patch(withOrigin(r).Context(), r.PathValue("id"),
			r.Header.Get("If-Match"), req.Ops)
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		writeNote(w, http.StatusOK, note)
	}
}

func handleMoveNote(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		var req struct {
			Path         string `json:"path"`
			RewriteLinks *bool  `json:"rewrite_links"`
		}
		if err := decodeJSON(w, r, &req); err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_body",
				`Request body must be JSON like {"path":"archive/note.md","rewrite_links":true}.`)
			return
		}
		rewrite := true
		if req.RewriteLinks != nil {
			rewrite = *req.RewriteLinks
		}
		result, err := opts.Notes.Move(withOrigin(r).Context(), r.PathValue("id"), req.Path, rewrite)
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		w.Header().Set("ETag", result.Note.ETag)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	}
}

func handleDeleteNote(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		result, err := opts.Notes.Delete(withOrigin(r).Context(), r.PathValue("id"), r.Header.Get("If-Match"))
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, result)
	}
}

func handleNoteLinks(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		note, err := opts.Notes.Read(r.Context(), r.PathValue("id"))
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"links": note.Links})
	}
}

func handleNoteBacklinks(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		note, err := opts.Notes.Read(r.Context(), r.PathValue("id"))
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"backlinks": note.Backlinks})
	}
}

func handleTree(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		depth, err := intParam(r.URL.Query().Get("depth"), 3)
		if err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_depth", "depth must be an integer.")
			return
		}
		tree, err := opts.Notes.Tree(r.Context(), r.URL.Query().Get("path"), depth)
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tree": tree})
	}
}

func handleFile(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		data, contentType, etag, err := opts.Notes.ReadFile(r.Context(), r.PathValue("path"))
		if err != nil {
			writeNoteError(w, r, opts, err)
			return
		}
		if matchesETag(r.Header.Get("If-None-Match"), etag) {
			w.Header().Set("ETag", etag)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		disposition := "attachment"
		if strings.HasPrefix(contentType, "image/") || contentType == "application/pdf" {
			disposition = "inline"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("ETag", etag)
		w.Header().Set("Content-Disposition", disposition)
		w.Header().Set("Cache-Control", "private, max-age=60")
		_, _ = w.Write(data)
	}
}

func handleSearch(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !notesAvailable(w, r, opts) {
			return
		}
		query, err := search.Parse(r.URL.Query().Get("q"))
		if err != nil {
			writeProblem(w, r, http.StatusBadRequest, "invalid_query", err.Error())
			return
		}
		if len(query.Is) > 1 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_query", "use one is: filter per search")
			return
		}
		if len(query.NegPaths) > 0 || len(query.NegHas) > 0 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_query",
				"negative path: and has: filters are not available in this build")
			return
		}
		limit, err := intParam(r.URL.Query().Get("limit"), 50)
		if err != nil || limit < 0 {
			writeProblem(w, r, http.StatusBadRequest, "invalid_limit", "limit must be a non-negative integer.")
			return
		}
		offset := 0
		if cursor := r.URL.Query().Get("cursor"); cursor != "" {
			offset, err = notes.DecodeCursor(cursor)
			if err != nil {
				writeProblem(w, r, http.StatusBadRequest, "invalid_cursor", "cursor is not valid for this endpoint.")
				return
			}
		}

		filter := index.SearchFilter{
			Match:  query.FTSMatch(),
			Limit:  limit,
			Offset: offset,
		}
		if len(query.Tags) > 0 {
			filter.Tags = query.Tags
		}
		if len(query.Paths) > 0 {
			filter.PathPrefix = query.Paths[0]
		}
		if len(query.Is) == 1 {
			filter.Resolution = query.Is[0]
		}
		if len(query.Has) == 1 {
			filter.Kind = "embed"
		}
		hits, total, err := opts.Index.Search(r.Context(), filter)
		if err != nil {
			opts.Logger.Error("search failed", "error", err)
			writeProblem(w, r, http.StatusInternalServerError, "search_failed", "The search failed.")
			return
		}
		status := map[string]any{"hits": hits, "total": total}
		if next := offset + len(hits); next < total {
			status["next_cursor"] = notes.EncodeCursor(next)
		}
		writeJSON(w, http.StatusOK, status)
	}
}

func handleMeta(opts Options) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		instance := ""
		if opts.Auth != nil {
			if name, err := opts.Auth.InstanceName(r.Context()); err == nil {
				instance = name
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"version":       opts.Version,
			"instance_name": instance,
			"limits": map[string]any{
				"max_note_bytes": notes.DefaultMaxBytes,
			},
			"features": []string{"notes", "wikilinks", "search", "tree", "directories", "sse"},
		})
	}
}

func intParam(raw string, fallback int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid integer %q", raw)
	}
	return value, nil
}

func matchesETag(header, etag string) bool {
	if header == "" {
		return false
	}
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == "*" || candidate == etag || strings.Trim(candidate, `"`) == strings.Trim(etag, `"`) {
			return true
		}
	}
	return false
}
