package skills

import (
	"embed"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
)

// BuiltinPrefix is the path prefix for builtin skill files. It is used by
// the View tool to distinguish embedded files from disk files.
const BuiltinPrefix = "atlas://skills/"

// LegacyBuiltinPrefix keeps saved conversations and external references readable.
const LegacyBuiltinPrefix = "crush://skills/"

// IsBuiltinPath recognizes canonical and legacy embedded skill addresses.
func IsBuiltinPath(path string) bool {
	return strings.HasPrefix(path, BuiltinPrefix) || strings.HasPrefix(path, LegacyBuiltinPrefix)
}

// CanonicalBuiltinPath normalizes embedded addresses without changing disk paths.
func CanonicalBuiltinPath(path string) string {
	if strings.HasPrefix(path, LegacyBuiltinPrefix) {
		return BuiltinPrefix + strings.TrimPrefix(path, LegacyBuiltinPrefix)
	}
	return path
}

//go:embed builtin/*
var builtinFS embed.FS

// BuiltinFS returns the embedded filesystem containing builtin skills.
func BuiltinFS() embed.FS {
	return builtinFS
}

// DiscoverBuiltin finds all valid skills embedded in the binary.
func DiscoverBuiltin() []*Skill {
	skills, _ := DiscoverBuiltinWithStates()
	return skills
}

// DiscoverBuiltinWithStates is like DiscoverBuiltin but additionally returns
// a per-file state slice describing parse/validation outcomes. Useful for
// diagnostics.
func DiscoverBuiltinWithStates() ([]*Skill, []*SkillState) {
	var discovered []*Skill
	var states []*SkillState

	fs.WalkDir(builtinFS, "builtin", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || d.Name() != SkillFileName {
			return nil
		}

		content, err := builtinFS.ReadFile(path)
		if err != nil {
			slog.Warn("Failed to read builtin skill file", "path", path, "error", err)
			states = append(states, &SkillState{Path: path, State: StateError, Err: err})
			return nil
		}

		skill, err := ParseContent(content)
		if err != nil {
			slog.Warn("Failed to parse builtin skill file", "path", path, "error", err)
			states = append(states, &SkillState{Path: path, State: StateError, Err: err})
			return nil
		}

		// Set paths using the Atlas-Agent prefix. Strip the leading "builtin/"
		// so the path is relative to the embedded root
		// (e.g., "atlas://skills/atlas-config/SKILL.md").
		relPath, _ := filepath.Rel("builtin", path)
		relPath = filepath.ToSlash(relPath)
		skill.SkillFilePath = BuiltinPrefix + relPath
		skill.Path = BuiltinPrefix + filepath.Dir(relPath)
		skill.Builtin = true

		if err := skill.Validate(); err != nil {
			slog.Warn("Builtin skill validation failed", "path", path, "error", err)
			states = append(states, &SkillState{Name: skill.Name, Path: path, State: StateError, Err: err})
			return nil
		}

		slog.Debug("Successfully loaded builtin skill", "name", skill.Name, "path", skill.SkillFilePath)
		discovered = append(discovered, skill)
		states = append(states, &SkillState{Name: skill.Name, Path: skill.SkillFilePath, State: StateNormal})
		return nil
	})

	return discovered, states
}
