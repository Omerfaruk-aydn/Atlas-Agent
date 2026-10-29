package harnessmetrics

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var ownedTraceName = regexp.MustCompile(`^[0-9a-f]{64}\.jsonl(?:\.1|\.turn)?$`)

// PruneTraces removes inactive owned session traces, oldest first. The quota
// is soft: sessions written within the last day are always preserved. Turn
// counters of removed sessions are removed too; a pruned session starts anew.
// No symlink is followed and unrelated files are never selected.
func PruneTraces(ctx context.Context, root string, now time.Time, age time.Duration, quota int64) (int, error) {
	dir := filepath.Join(root, Dir, "traces")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	type group struct {
		name  string
		files []string
		at    time.Time
		size  int64
	}
	groups := map[string]*group{}
	var total int64
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		if !ownedTraceName.MatchString(e.Name()) || e.Type()&os.ModeSymlink != 0 || e.IsDir() {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return 0, err
		}
		name := e.Name()[:64]
		g := groups[name]
		if g == nil {
			g = &group{name: name}
			groups[name] = g
		}
		g.files = append(g.files, e.Name())
		g.size += info.Size()
		total += info.Size()
		// A counter alone must not retain an old session forever.
		if !strings.HasSuffix(e.Name(), ".turn") && info.ModTime().After(g.at) {
			g.at = info.ModTime()
		}
	}
	list := make([]*group, 0, len(groups))
	for _, g := range groups {
		list = append(list, g)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].at.Before(list[j].at) })
	removed := 0
	for _, g := range list {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if now.Sub(g.at) < 24*time.Hour || (now.Sub(g.at) <= age && total <= quota) {
			continue
		}
		lock := traceFileLock(filepath.Join(dir, g.name+".jsonl"))
		lock.Lock()
		// Recheck after acquiring the session stripe: an active writer may
		// have refreshed or rotated this session since directory enumeration.
		active := false
		for _, suffix := range []string{".jsonl", ".jsonl.1"} {
			if info, err := os.Lstat(filepath.Join(dir, g.name+suffix)); err == nil && (info.Mode()&os.ModeSymlink != 0 || now.Sub(info.ModTime()) < 24*time.Hour) {
				active = true
			}
		}
		if active {
			lock.Unlock()
			continue
		}
		var deleteErr error
		for _, name := range g.files {
			if err := os.Remove(filepath.Join(dir, name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				deleteErr = err
				break
			}
		}
		lock.Unlock()
		if deleteErr != nil {
			return removed, deleteErr
		}
		total -= g.size
		removed++
	}
	return removed, nil
}
