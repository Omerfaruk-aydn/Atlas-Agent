package verification

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMutationNeverWritesOriginalEvenOnPanicOrCancellation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"survive", "kill", "panic", "cancel", "timeout", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			source := []byte("package fixture\nfunc Live(x int) bool { return x <= 3 }\n")
			require.NoError(t, os.WriteFile(filepath.Join(root, "core.go"), source, 0o640))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calls := 0
			run := func(ctx context.Context, dir, command string) (int, string, error) {
				require.NotEqual(t, root, dir)
				calls++
				if calls == 1 {
					return 0, "ok", nil
				}
				switch mode {
				case "timeout":
					<-ctx.Done()
					return 0, "", ctx.Err()
				case "invalid":
					return 1, "FAIL fixture [build failed]", nil
				case "panic":
					panic("runner panic")
				case "cancel":
					cancel()
					return 0, "", ctx.Err()
				case "kill":
					return 1, "--- FAIL: TestLive", nil
				}
				return 0, "ok", nil
			}
			report, err := RunMutations(ctx, root, []Change{{Path: "core.go", After: string(source), Lines: []int{2}}}, MutationOptions{Max: 2, Timeout: time.Second, Command: "go test ./..."}, run)
			data, readErr := os.ReadFile(filepath.Join(root, "core.go"))
			require.NoError(t, readErr)
			require.Equal(t, source, data)
			switch mode {
			case "panic", "cancel", "timeout":
				require.Error(t, err)
				if mode == "timeout" {
					require.ErrorIs(t, err, context.DeadlineExceeded)
				}
			case "invalid":
				require.NoError(t, err)
				require.Positive(t, report.Invalid)
				require.Zero(t, report.Killed)
			case "kill":
				require.NoError(t, err)
				require.Positive(t, report.Killed)
			case "survive":
				require.NoError(t, err)
				require.Positive(t, report.Survived)
			}
		})
	}
}

func TestRealGoMutantSeparatesWeakAndBoundaryTests(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.26.0\n"), 0o600))
	source := "package fixture\nfunc Live(x int) bool { return x <= 3 }\n"
	require.NoError(t, os.WriteFile(filepath.Join(root, "core.go"), []byte(source), 0o600))
	run := func(ctx context.Context, dir, _ string) (int, string, error) {
		cmd := exec.CommandContext(ctx, "go", "test", "-p", "1", "-count=1", "./...")
		cmd.Dir = dir
		data, err := cmd.CombinedOutput()
		if err != nil {
			if e, ok := err.(*exec.ExitError); ok {
				return e.ExitCode(), string(data), nil
			}
			return 0, string(data), err
		}
		return 0, string(data), nil
	}
	for _, boundary := range []bool{false, true} {
		value := "1"
		if boundary {
			value = "3"
		}
		test := "package fixture\nimport \"testing\"\nfunc TestLive(t *testing.T) { if !Live(" + value + ") { t.Fatal(\"boundary\") } }\n"
		require.NoError(t, os.WriteFile(filepath.Join(root, "core_test.go"), []byte(test), 0o600))
		r, err := RunMutations(t.Context(), root, []Change{{Path: "core.go", After: source, Lines: []int{2}}}, MutationOptions{Timeout: 30 * time.Second}, run)
		require.NoError(t, err)
		if boundary {
			require.Equal(t, 1, r.Killed)
			require.Zero(t, r.Survived)
		} else {
			require.Equal(t, 1, r.Survived)
		}
		data, err := os.ReadFile(filepath.Join(root, "core.go"))
		require.NoError(t, err)
		require.Equal(t, source, string(data))
	}
}

func TestMutationsUseOnlyChangedLines(t *testing.T) {
	t.Parallel()
	source := "package fixture\nfunc One(x int) bool { return x <= 1 }\nfunc Two(x int) bool { return x <= 2 }\n"
	mutants, err := (GoMutator{}).Mutants(Change{Path: "core.go", After: source, Lines: []int{3}}, 4)
	require.NoError(t, err)
	require.NotEmpty(t, mutants)
	for _, m := range mutants {
		require.Contains(t, string(m.Source), "return x <= 1")
		require.NotEqual(t, source, string(m.Source))
	}
}
