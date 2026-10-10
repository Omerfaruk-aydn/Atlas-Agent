package localapps

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
)

// Backup archive limits.
const (
	backupFormat       = 1
	backupMetaName     = "atlas-backup.json"
	maxBackupBytes     = 2 << 30
	maxSourceBytes     = 200 << 20
	maxBackupEntries   = 20000
	maxCompressionRate = 200
)

// skipSourceDirs are never part of a source snapshot.
var skipSourceDirs = []string{".git", "__pycache__", "node_modules", ".venv", "venv", ".pytest_cache", ".mypy_cache"}

// BackupFile is one archived file.
type BackupFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// BackupMeta is atlas-backup.json.
type BackupMeta struct {
	Format          int            `json:"format"`
	AppID           string         `json:"app_id"`
	AppName         string         `json:"app_name"`
	ManifestVersion int            `json:"manifest_version"`
	Runtime         string         `json:"runtime"`
	CreatedAt       time.Time      `json:"created_at"`
	Reason          string         `json:"reason"`
	SchemaVersions  map[string]int `json:"schema_versions,omitempty"`
	IncludesSource  bool           `json:"includes_source"`
	SourceHash      string         `json:"source_hash,omitempty"`
	Files           []BackupFile   `json:"files"`
}

// BackupInfo lists one backup.
type BackupInfo struct {
	ID   string     `json:"id"`
	Path string     `json:"path"`
	Size int64      `json:"size"`
	Meta BackupMeta `json:"meta"`
}

// Backup creates a consistent backup of the app's data, plus its source
// when withSource is set.
func (m *Manager) Backup(ctx context.Context, id, reason string, withSource bool) (BackupInfo, error) {
	release, err := m.acquire(id, "backup")
	if err != nil {
		return BackupInfo{}, err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return BackupInfo{}, err
	}
	m.startJournal(id, "backup")
	info, err := m.backupLocked(ctx, rec, reason, withSource)
	m.finishJournal(id, err)
	return info, err
}

var reasonPattern = strings.NewReplacer(" ", "-", "/", "-", `\`, "-", ":", "-")

func (m *Manager) backupLocked(ctx context.Context, rec Record, reason string, withSource bool) (BackupInfo, error) {
	m.stage(rec.ID, StageBackingUp, StageRunning, reason)
	info, err := m.writeBackup(ctx, rec, reason, withSource)
	if err != nil {
		m.stage(rec.ID, StageBackingUp, StageFailed, err.Error())
		return BackupInfo{}, err
	}
	m.stage(rec.ID, StageBackingUp, StagePassed, info.Path)
	return info, nil
}

func (m *Manager) writeBackup(ctx context.Context, rec Record, reason string, withSource bool) (BackupInfo, error) {
	man, err := LoadManifest(rec.Root)
	if err != nil {
		return BackupInfo{}, fmt.Errorf("read the manifest: %w", err)
	}
	reason = reasonPattern.Replace(strings.ToLower(strings.TrimSpace(reason)))
	if reason == "" {
		reason = "manual"
	}
	if len(reason) > 32 {
		reason = reason[:32]
	}
	dir := m.backupDir(rec.ID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return BackupInfo{}, err
	}
	stage, err := os.MkdirTemp(dir, ".stage-*")
	if err != nil {
		return BackupInfo{}, err
	}
	defer os.RemoveAll(stage)

	meta := BackupMeta{Format: backupFormat, AppID: rec.ID, AppName: man.Name, ManifestVersion: man.ManifestVersion, Runtime: man.Runtime, CreatedAt: time.Now().UTC(), Reason: reason, SchemaVersions: map[string]int{}}
	type entry struct{ name, src string }
	var entries []entry

	// SQLite databases: consistent snapshots, checked before archiving.
	sqliteSet := map[string]bool{}
	for i, rel := range man.Backup.SQLite {
		src, err := insideRoot(rec.Root, rel)
		if err != nil {
			return BackupInfo{}, err
		}
		key := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
		sqliteSet[key] = true
		if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
			continue
		}
		snap := filepath.Join(stage, fmt.Sprintf("db-%d.sqlite", i))
		if err := db.SnapshotSQLite(ctx, src, snap); err != nil {
			return BackupInfo{}, err
		}
		version, err := db.InspectSQLite(ctx, snap)
		if err != nil {
			return BackupInfo{}, fmt.Errorf("the snapshot of %s failed its integrity check: %w", rel, err)
		}
		meta.SchemaVersions[key] = version
		entries = append(entries, entry{name: "data/" + key, src: snap})
	}
	// Remaining files in the data folders.
	for _, d := range man.DataDirs {
		dataDir, err := insideRoot(rec.Root, d)
		if err != nil {
			return BackupInfo{}, err
		}
		err = filepath.WalkDir(dataDir, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			if e.Type()&fs.ModeSymlink != 0 || e.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(rec.Root, p)
			key := filepath.ToSlash(rel)
			base := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(key, "-journal"), "-wal"), "-shm")
			if sqliteSet[key] || sqliteSet[base] {
				return nil
			}
			entries = append(entries, entry{name: "data/" + key, src: p})
			return nil
		})
		if err != nil {
			return BackupInfo{}, err
		}
	}
	if withSource {
		var total int64
		err := filepath.WalkDir(rec.Root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(rec.Root, p)
			if rel == "." {
				return nil
			}
			if e.IsDir() {
				if slices.Contains(skipSourceDirs, e.Name()) || isDataDir(rel, man.DataDirs) || strings.HasPrefix(e.Name(), ".atlas-") {
					return filepath.SkipDir
				}
				return nil
			}
			if e.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return err
			}
			total += info.Size()
			if total > maxSourceBytes {
				return fmt.Errorf("the source is larger than %d MiB; back it up with git instead", maxSourceBytes>>20)
			}
			entries = append(entries, entry{name: "source/" + filepath.ToSlash(rel), src: p})
			return nil
		})
		if err != nil {
			return BackupInfo{}, err
		}
		meta.IncludesSource = true
		meta.SourceHash, err = sourceHash(rec.Root, man.DataDirs)
		if err != nil {
			return BackupInfo{}, err
		}
	}

	id := time.Now().UTC().Format("20060102T150405.000Z") + "-" + reason
	id = strings.ReplaceAll(id, ".", "")
	final := filepath.Join(dir, id+".zip")
	tmp, err := os.CreateTemp(dir, ".zip-*")
	if err != nil {
		return BackupInfo{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	zw := zip.NewWriter(tmp)
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			zw.Close()
			tmp.Close()
			return BackupInfo{}, err
		}
		f, err := addZipFile(zw, e.name, e.src)
		if err != nil {
			zw.Close()
			tmp.Close()
			return BackupInfo{}, err
		}
		meta.Files = append(meta.Files, f)
	}
	metaData, _ := json.MarshalIndent(meta, "", "  ")
	w, err := zw.Create(backupMetaName)
	if err == nil {
		_, err = w.Write(metaData)
	}
	if err == nil {
		err = zw.Close()
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return BackupInfo{}, fmt.Errorf("write backup: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		return BackupInfo{}, err
	}
	st, _ := os.Stat(final)
	size := int64(0)
	if st != nil {
		size = st.Size()
	}
	return BackupInfo{ID: id, Path: final, Size: size, Meta: meta}, nil
}

func isDataDir(rel string, dirs []string) bool {
	for _, d := range dirs {
		if samePath(filepath.Clean(rel), filepath.Clean(filepath.FromSlash(d))) {
			return true
		}
	}
	return false
}

func addZipFile(zw *zip.Writer, name, src string) (BackupFile, error) {
	f, err := os.Open(src)
	if err != nil {
		return BackupFile{}, err
	}
	defer f.Close()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return BackupFile{}, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, h), f)
	if err != nil {
		return BackupFile{}, fmt.Errorf("archive %s: %w", name, err)
	}
	return BackupFile{Path: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// Backups lists the app's backups, newest first. Unreadable archives are
// skipped.
func (m *Manager) Backups(id string) ([]BackupInfo, error) {
	if !ValidID(id) {
		return nil, opErr(ErrNotFound, "", "invalid app id")
	}
	entries, err := os.ReadDir(m.backupDir(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []BackupInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".zip") {
			continue
		}
		p := filepath.Join(m.backupDir(id), e.Name())
		meta, err := readBackupMeta(p)
		if err != nil {
			continue
		}
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		out = append(out, BackupInfo{ID: strings.TrimSuffix(e.Name(), ".zip"), Path: p, Size: size, Meta: meta})
	}
	slices.SortFunc(out, func(a, b BackupInfo) int { return b.Meta.CreatedAt.Compare(a.Meta.CreatedAt) })
	return out, nil
}

func readBackupMeta(p string) (BackupMeta, error) {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return BackupMeta{}, err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != backupMetaName {
			continue
		}
		if f.UncompressedSize64 > 8<<20 {
			return BackupMeta{}, opErr(ErrInvalidBackup, "", "backup metadata is too large")
		}
		rc, err := f.Open()
		if err != nil {
			return BackupMeta{}, err
		}
		defer rc.Close()
		var meta BackupMeta
		if err := json.NewDecoder(io.LimitReader(rc, 8<<20)).Decode(&meta); err != nil {
			return BackupMeta{}, opErr(ErrInvalidBackup, "", "backup metadata is unreadable: %v", err)
		}
		return meta, nil
	}
	return BackupMeta{}, opErr(ErrInvalidBackup, "", "the archive has no %s", backupMetaName)
}

// safeEntryName validates an archive path: forward slashes, relative, no
// parent references, no drive letters.
func safeEntryName(name string) (string, error) {
	if name == "" || strings.Contains(name, `\`) || strings.HasPrefix(name, "/") || strings.Contains(name, ":") || strings.ContainsRune(name, 0) {
		return "", opErr(ErrUnsafePath, "", "unsafe path %q in backup", name)
	}
	clean := path.Clean(name)
	if clean != name || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", opErr(ErrUnsafePath, "", "unsafe path %q in backup", name)
	}
	return clean, nil
}

// RestoreOptions choose what a restore replaces.
type RestoreOptions struct {
	// Source also restores the app's files (used to roll back updates).
	Source bool
}

// Restore replaces the app's data (and source when asked) from a backup.
// The archive is validated first; the current state is backed up; files
// are extracted to a staging folder and swapped in, so a failure leaves the
// current data in place.
func (m *Manager) Restore(ctx context.Context, id, backupID string, opts RestoreOptions) (string, error) {
	release, err := m.acquire(id, "restore")
	if err != nil {
		return "", err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return "", err
	}
	m.startJournal(id, "restore")
	msg, err := m.restoreLocked(ctx, rec, backupID, opts)
	m.finishJournal(id, err)
	return msg, err
}

func (m *Manager) restoreLocked(ctx context.Context, rec Record, backupID string, opts RestoreOptions) (string, error) {
	if strings.ContainsAny(backupID, `/\:`) || strings.Contains(backupID, "..") || backupID == "" {
		return "", opErr(ErrInvalidBackup, "Choose a backup from the list.", "invalid backup id")
	}
	archive := filepath.Join(m.backupDir(rec.ID), backupID+".zip")
	m.stage(rec.ID, StageRestoring, StageRunning, "checking the backup")
	meta, err := m.validateBackup(ctx, archive, rec, opts)
	if err != nil {
		m.stage(rec.ID, StageRestoring, StageFailed, err.Error())
		return "", err
	}
	man, err := LoadManifest(rec.Root)
	if err != nil && !opts.Source {
		return "", fmt.Errorf("read the manifest: %w", err)
	}
	wasRunning := m.view(rec, false).Running
	if _, err := m.stopLocked(ctx, rec); err != nil {
		return "", err
	}
	safety, err := m.backupLocked(ctx, rec, "before-restore", opts.Source)
	if err != nil {
		return "", fmt.Errorf("the safety backup failed, nothing was restored: %w", err)
	}
	m.stage(rec.ID, StageRestoring, StageRunning, "extracting")
	staging, err := os.MkdirTemp(rec.Root, ".atlas-restore-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	if err := extractBackup(ctx, archive, staging, meta); err != nil {
		m.stage(rec.ID, StageRestoring, StageFailed, err.Error())
		return "", err
	}
	if opts.Source {
		if err := swapSource(rec.Root, filepath.Join(staging, "source"), staging, dataDirsOf(man)); err != nil {
			m.stage(rec.ID, StageRestoring, StageFailed, err.Error())
			return "", err
		}
		if man, err = LoadManifest(rec.Root); err != nil {
			return "", fmt.Errorf("the restored manifest is not valid: %w", err)
		}
	}
	if err := swapData(rec.Root, filepath.Join(staging, "data"), staging, man.DataDirs); err != nil {
		m.stage(rec.ID, StageRestoring, StageFailed, err.Error())
		return "", err
	}
	for _, rel := range man.Backup.SQLite {
		p, err := insideRoot(rec.Root, rel)
		if err != nil {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			if _, err := db.InspectSQLite(ctx, p); err != nil {
				return "", fmt.Errorf("the restored database %s failed its integrity check; the previous data is in %s: %w", rel, safety.Path, err)
			}
		}
	}
	m.stage(rec.ID, StageRestoring, StagePassed, backupID)
	_, _ = m.mutate(ctx, rec.ID, func(r *Record) error {
		r.ManifestHash = manifestHash(r.Root)
		return nil
	})
	msg := fmt.Sprintf("Restored %s. The state before the restore was backed up to %s.", backupID, safety.Path)
	if wasRunning {
		fresh, err := m.record(rec.ID)
		if err == nil {
			if res, err := m.startLocked(ctx, fresh, StartOptions{}); err == nil {
				msg += " The app was started again at " + res.URL + "."
			} else {
				msg += " The app did not start again: " + err.Error()
			}
		}
	}
	return msg, nil
}

func dataDirsOf(man Manifest) []string {
	if len(man.DataDirs) == 0 {
		return []string{"data"}
	}
	return man.DataDirs
}

// validateBackup checks identity, compatibility and every entry before
// anything is extracted.
func (m *Manager) validateBackup(ctx context.Context, archive string, rec Record, opts RestoreOptions) (BackupMeta, error) {
	meta, err := readBackupMeta(archive)
	if err != nil {
		return meta, err
	}
	switch {
	case meta.Format != backupFormat:
		return meta, opErr(ErrIncompatible, "", "backup format %d is not supported", meta.Format)
	case meta.AppID != rec.ID:
		return meta, opErr(ErrIncompatible, "Restore it into the app it was taken from.", "this backup belongs to %s, not %s", meta.AppID, rec.ID)
	case meta.ManifestVersion > ManifestVersion:
		return meta, opErr(ErrIncompatible, "Update Atlas first.", "the backup was made with a newer manifest version (%d)", meta.ManifestVersion)
	case opts.Source && !meta.IncludesSource:
		return meta, opErr(ErrIncompatible, "Choose a backup taken before an update.", "this backup holds data only, not the app's files")
	}
	if !opts.Source {
		// Data from a newer schema cannot be read by the current code.
		for rel, version := range meta.SchemaVersions {
			p, err := insideRoot(rec.Root, rel)
			if err != nil {
				return meta, err
			}
			if _, err := os.Stat(p); err != nil {
				continue
			}
			current, err := db.InspectSQLite(ctx, p)
			if err == nil && version > current {
				return meta, opErr(ErrIncompatible, "Restore it together with the app files from the same backup, or update the app first.", "the backup's database schema (%d) is newer than the app's (%d)", version, current)
			}
		}
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return meta, opErr(ErrInvalidBackup, "", "the archive cannot be opened: %v", err)
	}
	defer zr.Close()
	if len(zr.File) > maxBackupEntries {
		return meta, opErr(ErrInvalidBackup, "", "the archive has too many entries")
	}
	listed := map[string]BackupFile{}
	for _, f := range meta.Files {
		listed[f.Path] = f
	}
	var total uint64
	for _, f := range zr.File {
		name, err := safeEntryName(f.Name)
		if err != nil {
			return meta, err
		}
		if f.Mode()&fs.ModeSymlink != 0 || f.Mode()&fs.ModeType != 0 && !f.FileInfo().IsDir() {
			return meta, opErr(ErrUnsafePath, "", "the backup contains a link or special file: %s", name)
		}
		if name == backupMetaName || f.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(name, "data/") && !strings.HasPrefix(name, "source/") {
			return meta, opErr(ErrUnsafePath, "", "unexpected entry %s in backup", name)
		}
		if _, ok := listed[name]; !ok {
			return meta, opErr(ErrInvalidBackup, "", "the entry %s is not listed in the backup metadata", name)
		}
		total += f.UncompressedSize64
		if total > maxBackupBytes {
			return meta, opErr(ErrInvalidBackup, "", "the backup expands beyond %d GiB", maxBackupBytes>>30)
		}
		if f.CompressedSize64 > 0 && f.UncompressedSize64/f.CompressedSize64 > maxCompressionRate {
			return meta, opErr(ErrInvalidBackup, "", "the entry %s has a suspicious compression ratio", name)
		}
	}
	return meta, nil
}

func extractBackup(ctx context.Context, archive, dest string, meta BackupMeta) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	want := map[string]BackupFile{}
	for _, f := range meta.Files {
		want[f.Path] = f
	}
	for _, f := range zr.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name, err := safeEntryName(f.Name)
		if err != nil {
			return err
		}
		if name == backupMetaName || f.FileInfo().IsDir() {
			continue
		}
		target := filepath.Join(dest, filepath.FromSlash(name))
		rel, err := filepath.Rel(dest, target)
		if err != nil || strings.HasPrefix(rel, "..") {
			return opErr(ErrUnsafePath, "", "unsafe path %q in backup", name)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			rc.Close()
			return err
		}
		h := sha256.New()
		n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(rc, int64(f.UncompressedSize64)+1))
		rc.Close()
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		exp := want[name]
		if n != exp.Size || hex.EncodeToString(h.Sum(nil)) != exp.SHA256 {
			return opErr(ErrInvalidBackup, "Use another backup.", "the backup entry %s is damaged (checksum mismatch)", name)
		}
	}
	return nil
}

// swapData replaces each data folder with its restored copy. The current
// folders are moved aside first and moved back if anything fails.
func swapData(root, restored, staging string, dataDirs []string) error {
	aside := filepath.Join(staging, "previous-data")
	type moved struct{ from, to string }
	var done []moved
	undo := func() {
		for i := len(done) - 1; i >= 0; i-- {
			_ = os.Rename(done[i].to, done[i].from)
		}
	}
	for _, d := range dataDirs {
		live, err := insideRoot(root, d)
		if err != nil {
			undo()
			return err
		}
		rel := filepath.FromSlash(d)
		src := filepath.Join(restored, rel)
		if _, err := os.Stat(live); err == nil {
			to := filepath.Join(aside, rel)
			if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
				undo()
				return err
			}
			if err := os.Rename(live, to); err != nil {
				undo()
				return fmt.Errorf("move the current data aside (is the app still open?): %w", err)
			}
			done = append(done, moved{live, to})
		}
		if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
			if err := os.MkdirAll(live, 0o755); err != nil {
				undo()
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(live), 0o755); err != nil {
			undo()
			return err
		}
		if err := os.Rename(src, live); err != nil {
			undo()
			return fmt.Errorf("put the restored data in place: %w", err)
		}
		done = append(done, moved{src, live})
	}
	return nil
}

// swapSource replaces backed-up source files while preserving excluded
// directories, links and application data at every depth.
func swapSource(root, restored, staging string, dataDirs []string) error {
	return restoreSourceFiles(root, restored, staging, dataDirs)
}
