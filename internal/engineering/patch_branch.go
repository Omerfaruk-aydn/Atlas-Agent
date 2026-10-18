package engineering

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// PatchBranch isolates alternative text changes without a checkout or worktree.
// It owns its snapshots and notes; proposals never mutate the live project.
type PatchBranch struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Root      string       `json:"root"`
	TaskID    string       `json:"task_id,omitempty"`
	Files     []BranchFile `json:"files"`
	Notes     []string     `json:"notes,omitempty"`
	Status    string       `json:"status"`
	CreatedAt int64        `json:"created_at"`
}

type BranchFile struct {
	Path     string  `json:"path"`
	Base     string  `json:"base"`
	BaseHash string  `json:"base_hash"`
	Proposed *string `json:"proposed,omitempty"`
	Dispatch string  `json:"dispatch,omitempty"`
}

// ValidatePatchSources validates locations before invoking a read tool.
// This prevents malformed plans from reading outside their declared project.
func ValidatePatchSources(ctx context.Context, root string, paths []string) error {
	seen := map[string]bool{}
	for _, path := range paths {
		clean := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(path, `\`, "/")))
		if clean == "." || clean == ".." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.ContainsAny(clean, "*?:\x00") {
			return errors.New("source must be a literal project-relative file")
		}
		key := strings.ToLower(clean)
		if seen[key] {
			return errors.New("duplicate normalized source")
		}
		seen[key] = true
		current := root
		for _, segment := range strings.Split(clean, string(filepath.Separator)) {
			if err := ctx.Err(); err != nil {
				return err
			}
			current = filepath.Join(current, segment)
			info, err := os.Lstat(current)
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("source symlinks are not supported")
			}
		}
	}
	return nil
}

func (s *Store) ForkPatchBranch(ctx context.Context, sessionID, root, name, task string, paths []string) (PatchBranch, error) {
	if !validID(name) || len(paths) == 0 || len(paths) > 8 {
		return PatchBranch{}, errors.New("branch needs a valid name and 1-8 existing text files")
	}
	if err := ValidatePatchSources(ctx, root, paths); err != nil {
		return PatchBranch{}, err
	}
	b := PatchBranch{ID: "patch-" + Hash(sessionID + "\n" + name)[:24], Name: name, Root: root, TaskID: task, Status: "draft", CreatedAt: time.Now().UnixMilli()}
	seen := map[string]bool{}
	total := 0
	for _, path := range paths {
		if seen[path] {
			return b, errors.New("duplicate branch path")
		}
		seen[path] = true
		data, err := readProjectFile(ctx, root, path, 32*1024)
		if err != nil {
			return b, err
		}
		if len(data) == 0 || strings.ContainsRune(string(data), 0) {
			return b, errors.New("patch branches require nonempty text files; use an isolated workspace for binary/new files")
		}
		total += len(data)
		if total > 64*1024 {
			return b, errors.New("branch snapshots exceed 64 KiB")
		}
		b.Files = append(b.Files, BranchFile{Path: path, Base: string(data), BaseHash: Hash(string(data))})
	}
	err := s.Update(ctx, sessionID, func(st *State) error {
		for _, other := range st.Branches {
			if other.ID == b.ID {
				return errors.New("branch name already exists")
			}
		}
		if len(st.Branches) >= 4 {
			return errors.New("at most four patch branches per session")
		}
		st.Branches = append(st.Branches, b)
		return nil
	})
	return b, err
}

func (s *Store) ProposeBranch(ctx context.Context, sessionID, id string, files []BranchFile, notes []string) (PatchBranch, error) {
	var result PatchBranch
	if len(files) == 0 || len(files) > 8 || !methodTexts(notes, 0, 8) {
		return result, errors.New("proposal needs 1-8 files and at most eight bounded notes")
	}
	err := s.Update(ctx, sessionID, func(st *State) error {
		for i := range st.Branches {
			b := &st.Branches[i]
			if b.ID != id {
				continue
			}
			if b.Status != "draft" && b.Status != "proposed" {
				return errors.New("branch already submitted for merge; inspect outcomes instead of replaying")
			}
			seen := map[string]bool{}
			for _, f := range files {
				if f.Proposed == nil || len(*f.Proposed) > 32*1024 || strings.ContainsRune(*f.Proposed, 0) || seen[f.Path] {
					return errors.New("each proposed text must be bounded and unique")
				}
				seen[f.Path] = true
				found := false
				for j := range b.Files {
					if b.Files[j].Path == f.Path {
						b.Files[j].Proposed = f.Proposed
						found = true
					}
				}
				if !found {
					return errors.New("proposal path was not snapshotted")
				}
			}
			b.Notes = slices.Clone(notes)
			b.Status = "proposed"
			result = *b
			return nil
		}
		return errors.New("unknown patch branch")
	})
	return result, err
}

// BeginBranchMerge checks every base before any live write and journals dispatch.
func (s *Store) BeginBranchMerge(ctx context.Context, sessionID, root, id string) (PatchBranch, error) {
	var result PatchBranch
	err := s.Update(ctx, sessionID, func(st *State) error {
		for i := range st.Branches {
			b := &st.Branches[i]
			if b.ID != id {
				continue
			}
			if b.Root != root || b.Status != "proposed" {
				return errors.New("branch root/status changed; merge cannot replay")
			}
			changed := false
			for _, f := range b.Files {
				data, err := readProjectFile(ctx, root, f.Path, 32*1024)
				if err != nil || Hash(string(data)) != f.BaseHash {
					return fmt.Errorf("branch conflict at %s; inspect and fork a fresh branch", f.Path)
				}
				if f.Proposed != nil && *f.Proposed != f.Base {
					changed = true
				}
			}
			if !changed {
				return errors.New("branch has no proposed changes")
			}
			b.Status = "merging"
			result = *b
			return nil
		}
		return errors.New("unknown patch branch")
	})
	return result, err
}

func (s *Store) RecordBranchDispatch(ctx context.Context, sessionID, id, path, status string) error {
	return s.Update(ctx, sessionID, func(st *State) error {
		for i := range st.Branches {
			b := &st.Branches[i]
			if b.ID != id {
				continue
			}
			for j := range b.Files {
				if b.Files[j].Path == path {
					b.Files[j].Dispatch = status
					return nil
				}
			}
			return errors.New("unknown branch path")
		}
		return errors.New("unknown patch branch")
	})
}

func (s *Store) FinishBranchMerge(ctx context.Context, sessionID, id, status string) error {
	if status != "applied" && status != "interrupted" {
		return errors.New("invalid branch merge status")
	}
	return s.Update(ctx, sessionID, func(st *State) error {
		for i := range st.Branches {
			if st.Branches[i].ID == id {
				st.Branches[i].Status = status
				return nil
			}
		}
		return errors.New("unknown patch branch")
	})
}
