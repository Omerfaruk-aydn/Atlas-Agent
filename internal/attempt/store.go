package attempt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

const reportFile = "report.json"

var idPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

// ValidID reports whether id has the shape of a run ID. Run IDs become
// directory names, so anything else is rejected before touching the disk.
func ValidID(id string) bool {
	return idPattern.MatchString(id)
}

func runPath(stateDir, id string) (string, error) {
	if !ValidID(id) {
		return "", fmt.Errorf("invalid attempt run ID %q", id)
	}
	return filepath.Join(stateDir, id), nil
}

// Save writes the report atomically.
func Save(stateDir string, r *Report) error {
	dir, err := runPath(stateDir, r.ID)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".report-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, reportFile)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// Load reads a stored report.
func Load(stateDir, id string) (*Report, error) {
	dir, err := runPath(stateDir, id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, reportFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("no attempt run %q", id)
		}
		return nil, err
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("attempt run %s is unreadable: %w", id, err)
	}
	return &r, nil
}

// List returns stored reports, newest first. Unreadable runs are skipped.
func List(stateDir string) ([]*Report, error) {
	entries, err := os.ReadDir(stateDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Report
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		if r, err := Load(stateDir, e.Name()); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Apply applies one attempt's stored patch to the tree at root. pick is the
// attempt index; 0 means the report's winner. It refuses when the run has
// already been applied, or when the patch no longer applies cleanly.
func Apply(ctx context.Context, stateDir, id string, pick int) (*Report, *Attempt, error) {
	r, err := Load(stateDir, id)
	if err != nil {
		return nil, nil, err
	}
	if r.Applied != 0 {
		return r, nil, fmt.Errorf("attempt %d of run %s was already applied at %s", r.Applied, id, r.AppliedAt.Format(time.RFC3339))
	}
	if pick == 0 {
		pick = r.Winner
	}
	if pick == 0 {
		return r, nil, errors.New("this run has no winner; choose an attempt explicitly")
	}
	if pick < 1 || pick > len(r.Attempts) {
		return r, nil, fmt.Errorf("run %s has no attempt %d", id, pick)
	}
	a := &r.Attempts[pick-1]
	if a.PatchFile == "" {
		return r, a, fmt.Errorf("attempt %s produced no patch (status %s)", a.Label, a.Status)
	}
	dir, err := runPath(stateDir, id)
	if err != nil {
		return r, a, err
	}
	patch, err := os.ReadFile(filepath.Join(dir, filepath.Base(a.PatchFile)))
	if err != nil {
		return r, a, fmt.Errorf("reading the stored patch: %w", err)
	}
	if err := applyPatch(ctx, r.Root, patch); err != nil {
		return r, a, fmt.Errorf("the patch does not apply to the current tree: %w", err)
	}
	r.Applied, r.AppliedAt = pick, time.Now().UTC()
	if err := Save(stateDir, r); err != nil {
		return r, a, fmt.Errorf("applied, but recording it failed: %w", err)
	}
	return r, a, nil
}

// Clean removes stored runs older than olderThan (all when zero) and
// returns their IDs.
func Clean(stateDir string, olderThan time.Duration) ([]string, error) {
	reports, err := List(stateDir)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, r := range reports {
		if olderThan > 0 && time.Since(r.CreatedAt) < olderThan {
			continue
		}
		dir, err := runPath(stateDir, r.ID)
		if err != nil {
			continue
		}
		if err := os.RemoveAll(dir); err != nil {
			return removed, err
		}
		removed = append(removed, r.ID)
	}
	return removed, nil
}
