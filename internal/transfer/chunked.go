package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MaxChunkBytes bounds one upload chunk, so a pause, cancel or disconnect
// never waits behind more than a few seconds of data.
const MaxChunkBytes int64 = 4 << 20

// Conflict decides what happens when the destination already exists.
type Conflict string

const (
	// ConflictError refuses to replace an existing file.
	ConflictError Conflict = "error"
	// ConflictRename keeps the existing file and saves as "name (1).ext".
	ConflictRename Conflict = "rename"
	// ConflictOverwrite replaces an existing regular file.
	ConflictOverwrite Conflict = "overwrite"
)

// ParseConflict maps a request value to a policy; empty means error.
func ParseConflict(s string) (Conflict, error) {
	switch Conflict(s) {
	case "", ConflictError:
		return ConflictError, nil
	case ConflictRename, ConflictOverwrite:
		return Conflict(s), nil
	}
	return "", fail("invalid_request", "conflict must be error, rename or overwrite")
}

// Destination resolves root/rel for writing under policy. It returns the
// path the file will have, which differs from rel only for rename.
func Destination(root, rel string, policy Conflict) (string, error) {
	dest, err := Resolve(root, rel)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(dest)
	if errors.Is(err, os.ErrNotExist) {
		return dest, nil
	}
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fail("symlink_escape", "destination %q is a symlink", rel)
	}
	if info.IsDir() {
		return "", fail("invalid_path", "destination %q is a directory", rel)
	}
	switch policy {
	case ConflictOverwrite:
		return dest, nil
	case ConflictRename:
		ext := filepath.Ext(dest)
		base := strings.TrimSuffix(dest, ext)
		for i := 1; i < 1000; i++ {
			candidate := fmt.Sprintf("%s (%d)%s", base, i, ext)
			if _, err := os.Lstat(candidate); errors.Is(err, os.ErrNotExist) {
				return candidate, nil
			}
		}
		return "", fail("exists", "no free name for %q", rel)
	}
	return "", fail("exists", "destination %q exists; choose rename or overwrite", rel)
}

// Install moves a verified file into root/rel. The bytes are hashed again
// while they are copied next to the destination, then renamed into place,
// so a half-copied or substituted file never appears at the final path.
func Install(root, rel, src string, size int64, sha string, policy Conflict) (string, error) {
	if !ValidSHA256(sha) {
		return "", fail("invalid_hash", "sha256 must be 64 lowercase hex characters")
	}
	dest, err := Destination(root, rel, policy)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	// Re-resolve after creating parents: a racing symlink would be caught.
	if _, err := Resolve(root, rel); err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".atlas-transfer-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), in)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fail("interrupted", "install interrupted after %d bytes: %v", n, err)
	}
	if n != size {
		return "", fail("size_mismatch", "installed %d bytes, expected %d", n, size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sha {
		return "", fail("hash_mismatch", "received sha256 %s, expected %s", got, sha)
	}
	if policy != ConflictOverwrite {
		// The name was free when chosen; refuse if something appeared since.
		if _, err := os.Lstat(dest); err == nil {
			return "", fail("exists", "destination %q appeared during the transfer", filepath.Base(dest))
		}
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return "", err
	}
	return dest, nil
}

// UploadSpec describes a chunked upload. Owner and Key make resumption
// exact: only the same owner (one session's device) resuming the same
// operation for the same content may continue it.
type UploadSpec struct {
	Owner    string   `json:"owner"`
	Key      string   `json:"key"`
	Root     string   `json:"root"`
	Rel      string   `json:"rel"`
	Size     int64    `json:"size"`
	SHA256   string   `json:"sha256"`
	Conflict Conflict `json:"conflict"`
}

// UploadStatus is the durable state of one upload.
type UploadStatus struct {
	ID          string   `json:"id"`
	OperationID string   `json:"operation_id"`
	Rel         string   `json:"rel"`
	Size        int64    `json:"size"`
	Received    int64    `json:"received"`
	SHA256      string   `json:"sha256"`
	Conflict    Conflict `json:"conflict"`
	// State is receiving, complete, failed or aborted.
	State     string `json:"state"`
	Path      string `json:"path,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
	UpdatedAt int64  `json:"updated_at"`
}

type uploadRecord struct {
	Spec   UploadSpec   `json:"spec"`
	Status UploadStatus `json:"status"`
}

// Uploads keeps partial uploads outside the destination folders, so an
// interrupted transfer never leaves a half file where the user looks.
type Uploads struct {
	dir string
	mu  sync.Mutex
}

// uploadTTL bounds how long an abandoned partial upload is kept.
const uploadTTL = 24 * time.Hour

// OpenUploads uses dir for partial data and removes stale uploads.
func OpenUploads(dir string) (*Uploads, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	u := &Uploads{dir: dir}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && time.Since(info.ModTime()) > uploadTTL {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return u, nil
}

func uploadID(owner, key string) string {
	sum := sha256.Sum256([]byte(owner + "\x00" + key))
	return "up-" + hex.EncodeToString(sum[:12])
}

func (u *Uploads) metaPath(id string) string { return filepath.Join(u.dir, id+".json") }
func (u *Uploads) partPath(id string) string { return filepath.Join(u.dir, id+".part") }

func (u *Uploads) load(id string) (uploadRecord, error) {
	var rec uploadRecord
	if !strings.HasPrefix(id, "up-") || len(id) != 27 || strings.ContainsAny(id, `/\.`) {
		return rec, fail("not_found", "unknown upload")
	}
	data, err := os.ReadFile(u.metaPath(id))
	if err != nil {
		return rec, fail("not_found", "unknown upload")
	}
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, fmt.Errorf("corrupt upload record: %w", err)
	}
	return rec, nil
}

func (u *Uploads) store(rec uploadRecord) error {
	rec.Status.UpdatedAt = time.Now().UnixMilli()
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp := u.metaPath(rec.Status.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, u.metaPath(rec.Status.ID))
}

// ErrUploadConflict means the operation key was used for other content.
var ErrUploadConflict = errors.New("upload key was already used for a different file")

// Create starts an upload or returns the existing one for the same owner,
// key and content, which is how an interrupted upload resumes.
func (u *Uploads) Create(spec UploadSpec, maxBytes int64) (UploadStatus, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	if spec.Owner == "" || spec.Key == "" {
		return UploadStatus{}, fail("invalid_request", "upload needs an owner and an operation id")
	}
	if !ValidSHA256(spec.SHA256) {
		return UploadStatus{}, fail("invalid_hash", "sha256 must be 64 lowercase hex characters")
	}
	if spec.Size < 0 || spec.Size > maxBytes {
		return UploadStatus{}, fail("too_large", "transfer of %d bytes exceeds the %d byte limit", spec.Size, maxBytes)
	}
	if _, err := ParseConflict(string(spec.Conflict)); err != nil {
		return UploadStatus{}, err
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	id := uploadID(spec.Owner, spec.Key)
	if rec, err := u.load(id); err == nil {
		if rec.Spec != spec {
			return UploadStatus{}, ErrUploadConflict
		}
		return rec.Status, nil
	}
	// Fail early when the destination cannot be written at all.
	if _, err := Destination(spec.Root, spec.Rel, spec.Conflict); err != nil {
		return UploadStatus{}, err
	}
	if err := os.WriteFile(u.partPath(id), nil, 0o600); err != nil {
		return UploadStatus{}, err
	}
	rec := uploadRecord{Spec: spec, Status: UploadStatus{ID: id, OperationID: spec.Key, Rel: spec.Rel, Size: spec.Size, SHA256: spec.SHA256, Conflict: spec.Conflict, State: "receiving"}}
	if err := u.store(rec); err != nil {
		return UploadStatus{}, err
	}
	return rec.Status, nil
}

func (u *Uploads) owned(id, owner string) (uploadRecord, error) {
	rec, err := u.load(id)
	if err != nil {
		return rec, err
	}
	if rec.Spec.Owner != owner {
		// Another session's upload is indistinguishable from a missing one.
		return uploadRecord{}, fail("not_found", "unknown upload")
	}
	return rec, nil
}

// Status reads an upload owned by owner.
func (u *Uploads) Status(id, owner string) (UploadStatus, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	rec, err := u.owned(id, owner)
	return rec.Status, err
}

// Write appends one chunk at offset, which must equal the bytes already
// received. A chunk whose hash does not match is discarded.
func (u *Uploads) Write(id, owner string, offset int64, src io.Reader, n int64, chunkSHA string) (UploadStatus, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	rec, err := u.owned(id, owner)
	if err != nil {
		return UploadStatus{}, err
	}
	st := rec.Status
	if st.State != "receiving" {
		return st, fail("upload_"+st.State, "upload is %s", st.State)
	}
	if offset != st.Received {
		return st, fail("offset_mismatch", "upload has %d bytes; send from that offset", st.Received)
	}
	if n <= 0 || n > MaxChunkBytes || offset+n > st.Size {
		return st, fail("invalid_request", "chunk must be 1-%d bytes and stay within the file size", MaxChunkBytes)
	}
	f, err := os.OpenFile(u.partPath(id), os.O_WRONLY, 0o600)
	if err != nil {
		return st, err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return st, err
	}
	h := sha256.New()
	got, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(src, n+1))
	discard := func(code, msg string) (UploadStatus, error) {
		_ = f.Truncate(offset)
		return st, fail(code, "%s", msg)
	}
	if err != nil {
		return discard("interrupted", fmt.Sprintf("chunk interrupted after %d bytes: %v", got, err))
	}
	if got != n {
		return discard("size_mismatch", fmt.Sprintf("chunk had %d bytes, expected %d", got, n))
	}
	if chunkSHA != "" && hex.EncodeToString(h.Sum(nil)) != chunkSHA {
		return discard("hash_mismatch", "chunk sha256 does not match")
	}
	if err := f.Sync(); err != nil {
		return discard("io_error", err.Error())
	}
	rec.Status.Received = offset + n
	if err := u.store(rec); err != nil {
		return discard("io_error", err.Error())
	}
	return rec.Status, nil
}

// Complete verifies the whole file and installs it. A hash mismatch fails
// the upload: a partial or corrupted file is never delivered.
func (u *Uploads) Complete(id, owner string) (UploadStatus, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	rec, err := u.owned(id, owner)
	if err != nil {
		return UploadStatus{}, err
	}
	switch rec.Status.State {
	case "complete":
		return rec.Status, nil
	case "receiving":
	default:
		return rec.Status, fail("upload_"+rec.Status.State, "upload is %s", rec.Status.State)
	}
	if rec.Status.Received != rec.Spec.Size {
		return rec.Status, fail("incomplete", "upload has %d of %d bytes", rec.Status.Received, rec.Spec.Size)
	}
	settle := func(state, code string, cause error) (UploadStatus, error) {
		rec.Status.State, rec.Status.ErrorCode, rec.Status.Error = state, code, cause.Error()
		_ = os.Remove(u.partPath(id))
		if err := u.store(rec); err != nil {
			return rec.Status, err
		}
		return rec.Status, cause
	}
	have, err := HashFile(u.partPath(id))
	if err != nil {
		return rec.Status, err
	}
	if have != rec.Spec.SHA256 {
		return settle("failed", "hash_mismatch", fail("hash_mismatch", "received sha256 %s, expected %s", have, rec.Spec.SHA256))
	}
	path, err := Install(rec.Spec.Root, rec.Spec.Rel, u.partPath(id), rec.Spec.Size, rec.Spec.SHA256, rec.Spec.Conflict)
	if err != nil {
		code := Code(err)
		if code == "exists" || code == "symlink_escape" || code == "path_traversal" || code == "hash_mismatch" {
			return settle("failed", code, err)
		}
		// Disk errors may be transient; the verified partial data stays.
		return rec.Status, err
	}
	rec.Status.State, rec.Status.Path = "complete", path
	_ = os.Remove(u.partPath(id))
	if err := u.store(rec); err != nil {
		return rec.Status, err
	}
	return rec.Status, nil
}

// Abort discards partial data. A completed upload is not undone.
func (u *Uploads) Abort(id, owner string) (UploadStatus, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	rec, err := u.owned(id, owner)
	if err != nil {
		return UploadStatus{}, err
	}
	if rec.Status.State != "receiving" {
		return rec.Status, nil
	}
	rec.Status.State = "aborted"
	_ = os.Remove(u.partPath(id))
	return rec.Status, u.store(rec)
}

// Entry is one directory listing row.
type Entry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir,omitempty"`
	Link    bool   `json:"link,omitempty"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"mod_time"`
}

// List reads up to limit entries of root/rel. Links are reported but never
// followed, so a listing cannot reveal anything outside root.
func List(root, rel string, limit int) ([]Entry, bool, error) {
	dir := root
	if rel != "" && rel != "." {
		var err error
		if dir, err = Resolve(root, rel); err != nil {
			return nil, false, err
		}
	} else if real, err := filepath.EvalSymlinks(root); err == nil {
		dir = real
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return nil, false, fail("not_found", "%s: %v", rel, err)
	}
	if !info.IsDir() {
		return nil, false, fail("invalid_path", "%q is not a directory", rel)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false, err
	}
	if limit <= 0 {
		limit = 500
	}
	out := make([]Entry, 0, min(len(entries), limit))
	for _, e := range entries {
		if len(out) >= limit {
			return out, true, nil
		}
		row := Entry{Name: e.Name(), Dir: e.IsDir(), Link: e.Type()&os.ModeSymlink != 0}
		if fi, err := e.Info(); err == nil {
			row.ModTime = fi.ModTime().UnixMilli()
			if fi.Mode().IsRegular() {
				row.Size = fi.Size()
			}
		}
		out = append(out, row)
	}
	return out, false, nil
}
