package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/caleblewallen/lewnotes/cli/client"
)

var ulidPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

// resolveNote implements the documented ref order: exact id, exact path,
// unique path suffix, then unique title. Ambiguity is a first-class error.
func resolveNote(ctx context.Context, c *client.Client, ref string) (client.Note, error) {
	ref = strings.TrimSpace(strings.TrimPrefix(ref, "/"))
	if ref == "" {
		return client.Note{}, &usageError{message: "note reference is required"}
	}

	if ulidPattern.MatchString(ref) {
		note, err := c.GetNote(ctx, ref)
		if err == nil {
			return note, nil
		}
		if !isNotFound(err) {
			return client.Note{}, err
		}
	}

	if strings.Contains(ref, "/") || strings.HasSuffix(strings.ToLower(ref), ".md") {
		for _, candidate := range pathCandidates(ref) {
			note, err := c.GetNoteByPath(ctx, candidate)
			if err == nil {
				return note, nil
			}
			if !isNotFound(err) {
				return client.Note{}, err
			}
		}
	}

	matches := map[string]client.NoteSummary{}
	cursor := ""
	for page := 0; page < 50; page++ {
		list, err := c.ListNotes(ctx, client.ListOptions{Limit: 200, Cursor: cursor})
		if err != nil {
			return client.Note{}, err
		}
		for _, note := range list.Notes {
			if matchesRef(note, ref) {
				matches[note.ID] = note
			}
		}
		if list.NextCursor == "" {
			break
		}
		cursor = list.NextCursor
	}

	if len(matches) == 0 {
		return client.Note{}, fmt.Errorf("no note matches %q", ref)
	}
	if len(matches) > 1 {
		candidates := make([]string, 0, len(matches))
		for _, note := range matches {
			candidates = append(candidates, note.Path)
		}
		sort.Strings(candidates)
		return client.Note{}, &ambiguousError{Ref: ref, Candidates: candidates}
	}
	for id := range matches {
		return c.GetNote(ctx, id)
	}
	return client.Note{}, fmt.Errorf("no note matches %q", ref)
}

func pathCandidates(ref string) []string {
	ref = strings.TrimPrefix(strings.ReplaceAll(ref, "\\", "/"), "/")
	candidates := []string{ref}
	if !strings.HasSuffix(strings.ToLower(ref), ".md") {
		candidates = append(candidates, ref+".md")
	}
	return candidates
}

func matchesRef(note client.NoteSummary, ref string) bool {
	target := strings.TrimSuffix(strings.TrimPrefix(ref, "/"), ".md")
	path := strings.TrimSuffix(note.Path, ".md")
	if strings.EqualFold(path, target) {
		return true
	}
	if strings.HasSuffix(strings.ToLower(path), "/"+strings.ToLower(target)) {
		return true
	}
	return strings.EqualFold(note.Title, target)
}

func isNotFound(err error) bool {
	var apiErr *client.APIError
	return errors.As(err, &apiErr) && apiErr.Status == 404
}
