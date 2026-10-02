package workflows

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

//go:embed builtin/*.json
var builtin embed.FS

func decodeRecipe(data []byte) (Recipe, error) {
	if len(data) > 1024*1024 || !utf8.Valid(data) {
		return Recipe{}, fmt.Errorf("recipe must be valid UTF-8 and at most 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var scan func(int) error
	scan = func(depth int) error {
		if depth > 64 {
			return fmt.Errorf("recipe JSON nesting exceeds 64 levels")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, container := token.(json.Delim)
		if !container {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate or invalid recipe JSON field")
				}
				seen[name] = true
				if err := scan(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for decoder.More() {
				if err := scan(depth + 1); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid recipe JSON delimiter")
		}
		_, err = decoder.Token()
		return err
	}
	if err := scan(0); err != nil {
		return Recipe{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return Recipe{}, fmt.Errorf("recipe must contain one JSON object")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var recipe Recipe
	if err := decoder.Decode(&recipe); err != nil {
		return recipe, err
	}
	if err := validate(context.Background(), recipe, nil, false); err != nil {
		return recipe, err
	}
	return recipe, nil
}

func Load(ctx context.Context, paths []string) ([]Recipe, error) {
	var recipes []Recipe
	seen := map[string]bool{}
	add := func(data []byte, source string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		r, err := decodeRecipe(data)
		if err != nil {
			return fmt.Errorf("recipe %s: %w", source, err)
		}
		if seen[r.ID] {
			return fmt.Errorf("recipe name collision: %s", r.ID)
		}
		if len(recipes) >= 128 {
			return fmt.Errorf("recipe catalog exceeds 128 entries")
		}
		seen[r.ID] = true
		recipes = append(recipes, r)
		return nil
	}
	entries, err := builtin.ReadDir("builtin")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		data, err := builtin.ReadFile("builtin/" + entry.Name())
		if err != nil {
			return nil, err
		}
		if err := add(data, entry.Name()); err != nil {
			return nil, err
		}
	}
	if len(paths) > 32 {
		return nil, fmt.Errorf("at most 32 workflow source paths")
	}
	var files []string
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) && filepath.Ext(path) != ".json" {
			continue
		}
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("workflow sources cannot be symlinks")
		}
		if info.IsDir() {
			entries, err := os.ReadDir(path)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
					files = append(files, filepath.Join(path, entry.Name()))
				}
			}
		} else {
			files = append(files, path)
		}
		if len(files) > 128 {
			return nil, fmt.Errorf("workflow file collection exceeds 128 entries")
		}
	}
	slices.Sort(files)
	for _, path := range files {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > 1024*1024 {
			return nil, fmt.Errorf("workflow must be a bounded regular file")
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(file, 1024*1024+1))
		closeErr := file.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		if err := add(data, path); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(recipes, func(a, b Recipe) int { return strings.Compare(a.ID, b.ID) })
	return recipes, nil
}
