// Package verification provides deterministic, bounded quality checks.
package verification

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type (
	Change struct {
		Path, Before, After string
		Lines               []int
	}
	Mutant struct {
		Path   string
		Line   int
		Source []byte
	}
)

// Mutator is the extension point for other source languages.
type (
	Mutator interface {
		Mutants(Change, int) ([]Mutant, error)
	}
	GoMutator struct{}
)

func (GoMutator) Mutants(c Change, limit int) ([]Mutant, error) {
	set := token.NewFileSet()
	f, err := parser.ParseFile(set, c.Path, c.After, 0)
	if err != nil {
		return nil, err
	}
	var out []Mutant
	add := func(pos token.Pos, length int, replacement string) {
		p := set.Position(pos)
		if len(out) >= limit || !slices.Contains(c.Lines, p.Line) {
			return
		}
		s := c.After[:p.Offset] + replacement + c.After[p.Offset+length:]
		out = append(out, Mutant{Path: c.Path, Line: p.Line, Source: []byte(s)})
	}
	ast.Inspect(f, func(n ast.Node) bool {
		if len(out) >= limit {
			return false
		}
		switch x := n.(type) {
		case *ast.BinaryExpr:
			replacements := map[token.Token]string{token.LEQ: "<", token.GEQ: ">", token.LSS: "<=", token.GTR: ">=", token.EQL: "!=", token.NEQ: "==", token.LAND: "||", token.LOR: "&&", token.ADD: "-", token.SUB: "+"}
			if replacement, ok := replacements[x.Op]; ok {
				add(x.OpPos, len(x.Op.String()), replacement)
			}
		case *ast.BasicLit:
			if x.Kind == token.INT && x.Value == "0" {
				add(x.Pos(), 1, "1")
			}
		case *ast.Ident:
			switch x.Name {
			case "true":
				add(x.Pos(), 4, "false")
			case "false":
				add(x.Pos(), 5, "true")
			}
		}
		return true
	})
	return out, nil
}

type (
	MutationOptions struct {
		Max     int
		Timeout time.Duration
		Command string
	}
	MutationReport struct {
		Generated   int `json:"generated"`
		Killed      int `json:"killed"`
		Survived    int `json:"survived"`
		Invalid     int `json:"invalid"`
		Unsupported int `json:"unsupported"`
	}
)
type Runner func(context.Context, string, string) (int, string, error)

// RunMutations never writes the original root. A disposable copy contains even
// the baseline run, and is removed on errors, cancellation and panics. Runtime
// source restoration therefore does not depend on a deferred write succeeding.
func RunMutations(ctx context.Context, root string, changes []Change, opts MutationOptions, run Runner) (report MutationReport, err error) {
	if opts.Max <= 0 {
		opts.Max = 4
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.Command == "" {
		opts.Command = "go test -p 1 -count=1 ./..."
	}
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()
	dir, err := os.MkdirTemp("", "atlas-mutation-")
	if err != nil {
		return report, err
	}
	defer func() {
		if value := recover(); value != nil {
			err = fmt.Errorf("mutation runner panicked")
		}
		err = errors.Join(err, os.RemoveAll(dir))
	}()
	if err = copyProject(ctx, root, dir); err != nil {
		return report, err
	}
	exit, _, err := run(ctx, dir, opts.Command)
	if err != nil {
		return report, err
	}
	if exit != 0 {
		return report, fmt.Errorf("mutation baseline did not pass")
	}
	for _, c := range changes {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if strings.HasSuffix(c.Path, "_test.go") {
			continue
		}
		if !strings.HasSuffix(c.Path, ".go") {
			report.Unsupported++
			continue
		}
		if report.Generated >= opts.Max {
			break
		}
		mutants, err := (GoMutator{}).Mutants(c, opts.Max-report.Generated)
		if err != nil {
			return report, err
		}
		path := filepath.Join(dir, filepath.FromSlash(c.Path))
		rel, err := filepath.Rel(dir, path)
		if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return report, fmt.Errorf("mutant path escapes copy")
		}
		original, err := os.ReadFile(path)
		if err != nil {
			return report, err
		}
		if string(original) != c.After {
			return report, fmt.Errorf("source changed during mutation snapshot")
		}
		for _, m := range mutants {
			if err := ctx.Err(); err != nil {
				return report, err
			}
			report.Generated++
			if err := os.WriteFile(path, m.Source, 0o600); err != nil {
				return report, err
			}
			exit, output, runErr := run(ctx, dir, opts.Command)
			restoreErr := os.WriteFile(path, original, 0o600)
			if err := errors.Join(runErr, restoreErr, ctx.Err()); err != nil {
				return report, err
			}
			if exit == 0 {
				report.Survived++
			} else if strings.Contains(output, "--- FAIL:") && !strings.Contains(output, "[build failed]") {
				report.Killed++
			} else {
				report.Invalid++
			}
		}
	}
	return report, nil
}

func copyProject(ctx context.Context, root, dest string) error {
	var size int64
	count := 0
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if rel != "." && slices.Contains([]string{".git", ".atlas", "node_modules", "vendor", ".venv", "__pycache__"}, d.Name()) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dest, rel), 0o700)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("mutation copy refuses symlinks")
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("mutation copy refuses nonregular files")
		}
		count++
		size += info.Size()
		if count > 10000 || size > 64<<20 {
			return fmt.Errorf("mutation copy exceeds bounded project size")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dest, rel), data, info.Mode().Perm())
	})
}
