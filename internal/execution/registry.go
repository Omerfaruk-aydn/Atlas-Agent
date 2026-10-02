package execution

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

type bindingKey struct{}

type readOnlyKey struct{}

func WithReadOnly(ctx context.Context) context.Context {
	return context.WithValue(ctx, readOnlyKey{}, true)
}

func IsReadOnly(ctx context.Context) bool { value, _ := ctx.Value(readOnlyKey{}).(bool); return value }

type Binding struct {
	Root    string
	Store   *engineering.Store
	Factory func(context.Context) (Runner, error)
}

func WithBinding(ctx context.Context, binding Binding) context.Context {
	return context.WithValue(ctx, bindingKey{}, binding)
}

func HasBinding(ctx context.Context) bool { _, ok := ctx.Value(bindingKey{}).(Binding); return ok }

func GetBinding(ctx context.Context) (Binding, bool) {
	value, ok := ctx.Value(bindingKey{}).(Binding)
	return value, ok
}

func ShellRequest(root, cwd, script string) (Request, error) {
	relative, err := filepath.Rel(root, cwd)
	if err != nil || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Request{}, fmt.Errorf("isolated working directory escapes project root")
	}
	if err := validateRoot(cwd); err != nil {
		return Request{}, err
	}
	directory := "/workspace"
	if relative != "." {
		directory += "/" + filepath.ToSlash(relative)
	}
	return Request{Root: root, Argv: []string{"/bin/sh", "-c", `cd "$1" && exec /bin/sh -c "$2"`, "atlas-shell", directory, script}}, nil
}

// ArgvRequest transports arguments independently through a static wrapper.
// No argument is interpolated into the container shell source.
func ArgvRequest(root, cwd string, argv []string) (Request, error) {
	request, err := ShellRequest(root, cwd, "")
	if err != nil {
		return Request{}, err
	}
	if len(argv) == 0 || len(argv) > 128 || argv[0] == "" {
		return Request{}, fmt.Errorf("invalid literal command arguments")
	}
	total := 0
	for _, arg := range argv {
		total += len(arg)
		if strings.ContainsRune(arg, 0) || len(arg) > 4096 || total > 16*1024 {
			return Request{}, fmt.Errorf("literal command arguments exceed bounds")
		}
	}
	directory := request.Argv[4]
	request.Argv = append([]string{"/bin/sh", "-c", `cd "$1" && shift && exec "$@"`, "atlas-argv", directory}, argv...)
	return request, nil
}

func RunArgv(ctx context.Context, cwd string, argv, env []string, stdout, stderr io.Writer) error {
	binding, ok := GetBinding(ctx)
	if !ok {
		return fmt.Errorf("isolated execution binding is unavailable")
	}
	request, err := ArgvRequest(binding.Root, cwd, argv)
	if err != nil {
		return err
	}
	return runRequest(ctx, binding, request, env, nil, stdout, stderr)
}

type ExitError struct{ Code int }

func (e ExitError) Error() string {
	return fmt.Sprintf("isolated command exited with status %d", e.Code)
}

// RunShell executes the entire script in the configured container shell.
func RunShell(ctx context.Context, cwd, script string, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	binding, ok := ctx.Value(bindingKey{}).(Binding)
	if !ok || binding.Factory == nil || binding.Store == nil {
		return fmt.Errorf("isolated execution binding is unavailable")
	}
	request, err := ShellRequest(binding.Root, cwd, script)
	if err != nil {
		return err
	}
	return runRequest(ctx, binding, request, env, stdin, stdout, stderr)
}

func runRequest(ctx context.Context, binding Binding, request Request, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if binding.Factory == nil || binding.Store == nil {
		return fmt.Errorf("isolated execution binding is unavailable")
	}
	request.Env, request.Stdin = env, stdin
	scope := engineering.GetScope(ctx, "")
	request.SessionID, request.TaskID = scope.SessionID, scope.TaskID
	runner, err := binding.Factory(ctx)
	if err != nil {
		return err
	}
	if runner == nil {
		return fmt.Errorf("required isolation returned no runner")
	}
	result, err := runner.Run(ctx, request)
	if err != nil {
		return err
	}
	if !result.Done || result.ExitCode == nil {
		return fmt.Errorf("isolated execution has no observed exit result")
	}
	for _, stream := range []struct {
		ref    engineering.ArtifactRef
		writer io.Writer
	}{{result.OutputRef, stdout}, {result.ErrorRef, stderr}} {
		if stream.ref.Hash == "" {
			return fmt.Errorf("isolated execution output is unavailable")
		}
		data, err := binding.Store.ReadArtifact(ctx, stream.ref)
		if err != nil {
			return err
		}
		if stream.writer != nil {
			if _, err := stream.writer.Write(data); err != nil {
				return err
			}
		}
	}
	if result.Status != "exited" {
		return fmt.Errorf("isolated execution ended with %s", result.Status)
	}
	if *result.ExitCode != 0 {
		return ExitError{Code: *result.ExitCode}
	}
	return nil
}
