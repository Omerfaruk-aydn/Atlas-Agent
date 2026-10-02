package engineering

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
)

const maxArtifactBytes = 32 * 1024 * 1024

var artifactKind = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type ArtifactRef struct {
	Kind    string `json:"kind"`
	Hash    string `json:"hash"`
	Version int    `json:"version"`
	Size    int64  `json:"size"`
}

type Record struct {
	Revision uint64        `json:"revision"`
	Ref      ArtifactRef   `json:"ref"`
	Linked   []ArtifactRef `json:"linked,omitempty"`
}

type artifactManifest struct {
	Version   int               `json:"version"`
	Namespace string            `json:"namespace"`
	Records   map[string]Record `json:"records"`
}

func (ref ArtifactRef) validate() error {
	decoded, err := hex.DecodeString(ref.Hash)
	if !artifactKind.MatchString(ref.Kind) || ref.Version != 1 || ref.Size < 0 || ref.Size > maxArtifactBytes || err != nil || len(decoded) != 32 || strings.ToLower(ref.Hash) != ref.Hash {
		return fmt.Errorf("invalid artifact reference")
	}
	return nil
}

func validRecordName(name string) bool {
	return strings.TrimSpace(name) != "" && len(name) <= 1024 && !strings.ContainsAny(name, "\x00\r\n")
}

func (s *Store) artifactLock(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, dir := range []string{s.dir, filepath.Join(s.dir, "artifacts"), filepath.Join(s.dir, "artifact-index")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("artifact directory must be a real directory")
		}
	}
	lockCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return lock.File(lockCtx, filepath.Join(s.dir, "artifacts.lock"))
}

func boundedArtifactFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("artifact is not a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("artifact exceeds size limit")
	}
	return data, nil
}

func (s *Store) readArtifact(ref ArtifactRef) ([]byte, error) {
	if err := ref.validate(); err != nil {
		return nil, err
	}
	data, err := boundedArtifactFile(filepath.Join(s.dir, "artifacts", ref.Hash+".blob"), maxArtifactBytes)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != ref.Size || Hash(string(data)) != ref.Hash {
		return nil, fmt.Errorf("artifact integrity mismatch")
	}
	return data, nil
}

func (s *Store) putArtifact(kind string, data []byte) (ArtifactRef, error) {
	ref := ArtifactRef{Kind: kind, Hash: Hash(string(data)), Version: 1, Size: int64(len(data))}
	if err := ref.validate(); err != nil {
		return ArtifactRef{}, err
	}
	path := filepath.Join(s.dir, "artifacts", ref.Hash+".blob")
	if _, err := os.Lstat(path); err == nil {
		_, err = s.readArtifact(ref)
		return ref, err
	} else if !errors.Is(err, os.ErrNotExist) {
		return ArtifactRef{}, err
	}
	return ref, AtomicWrite(path, data)
}

func (s *Store) PutArtifact(ctx context.Context, kind string, data []byte) (ArtifactRef, error) {
	if len(data) > maxArtifactBytes || !artifactKind.MatchString(kind) {
		return ArtifactRef{}, fmt.Errorf("artifact exceeds limit or has invalid kind")
	}
	release, err := s.artifactLock(ctx)
	if err != nil {
		return ArtifactRef{}, err
	}
	defer release()
	if err := ctx.Err(); err != nil {
		return ArtifactRef{}, err
	}
	return s.putArtifact(kind, data)
}

func (s *Store) ReadArtifact(ctx context.Context, ref ArtifactRef) ([]byte, error) {
	release, err := s.artifactLock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	data, err := s.readArtifact(ref)
	if err == nil {
		err = ctx.Err()
	}
	return data, err
}

func (s *Store) readManifest(namespace string) (artifactManifest, error) {
	if !validRecordName(namespace) {
		return artifactManifest{}, fmt.Errorf("invalid record namespace")
	}
	manifest := artifactManifest{Version: 1, Namespace: namespace, Records: map[string]Record{}}
	data, err := boundedArtifactFile(filepath.Join(s.dir, "artifact-index", Hash(namespace)+".json"), maxStateBytes)
	if errors.Is(err, os.ErrNotExist) {
		return manifest, nil
	}
	if err != nil {
		return manifest, err
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("decode artifact index: %w", err)
	}
	if manifest.Version != 1 || manifest.Namespace != namespace || manifest.Records == nil || len(manifest.Records) > 128 {
		return manifest, fmt.Errorf("invalid artifact index")
	}
	for key, record := range manifest.Records {
		if !validRecordName(key) || record.Revision == 0 || len(record.Linked) > 128 || record.Ref.Kind != "record" {
			return manifest, fmt.Errorf("invalid artifact record")
		}
		for _, ref := range append([]ArtifactRef{record.Ref}, record.Linked...) {
			if err := ref.validate(); err != nil {
				return manifest, err
			}
		}
	}
	return manifest, nil
}

func (s *Store) PutRecord(ctx context.Context, namespace, key string, expected uint64, data []byte, linked ...ArtifactRef) (Record, error) {
	return s.putRecord(ctx, namespace, key, expected, data, true, linked...)
}

// PutRecordStrict rejects a stale writer even when its bytes match the winner.
func (s *Store) PutRecordStrict(ctx context.Context, namespace, key string, expected uint64, data []byte, linked ...ArtifactRef) (Record, error) {
	return s.putRecord(ctx, namespace, key, expected, data, false, linked...)
}

func (s *Store) putRecord(ctx context.Context, namespace, key string, expected uint64, data []byte, retry bool, linked ...ArtifactRef) (Record, error) {
	if !validRecordName(key) || !validRecordName(namespace) || len(data) > maxArtifactBytes || len(linked) > 128 {
		return Record{}, fmt.Errorf("invalid record or size limit exceeded")
	}
	release, err := s.artifactLock(ctx)
	if err != nil {
		return Record{}, err
	}
	defer release()
	manifest, err := s.readManifest(namespace)
	if err != nil {
		return Record{}, err
	}
	old, exists := manifest.Records[key]
	if old.Revision != expected {
		if retry && exists && old.Revision == expected+1 && old.Ref.Hash == Hash(string(data)) && slices.Equal(old.Linked, linked) {
			for _, ref := range append([]ArtifactRef{old.Ref}, old.Linked...) {
				if _, err := s.readArtifact(ref); err != nil {
					return Record{}, err
				}
			}
			return old, nil
		}
		return Record{}, fmt.Errorf("record revision conflict")
	}
	if expected == ^uint64(0) || !exists && len(manifest.Records) >= 128 {
		return Record{}, fmt.Errorf("record collection or revision limit exceeded")
	}
	for _, ref := range linked {
		if _, err := s.readArtifact(ref); err != nil {
			return Record{}, fmt.Errorf("invalid linked artifact: %w", err)
		}
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	ref, err := s.putArtifact("record", data)
	if err != nil {
		return Record{}, err
	}
	record := Record{Revision: expected + 1, Ref: ref, Linked: slices.Clone(linked)}
	manifest.Records[key] = record
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return Record{}, err
	}
	if len(encoded) > maxStateBytes {
		return Record{}, fmt.Errorf("artifact index exceeds 1 MiB")
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	return record, AtomicWrite(filepath.Join(s.dir, "artifact-index", Hash(namespace)+".json"), encoded)
}

func (s *Store) ListRecords(ctx context.Context, namespace string) (map[string]Record, error) {
	release, err := s.artifactLock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	manifest, err := s.readManifest(namespace)
	return manifest.Records, err
}

func (s *Store) ReadRecord(ctx context.Context, namespace, key string) (Record, []byte, error) {
	release, err := s.artifactLock(ctx)
	if err != nil {
		return Record{}, nil, err
	}
	defer release()
	manifest, err := s.readManifest(namespace)
	if err != nil {
		return Record{}, nil, err
	}
	record, ok := manifest.Records[key]
	if !ok {
		return Record{}, nil, os.ErrNotExist
	}
	for _, ref := range record.Linked {
		if _, err := s.readArtifact(ref); err != nil {
			return Record{}, nil, err
		}
	}
	data, err := s.readArtifact(record.Ref)
	return record, data, err
}

// CleanArtifacts removes only unreferenced blobs after checking every index.
func (s *Store) CleanArtifacts(ctx context.Context) (int, error) {
	release, err := s.artifactLock(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	indices, err := os.ReadDir(filepath.Join(s.dir, "artifact-index"))
	if err != nil {
		return 0, err
	}
	live := map[string]bool{}
	for _, entry := range indices {
		if strings.HasPrefix(entry.Name(), ".atlas-state-") {
			continue
		}
		data, err := boundedArtifactFile(filepath.Join(s.dir, "artifact-index", entry.Name()), maxStateBytes)
		if err != nil {
			return 0, err
		}
		var identity struct{ Namespace string }
		if err := json.Unmarshal(data, &identity); err != nil || Hash(identity.Namespace)+".json" != entry.Name() {
			return 0, fmt.Errorf("invalid artifact index identity")
		}
		manifest, err := s.readManifest(identity.Namespace)
		if err != nil {
			return 0, err
		}
		for _, record := range manifest.Records {
			for _, ref := range append([]ArtifactRef{record.Ref}, record.Linked...) {
				if _, err := s.readArtifact(ref); err != nil {
					return 0, err
				}
				live[ref.Hash] = true
			}
		}
	}
	blobs, err := os.ReadDir(filepath.Join(s.dir, "artifacts"))
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, entry := range blobs {
		if err := ctx.Err(); err != nil {
			return removed, err
		}
		if !strings.HasSuffix(entry.Name(), ".blob") || entry.Type()&os.ModeSymlink != 0 || entry.IsDir() {
			continue
		}
		hash := strings.TrimSuffix(entry.Name(), ".blob")
		if (ArtifactRef{Kind: "record", Version: 1, Hash: hash}).validate() != nil || live[hash] {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, "artifacts", entry.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
