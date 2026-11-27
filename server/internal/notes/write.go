package notes

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/caleblewallen/lewnotes/server/internal/index"
	"github.com/caleblewallen/lewnotes/server/internal/links"
	"github.com/caleblewallen/lewnotes/server/internal/markdown"
)

// Update replaces a note's content. If-Match is mandatory.
func (s *Service) Update(ctx context.Context, id string, content []byte, ifMatch string) (Note, error) {
	row, err := s.noteRow(ctx, id)
	if err != nil {
		return Note{}, err
	}
	current, info, err := s.readFile(row.Path)
	if err != nil {
		return Note{}, err
	}
	return s.write(ctx, row, current, info, content, ifMatch)
}

func (s *Service) write(ctx context.Context, row index.Note, current []byte, info fs.FileInfo, content []byte, ifMatch string) (Note, error) {
	if err := CheckIfMatch(ifMatch, current); err != nil {
		return Note{}, err
	}
	if int64(len(content)) > s.maxBytes {
		return Note{}, ErrNoteTooLarge
	}

	doc, err := markdown.Parse(content)
	if err != nil {
		return Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
	}
	switch {
	case doc.ID() == "":
		content, _, err = markdown.EnsureID(content, row.ID)
		if err != nil {
			return Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
		}
	case doc.ID() != row.ID:
		return Note{}, fmt.Errorf("%w: the note id is server-owned", ErrInvalidNote)
	}

	if err := s.vault.WriteAtomic(row.Path, content, info.Mode().Perm()); err != nil {
		return Note{}, err
	}
	fresh, err := s.vault.Stat(row.Path)
	if err != nil {
		return Note{}, err
	}
	updated, err := s.reindex(ctx, row.Path, content, fresh)
	if err != nil {
		return Note{}, err
	}
	note, err := s.view(ctx, updated, content)
	if err != nil {
		return Note{}, err
	}
	s.publish(ctx, "note.updated", note)
	return note, nil
}

// PatchOp is one cheap edit operation.
type PatchOp struct {
	Type        string         `json:"type"`
	Text        string         `json:"text,omitempty"`
	Start       *int           `json:"start,omitempty"`
	End         *int           `json:"end,omitempty"`
	Frontmatter map[string]any `json:"frontmatter,omitempty"`
}

// Patch applies byte/body operations and writes once, preserving the note id.
func (s *Service) Patch(ctx context.Context, id, ifMatch string, ops []PatchOp) (Note, error) {
	row, err := s.noteRow(ctx, id)
	if err != nil {
		return Note{}, err
	}
	current, info, err := s.readFile(row.Path)
	if err != nil {
		return Note{}, err
	}
	if err := CheckIfMatch(ifMatch, current); err != nil {
		return Note{}, err
	}
	if len(ops) == 0 {
		return s.view(ctx, row, current)
	}

	doc, err := markdown.Parse(current)
	if err != nil {
		return Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
	}
	body := append([]byte(nil), doc.Body...)
	prefix := current[:len(current)-len(doc.Body)]
	content := current
	frontmatter := doc.Frontmatter

	for i, op := range ops {
		switch op.Type {
		case "append":
			if len(body) > 0 && body[len(body)-1] != '\n' {
				body = append(body, '\n')
			}
			body = append(body, op.Text...)
		case "prepend":
			addition := []byte(op.Text)
			if len(addition) > 0 && addition[len(addition)-1] != '\n' {
				addition = append(addition, '\n')
			}
			body = append(addition, body...)
		case "replace_range":
			if op.Start == nil || op.End == nil {
				return Note{}, fmt.Errorf("%w: op %d needs start and end", ErrInvalidNote, i)
			}
			if *op.Start < 0 || *op.End < *op.Start || *op.End > len(current) {
				return Note{}, fmt.Errorf("%w: op %d range is out of bounds", ErrInvalidNote, i)
			}
			content = append(append(append([]byte(nil), current[:*op.Start]...), op.Text...), current[*op.End:]...)
			doc, err = markdown.Parse(content)
			if err != nil {
				return Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
			}
			prefix = content[:len(content)-len(doc.Body)]
			body = append([]byte(nil), doc.Body...)
			frontmatter = doc.Frontmatter
		case "set_frontmatter":
			if frontmatter == nil {
				frontmatter = map[string]any{}
			}
			for key, value := range op.Frontmatter {
				frontmatter[key] = value
			}
			encoded, err := yaml.Marshal(frontmatter)
			if err != nil {
				return Note{}, fmt.Errorf("%w: %v", ErrInvalidNote, err)
			}
			prefix = []byte("---\n" + string(encoded) + "---\n")
			body = append([]byte(nil), doc.Body...)
		default:
			return Note{}, fmt.Errorf("%w: unknown op %q", ErrInvalidNote, op.Type)
		}
		if op.Type != "replace_range" && op.Type != "set_frontmatter" {
			content = append(append([]byte(nil), prefix...), body...)
		}
	}

	return s.write(ctx, row, current, info, content, ifMatch)
}

// MoveResult reports a rename and any partial link-rewrite failures.
type MoveResult struct {
	Moved          bool     `json:"moved"`
	Note           Note     `json:"note"`
	LinksRewritten int      `json:"links_rewritten"`
	Warnings       []string `json:"warnings,omitempty"`
}

// DirMoveResult reports a directory rename and the notes carried with it.
type DirMoveResult struct {
	Moved          bool     `json:"moved"`
	Path           string   `json:"path"`
	NotesMoved     int      `json:"notes_moved"`
	LinksRewritten int      `json:"links_rewritten"`
	Warnings       []string `json:"warnings,omitempty"`
}

// CreateDir creates a directory inside the vault.
func (s *Service) CreateDir(ctx context.Context, rel string) (string, error) {
	clean, err := CleanDirPath(rel)
	if err != nil {
		return "", err
	}
	if _, err := s.vault.Stat(clean); err == nil {
		return "", ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := s.vault.EnsureDir(clean); err != nil {
		return "", err
	}
	s.publishTreeChange(ctx, clean, "")
	return clean, nil
}

// MoveDir renames a directory, reindexes every note it contains, and rewrites
// full-path wikilinks that pointed into the old directory. Basename links do
// not need rewriting because a directory rename does not change basenames.
func (s *Service) MoveDir(ctx context.Context, rel, newRel string, rewrite bool) (DirMoveResult, error) {
	oldClean, err := CleanDirPath(rel)
	if err != nil {
		return DirMoveResult{}, err
	}
	newClean, err := CleanDirPath(newRel)
	if err != nil {
		return DirMoveResult{}, err
	}
	if oldClean == newClean {
		return DirMoveResult{Path: oldClean}, nil
	}
	if strings.HasPrefix(newClean+"/", oldClean+"/") {
		return DirMoveResult{}, fmt.Errorf("%w: a directory cannot move inside itself", ErrInvalidPath)
	}
	if !s.inAttachments(oldClean) && s.inAttachments(newClean) {
		return DirMoveResult{}, fmt.Errorf("%w: notes cannot be moved into %s; it holds uploaded files", ErrInvalidPath, s.attachments)
	}

	info, err := s.vault.Stat(oldClean)
	if errors.Is(err, fs.ErrNotExist) {
		return DirMoveResult{}, ErrNotFound
	}
	if err != nil {
		return DirMoveResult{}, err
	}
	if !info.IsDir() {
		return DirMoveResult{}, fmt.Errorf("%w: %s is not a directory", ErrInvalidPath, oldClean)
	}
	if _, err := s.vault.Stat(newClean); err == nil {
		return DirMoveResult{}, ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return DirMoveResult{}, err
	}

	all, err := s.index.AllNotes(ctx)
	if err != nil {
		return DirMoveResult{}, err
	}
	type movedNote struct {
		id      string
		oldPath string
		newPath string
	}
	moved := make([]movedNote, 0)
	for _, note := range all {
		if !strings.HasPrefix(note.Path, oldClean+"/") {
			continue
		}
		suffix := strings.TrimPrefix(note.Path, oldClean+"/")
		moved = append(moved, movedNote{
			id:      note.ID,
			oldPath: note.Path,
			newPath: newClean + "/" + suffix,
		})
	}

	if err := s.vault.Rename(oldClean, newClean); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return DirMoveResult{}, ErrExists
		}
		return DirMoveResult{}, err
	}

	result := DirMoveResult{Moved: true, Path: newClean, NotesMoved: len(moved), Warnings: []string{}}
	for _, note := range moved {
		if err := s.index.MoveNotePath(ctx, note.id, note.newPath); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("move index %s: %v", note.oldPath, err))
			continue
		}
		data, fresh, err := s.readFile(note.newPath)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("read %s: %v", note.newPath, err))
			continue
		}
		if _, err := s.reindex(ctx, note.newPath, data, fresh); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("reindex %s: %v", note.newPath, err))
		}
	}

	if rewrite && len(moved) > 0 {
		refs, err := s.index.AllNotes(ctx)
		if err != nil {
			result.Warnings = append(result.Warnings, "list notes for link rewrite: "+err.Error())
		} else {
			for _, ref := range refs {
				data, fresh, err := s.readFile(ref.Path)
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("read %s: %v", ref.Path, err))
					continue
				}
				updated := data
				changed := false
				for _, note := range moved {
					var rewritten []byte
					var noteChanged bool
					// The basenames are unchanged, so full-path forms must be
					// eligible even when a basename is ambiguous.
					rewritten, noteChanged = RewriteLinkTargets(updated, note.oldPath, note.newPath, true, true)
					if noteChanged {
						updated = rewritten
						changed = true
					}
				}
				if !changed {
					continue
				}
				if err := s.vault.WriteAtomic(ref.Path, updated, fresh.Mode().Perm()); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("rewrite %s: %v", ref.Path, err))
					continue
				}
				fresh, err = s.vault.Stat(ref.Path)
				if err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("stat %s: %v", ref.Path, err))
					continue
				}
				if _, err := s.reindex(ctx, ref.Path, updated, fresh); err != nil {
					result.Warnings = append(result.Warnings, fmt.Sprintf("reindex %s: %v", ref.Path, err))
					continue
				}
				result.LinksRewritten++
			}
		}
	}

	s.reResolveLinks(ctx)
	s.publishTreeChange(ctx, oldClean, newClean)
	return result, nil
}

func (s *Service) publishTreeChange(ctx context.Context, pathValue, newPath string) {
	if s.events == nil {
		return
	}
	data := map[string]any{
		"path":   pathValue,
		"origin": OriginFromContext(ctx),
	}
	if newPath != "" {
		data["new_path"] = newPath
	}
	s.events.Publish("tree.changed", data)
}

// Move renames a note and rewrites inbound wikilinks in the same operation.
func (s *Service) Move(ctx context.Context, id, newPath string, rewrite bool) (MoveResult, error) {
	row, err := s.noteRow(ctx, id)
	if err != nil {
		return MoveResult{}, err
	}
	clean, err := CleanPath(newPath)
	if err != nil {
		return MoveResult{}, err
	}
	if s.inAttachments(clean) {
		return MoveResult{}, fmt.Errorf("%w: notes cannot be moved into %s; it holds uploaded files", ErrInvalidPath, s.attachments)
	}
	oldPath := row.Path
	if clean == oldPath {
		note, err := s.Read(ctx, id)
		return MoveResult{Note: note}, err
	}
	if _, err := s.vault.Stat(clean); err == nil {
		return MoveResult{}, ErrExists
	} else if !errors.Is(err, fs.ErrNotExist) {
		return MoveResult{}, err
	}

	data, _, err := s.readFile(row.Path)
	if err != nil {
		return MoveResult{}, err
	}

	all, err := s.index.AllNotes(ctx)
	if err != nil {
		return MoveResult{}, err
	}
	oldBase := path.Base(oldPath)
	newBase := path.Base(clean)
	oldUnique := basenameCount(all, oldBase) == 1
	newUnique := basenameCountExcluding(all, newBase, id) == 0

	backlinks, err := s.index.BacklinksTo(ctx, id)
	if err != nil {
		return MoveResult{}, err
	}
	sources := make([]string, 0, len(backlinks))
	seen := map[string]bool{}
	for _, backlink := range backlinks {
		if seen[backlink.ID] {
			continue
		}
		seen[backlink.ID] = true
		sources = append(sources, backlink.ID)
	}

	if err := s.vault.Rename(oldPath, clean); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return MoveResult{}, ErrExists
		}
		return MoveResult{}, err
	}
	if err := s.index.MoveNotePath(ctx, id, clean); err != nil {
		return MoveResult{}, err
	}
	info, err := s.vault.Stat(clean)
	if err != nil {
		return MoveResult{}, err
	}
	row.Path = clean
	row.Mtime = info.ModTime().Unix()
	if err := s.index.UpsertNote(ctx, row); err != nil {
		return MoveResult{}, err
	}

	result := MoveResult{Moved: true, Warnings: []string{}}
	if rewrite {
		for _, sourceID := range sources {
			source, err := s.noteRow(ctx, sourceID)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("read %s: %v", sourceID, err))
				continue
			}
			sourceData, sourceInfo, err := s.readFile(source.Path)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("read %s: %v", source.Path, err))
				continue
			}
			rewritten, changed := RewriteLinkTargets(sourceData, oldPath, clean, oldUnique, newUnique)
			if !changed {
				continue
			}
			if err := s.vault.WriteAtomic(source.Path, rewritten, sourceInfo.Mode().Perm()); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("rewrite %s: %v", source.Path, err))
				continue
			}
			fresh, err := s.vault.Stat(source.Path)
			if err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("stat %s: %v", source.Path, err))
				continue
			}
			if _, err := s.reindex(ctx, source.Path, rewritten, fresh); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("reindex %s: %v", source.Path, err))
				continue
			}
			result.LinksRewritten++
		}
	}

	if err := s.refreshLinks(ctx, id, data); err != nil {
		result.Warnings = append(result.Warnings, "reindex moved note: "+err.Error())
	}
	// A rename changes resolution for notes that did not point at the moved
	// note directly (for example, ambiguous suffixes that now have one match).
	s.reResolveLinks(ctx)
	note, err := s.Read(ctx, id)
	if err != nil {
		return MoveResult{}, err
	}
	result.Note = note
	s.publish(ctx, "note.moved", note)
	return result, nil
}

// DeleteResult reports a soft delete.
type DeleteResult struct {
	Deleted   bool   `json:"deleted"`
	TrashPath string `json:"trash_path"`
}

// Delete moves a note to the trash. If-Match is mandatory.
func (s *Service) Delete(ctx context.Context, id, ifMatch string) (DeleteResult, error) {
	row, err := s.noteRow(ctx, id)
	if err != nil {
		return DeleteResult{}, err
	}
	data, _, err := s.readFile(row.Path)
	if err != nil {
		return DeleteResult{}, err
	}
	if err := CheckIfMatch(ifMatch, data); err != nil {
		return DeleteResult{}, err
	}
	trashPath, err := s.vault.MoveToTrash(row.Path)
	if err != nil {
		return DeleteResult{}, err
	}
	if err := s.index.DeleteNote(ctx, row.ID); err != nil {
		return DeleteResult{}, err
	}
	s.reResolveLinks(ctx)
	if s.events != nil {
		s.events.Publish("note.deleted", map[string]any{
			"id":     row.ID,
			"path":   row.Path,
			"trash":  trashPath,
			"origin": OriginFromContext(ctx),
		})
	}
	return DeleteResult{Deleted: true, TrashPath: trashPath}, nil
}

// TreeNode is one sidebar entry.
type TreeNode struct {
	Name     string     `json:"name"`
	Path     string     `json:"path"`
	Type     string     `json:"type"`
	ID       string     `json:"id,omitempty"`
	Size     int64      `json:"size,omitempty"`
	ModTime  string     `json:"modified,omitempty"`
	Children []TreeNode `json:"children,omitempty"`
}

// Tree lists the vault starting at rel, recursing up to depth levels (-1 is
// unlimited). Non-markdown files are included so embeds can be browsed.
func (s *Service) Tree(ctx context.Context, rel string, depth int) ([]TreeNode, error) {
	if depth == 0 {
		depth = 3
	}
	if depth < -1 {
		depth = -1
	}
	if depth > 10 {
		depth = 10
	}
	clean := strings.Trim(strings.ReplaceAll(rel, `\`, "/"), "/")
	if clean != "" {
		clean = path.Clean(clean)
		if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, ".") {
			return nil, ErrInvalidPath
		}
	}

	ids := map[string]string{}
	if all, err := s.index.AllNotes(ctx); err == nil {
		for _, note := range all {
			ids[note.Path] = note.ID
		}
	}

	entries, err := s.vault.List(clean)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	nodes := make([]TreeNode, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name, ".") {
			continue
		}
		childPath := entry.Name
		if clean != "" {
			childPath = clean + "/" + entry.Name
		}
		node := TreeNode{
			Name:    entry.Name,
			Path:    childPath,
			Size:    entry.Size,
			ModTime: time.Unix(entry.ModTime, 0).UTC().Format(time.RFC3339),
		}
		if entry.IsDir {
			node.Type = "dir"
			if depth != 0 && (depth < 0 || depth > 1) {
				children, err := s.Tree(ctx, childPath, depth-1)
				if err != nil {
					node.Children = []TreeNode{}
				} else {
					node.Children = children
				}
			}
		} else {
			node.Type = fileType(entry.Name)
			node.ID = ids[childPath]
		}
		nodes = append(nodes, node)
	}

	// Notes and other files sort before folders so a folder never sits above
	// the notes that belong to its parent.
	sort.SliceStable(nodes, func(i, j int) bool {
		iDir := nodes[i].Type == "dir"
		jDir := nodes[j].Type == "dir"
		if iDir != jDir {
			return !iDir
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	return nodes, nil
}

func fileType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".md":
		return "note"
	case ".excalidraw":
		return "board"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".avif", ".pdf":
		return "image"
	default:
		return "file"
	}
}

func basenameCount(notes []index.NoteRef, base string) int {
	want := links.Normalize(base)
	count := 0
	for _, note := range notes {
		if links.Normalize(path.Base(note.Path)) == want {
			count++
		}
	}
	return count
}

func basenameCountExcluding(notes []index.NoteRef, base, id string) int {
	want := links.Normalize(base)
	count := 0
	for _, note := range notes {
		if note.ID != id && links.Normalize(path.Base(note.Path)) == want {
			count++
		}
	}
	return count
}

// CheckIfMatch compares an If-Match header against the content's ETag.
func CheckIfMatch(ifMatch string, data []byte) error {
	if strings.TrimSpace(ifMatch) == "" {
		return ErrPreconditionRequired
	}
	current := ETag(data)
	for _, candidate := range strings.Split(ifMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" {
			return nil
		}
		candidate = strings.TrimPrefix(candidate, "W/")
		candidate = strings.Trim(candidate, `"`)
		if candidate == strings.Trim(current, `"`) {
			return nil
		}
	}
	return &ConflictError{ETag: current}
}

// RewriteLinkTargets rewrites wikilink targets in content that point at oldPath.
// Bare basename links are only touched when the old basename was unique, and
// they become a full path when the new basename is not unique.
func RewriteLinkTargets(content []byte, oldPath, newPath string, oldBaseUnique, newBaseUnique bool) ([]byte, bool) {
	oldNoExt := strings.TrimSuffix(oldPath, path.Ext(oldPath))
	newNoExt := strings.TrimSuffix(newPath, path.Ext(newPath))
	oldBaseNoExt := path.Base(oldNoExt)
	newBaseNoExt := path.Base(newNoExt)

	type form struct {
		old      string
		new      string
		requires bool // bare forms need oldBaseUnique
	}
	forms := make([]form, 0, 4)
	add := func(old, replacement string, bare bool) {
		for i := range forms {
			if forms[i].old != old {
				continue
			}
			if bare {
				forms[i].requires = true
				forms[i].new = replacement
			}
			return
		}
		forms = append(forms, form{old: old, new: replacement, requires: bare})
	}
	add(oldPath, newPath, false)
	// A root-level note's path and basename are the same string; the bare
	// rule must win so [[Note]] stays bare instead of gaining a path.
	add(oldNoExt, newNoExt, strings.Contains(oldNoExt, "/"))
	if !strings.Contains(oldNoExt, "/") {
		forms[len(forms)-1].new = newBaseNoExt
		forms[len(forms)-1].requires = true
	}
	add(oldBaseNoExt+path.Ext(oldPath), newBaseNoExt, true)
	add(oldBaseNoExt, newBaseNoExt, true)
	if !newBaseUnique {
		for i := range forms {
			if forms[i].requires {
				forms[i].new = newNoExt
			}
		}
	}

	out := append([]byte(nil), content...)
	changed := false
	for i := 0; i+1 < len(out); i++ {
		if out[i] != '[' || out[i+1] != '[' {
			continue
		}
		end := -1
		for j := i + 2; j+1 < len(out); j++ {
			if out[j] == '\n' {
				break
			}
			if out[j] == ']' && out[j+1] == ']' {
				end = j
				break
			}
		}
		if end < 0 {
			continue
		}
		innerStart := i + 2
		inner := string(out[innerStart:end])
		targetEnd := len(inner)
		if idx := strings.IndexAny(inner, "|#"); idx >= 0 {
			targetEnd = idx
		}
		target := strings.TrimSpace(inner[:targetEnd])
		replacement := ""
		for _, f := range forms {
			if target != f.old {
				continue
			}
			if f.requires && !oldBaseUnique {
				continue
			}
			if f.new != target {
				replacement = f.new
			}
			break
		}
		if replacement == "" {
			i = end
			continue
		}
		newInner := replacement + inner[targetEnd:]
		out = append(out[:innerStart], append([]byte(newInner), out[end:]...)...)
		changed = true
		i = innerStart + len(newInner) - 1
	}
	return out, changed
}
