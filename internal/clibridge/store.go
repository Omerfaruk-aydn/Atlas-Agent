package clibridge

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/appenv"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/home"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/secrets"
)

// Scope says where a profile is installed.
type Scope string

const (
	ScopeProject Scope = "project"
	ScopeUser    Scope = "user"
)

// ParseScope accepts project or user.
func ParseScope(value string) (Scope, error) {
	switch Scope(value) {
	case ScopeProject, ScopeUser:
		return Scope(value), nil
	}
	return "", fmt.Errorf("scope must be project or user")
}

// Installed is the stored envelope for an imported profile. The profile is
// kept exactly as validated; ProfileSHA256 detects later hand edits.
type Installed struct {
	Kind          string    `json:"kind"`
	Version       int       `json:"version"`
	Scope         Scope     `json:"scope"`
	ProfileSHA256 string    `json:"profile_sha256"`
	ImportedAt    int64     `json:"imported_at"`
	Source        string    `json:"source,omitempty"`
	Pin           *Identity `json:"pin,omitempty"`
	Profile       Profile   `json:"profile"`
}

// Store holds installed profiles for one project and one user.
type Store struct {
	ProjectDir string
	UserDir    string
}

// ProjectProfilesDir is where project-scoped profiles live, next to
// project skills under .atlas.
func ProjectProfilesDir(root string) string {
	return filepath.Join(root, ".atlas", "cli-bridge", "profiles")
}

// UserProfilesDir is the per-user profile directory.
// ATLAS_AGENT_CLI_BRIDGE_DIR overrides it.
func UserProfilesDir() string {
	if dir := appenv.Get("CLI_BRIDGE_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(home.Config(), "atlas", "cli-bridge", "profiles")
}

// NewStore returns the store for a project root.
func NewStore(root string) Store {
	return Store{ProjectDir: ProjectProfilesDir(root), UserDir: UserProfilesDir()}
}

func (s Store) dir(scope Scope) string {
	if scope == ScopeUser {
		return s.UserDir
	}
	return s.ProjectDir
}

// Ref is a profile reference: "id", "project:id" or "user:id".
type Ref struct {
	Scope Scope
	ID    string
}

// ParseRef parses a profile reference.
func ParseRef(value string) (Ref, error) {
	scope, id, qualified := strings.Cut(value, ":")
	if !qualified {
		id, scope = value, ""
	}
	if !idPattern.MatchString(id) {
		return Ref{}, fmt.Errorf("profile reference %q must be id, project:id or user:id", truncate(value, 80))
	}
	ref := Ref{ID: id}
	if qualified {
		parsed, err := ParseScope(scope)
		if err != nil {
			return Ref{}, err
		}
		ref.Scope = parsed
	}
	return ref, nil
}

func (r Ref) String() string {
	if r.Scope == "" {
		return r.ID
	}
	return string(r.Scope) + ":" + r.ID
}

// Load reads one installed profile and verifies its integrity.
func (s Store) Load(scope Scope, id string) (Installed, error) {
	path := filepath.Join(s.dir(scope), id+".json")
	data, err := readBounded(path, maxProfileBytes*2)
	if err != nil {
		return Installed{}, err
	}
	var installed Installed
	if err := Decode(data, &installed); err != nil {
		return Installed{}, fmt.Errorf("%s profile %s: %w", scope, id, err)
	}
	if installed.Kind != InstalledKind || installed.Version != ProfileVersion || installed.Scope != scope {
		return Installed{}, fmt.Errorf("%s profile %s has an invalid envelope", scope, id)
	}
	if err := installed.Profile.Validate(); err != nil {
		return Installed{}, fmt.Errorf("%s profile %s: %w", scope, id, err)
	}
	if installed.Profile.ID != id {
		return Installed{}, fmt.Errorf("%s profile file %s.json declares id %s", scope, id, installed.Profile.ID)
	}
	if installed.Profile.Hash() != installed.ProfileSHA256 {
		return Installed{}, fmt.Errorf("%s profile %s was modified outside `cli-bridge import`; re-import the reviewed profile", scope, id)
	}
	return installed, nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("%s must be a regular file of at most %d bytes", path, limit)
	}
	return os.ReadFile(path)
}

// Find resolves a reference. An unqualified id present in both scopes is
// ambiguous and must be qualified.
func (s Store) Find(ref Ref) (Installed, error) {
	if ref.Scope != "" {
		installed, err := s.Load(ref.Scope, ref.ID)
		if errors.Is(err, os.ErrNotExist) {
			return Installed{}, fmt.Errorf("no %s profile %q is installed", ref.Scope, ref.ID)
		}
		return installed, err
	}
	var found []Scope
	for _, scope := range []Scope{ScopeProject, ScopeUser} {
		if _, err := os.Lstat(filepath.Join(s.dir(scope), ref.ID+".json")); err == nil {
			found = append(found, scope)
		}
	}
	switch len(found) {
	case 0:
		return Installed{}, fmt.Errorf("no profile %q is installed; import one with `atlas-agent cli-bridge import`", ref.ID)
	case 1:
		return s.Load(found[0], ref.ID)
	}
	return Installed{}, fmt.Errorf("profile %q exists in both project and user scope; use project:%s or user:%s", ref.ID, ref.ID, ref.ID)
}

// Summary is one row of List.
type Summary struct {
	Scope       Scope    `json:"scope"`
	ID          string   `json:"id"`
	Description string   `json:"description,omitempty"`
	Operations  []string `json:"operations,omitempty"`
	ProfileHash string   `json:"profile_hash,omitempty"`
	Error       string   `json:"error,omitempty"`
}

// List returns every installed profile in both scopes. Damaged profiles
// are listed with their error instead of being hidden.
func (s Store) List() ([]Summary, error) {
	var out []Summary
	for _, scope := range []Scope{ScopeProject, ScopeUser} {
		entries, err := os.ReadDir(s.dir(scope))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			id, ok := strings.CutSuffix(entry.Name(), ".json")
			if !ok || entry.IsDir() || !idPattern.MatchString(id) {
				continue
			}
			installed, err := s.Load(scope, id)
			if err != nil {
				out = append(out, Summary{Scope: scope, ID: id, Error: err.Error()})
				continue
			}
			row := Summary{Scope: scope, ID: id, Description: installed.Profile.Description, ProfileHash: installed.ProfileSHA256}
			for _, op := range installed.Profile.Operations {
				row.Operations = append(row.Operations, op.ID)
			}
			out = append(out, row)
		}
	}
	slices.SortFunc(out, func(a, b Summary) int { return strings.Compare(string(a.Scope)+":"+a.ID, string(b.Scope)+":"+b.ID) })
	return out, nil
}

// ImportOptions control Import.
type ImportOptions struct {
	Scope   Scope
	Replace bool
	// Source names the file the profile came from, for the record.
	Source string
}

// ImportResult describes what Import did.
type ImportResult struct {
	Installed Installed `json:"installed"`
	Path      string    `json:"path"`
	Unchanged bool      `json:"unchanged,omitempty"`
	Replaced  string    `json:"replaced_profile_hash,omitempty"`
	// ExecutableStatus is available, unavailable or unsupported. Import
	// succeeds without an executable; the profile stays unusable until
	// the program is installed and pinned.
	ExecutableStatus Status `json:"executable_status"`
	ExecutableError  string `json:"executable_error,omitempty"`
	Executed         bool   `json:"executed"`
}

// Import validates a profile, refuses embedded credentials, pins its
// executable by content and writes it atomically. It never runs a command.
func (s Store) Import(ctx context.Context, data []byte, opts ImportOptions) (ImportResult, error) {
	data = NormalizeText(data)
	profile, err := ParseProfile(data)
	if err != nil {
		return ImportResult{}, err
	}
	if err := scanForSecrets(data); err != nil {
		return ImportResult{}, err
	}
	scope := opts.Scope
	if scope == "" {
		scope = ScopeProject
	}
	if _, err := ParseScope(string(scope)); err != nil {
		return ImportResult{}, err
	}
	dir := s.dir(scope)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ImportResult{}, err
	}
	release, err := lock.File(ctx, filepath.Join(filepath.Dir(dir), ".profiles.lock"))
	if err != nil {
		return ImportResult{}, err
	}
	defer release()
	other := ScopeUser
	if scope == ScopeUser {
		other = ScopeProject
	}
	if _, err := os.Lstat(filepath.Join(s.dir(other), profile.ID+".json")); err == nil {
		return ImportResult{}, fmt.Errorf("profile id %q already exists in %s scope; remove it there or choose another id so references stay unambiguous", profile.ID, other)
	}
	path := filepath.Join(dir, profile.ID+".json")
	result := ImportResult{Path: path}
	hash := profile.Hash()
	if existing, err := s.Load(scope, profile.ID); err == nil {
		if existing.ProfileSHA256 == hash {
			result.Installed, result.Unchanged = existing, true
			result.ExecutableStatus = StatusAvailable
			if _, err := Current(ctx, existing); err != nil {
				result.ExecutableStatus, result.ExecutableError = StatusOf(err), err.Error()
			}
			return result, nil
		}
		if !opts.Replace {
			return ImportResult{}, fmt.Errorf("%s profile %q already exists with different content (sha256 %s); pass --replace to overwrite it after review", scope, profile.ID, shortHash(existing.ProfileSHA256))
		}
		result.Replaced = existing.ProfileSHA256
	} else if !errors.Is(err, os.ErrNotExist) {
		if !opts.Replace {
			return ImportResult{}, fmt.Errorf("existing %s profile %q is unreadable (%v); pass --replace to overwrite it", scope, profile.ID, err)
		}
	}
	installed := Installed{Kind: InstalledKind, Version: ProfileVersion, Scope: scope, ProfileSHA256: hash, ImportedAt: time.Now().UnixMilli(), Source: filepath.Base(opts.Source), Profile: profile}
	result.ExecutableStatus = StatusAvailable
	identity, err := Pin(ctx, profile.Executable)
	if err != nil {
		result.ExecutableStatus, result.ExecutableError = StatusOf(err), err.Error()
	} else {
		installed.Pin = &identity
	}
	if err := s.write(installed); err != nil {
		return ImportResult{}, err
	}
	result.Installed = installed
	return result, nil
}

func (s Store) write(installed Installed) error {
	data, err := marshalIndent(installed)
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(s.dir(installed.Scope), installed.Profile.ID+".json"), data)
}

// Repin re-resolves the executable of an installed profile. It is a user
// decision to trust a new program; it never runs the program.
func (s Store) Repin(ctx context.Context, ref Ref) (Installed, error) {
	installed, err := s.Find(ref)
	if err != nil {
		return Installed{}, err
	}
	release, err := lock.File(ctx, filepath.Join(filepath.Dir(s.dir(installed.Scope)), ".profiles.lock"))
	if err != nil {
		return Installed{}, err
	}
	defer release()
	identity, err := Pin(ctx, installed.Profile.Executable)
	if err != nil {
		return Installed{}, err
	}
	installed.Pin = &identity
	return installed, s.write(installed)
}

// Remove deletes an installed profile.
func (s Store) Remove(ctx context.Context, ref Ref) (Installed, error) {
	installed, err := s.Find(ref)
	if err != nil {
		return Installed{}, err
	}
	release, err := lock.File(ctx, filepath.Join(filepath.Dir(s.dir(installed.Scope)), ".profiles.lock"))
	if err != nil {
		return Installed{}, err
	}
	defer release()
	return installed, os.Remove(filepath.Join(s.dir(installed.Scope), installed.Profile.ID+".json"))
}

// scanForSecrets refuses profiles that embed credentials. Profiles name
// environment variables instead; values stay in the environment.
func scanForSecrets(data []byte) error {
	if findings := secrets.ScanText("profile.json", string(data), secrets.Options{MinConfidence: secrets.ConfidenceMedium}); len(findings) > 0 {
		f := findings[0]
		return fmt.Errorf("profile appears to contain a credential (%s on line %d: %s); declare the variable name under env instead of embedding its value", f.Rule, f.Line, f.Redacted)
	}
	return nil
}
