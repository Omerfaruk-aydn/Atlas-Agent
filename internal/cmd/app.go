package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/client"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/server"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/version"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/webui"
	"github.com/spf13/cobra"
)

// The desktop app shares nothing with `atlas-agent web` but the server and
// gateway packages: it has its own flags, its own renderer and its own
// process model. `app` only finds and starts the Electron shell; the shell
// owns a hidden `app serve` child that lives as long as its windows do.
var (
	appPort       int
	appRenderer   string
	appDevOrigins []string
)

func init() {
	appServeCmd.Flags().StringVar(&appRenderer, "renderer", "", "Directory holding the built app renderer")
	appServeCmd.Flags().IntVar(&appPort, "port", 0, "Loopback port for the app's gateway (0 picks a free one)")
	appServeCmd.Flags().StringSliceVar(&appDevOrigins, "dev-origin", nil, "Extra origin allowed to call the API, e.g. a Vite dev server")
	_ = appServeCmd.MarkFlagRequired("renderer")
	appCmd.AddCommand(appServeCmd)
	rootCmd.AddCommand(appCmd)
}

var appCmd = &cobra.Command{
	Use:   "app [path]",
	Short: "Open the ATLAS-AGENT desktop app",
	Long: `Open a project in the ATLAS-AGENT desktop app.

The command starts the desktop app and returns at once, like an editor's
command line launcher. Each project gets its own window; opening a project
that already has one brings that window forward. Closing the last window stops
the app. Runs already started keep going on the shared ATLAS-AGENT server.

The app ships with the CLI: installers put it in an app directory next to the
executable. ATLAS_APP_DIR points somewhere else, and a source checkout works
after npm install in its app directory. ATLAS_APP_ELECTRON overrides the
Electron binary.`,
	Example: `
# Open the current directory
atlas-agent app

# Open another project in its own window
atlas-agent app ../other-project
  `,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project, err := resolveAppProject(cmd, args)
		if err != nil {
			return err
		}
		shell, err := findDesktopShell()
		if err != nil {
			return err
		}
		if err := startDesktopShell(shell, project); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Opening %s in Atlas.\n", project)
		return nil
	},
}

// appServeCmd is what the Electron shell runs. It is not meant for people.
var appServeCmd = &cobra.Command{
	Use:    "serve",
	Short:  "Run the desktop app's gateway (started by the app itself)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		err := runAppServe(cmd, os.Stdin)
		if err != nil {
			writeAppEvent(cmd.OutOrStdout(), appEvent{Type: "error", Message: err.Error()})
		}
		return err
	},
}

// appEvent is one line of the handshake between `app serve` and the shell.
type appEvent struct {
	Type    string `json:"type"`
	Port    int    `json:"port,omitempty"`
	Link    string `json:"link,omitempty"`
	Version string `json:"version,omitempty"`
	Message string `json:"message,omitempty"`
}

func writeAppEvent(w io.Writer, ev appEvent) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "%s\n", data)
}

// runAppServe serves the renderer and the gateway API until the shell goes
// away, which it notices as stdin closing. That holds even when the shell
// crashes, so a gateway never outlives its app.
func runAppServe(cmd *cobra.Command, stdin io.Reader) error {
	ctx := cmd.Context()
	if appPort < 0 || appPort > 65535 {
		return fmt.Errorf("invalid port %d", appPort)
	}
	renderer, err := filepath.Abs(appRenderer)
	if err != nil {
		return fmt.Errorf("resolve the renderer directory: %w", err)
	}
	hostURL, err := server.ParseHostURL(clientHost)
	if err != nil {
		return fmt.Errorf("invalid server host: %v", err)
	}
	if err := ensureServer(cmd, hostURL); err != nil {
		return err
	}
	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return err
	}
	c, err := client.NewClient(cwd, hostURL.Scheme, hostURL.Host)
	if err != nil {
		return err
	}
	if _, err := c.VersionInfo(ctx); err != nil {
		return fmt.Errorf("the Atlas server at %s does not answer: %w", clientHost, err)
	}

	gw, err := webui.New(webui.Options{
		Upstream:       c,
		Assets:         os.DirFS(renderer),
		ServerAddress:  clientHost,
		Version:        version.Version,
		DevOrigins:     appDevOrigins,
		Env:            os.Environ(),
		RecentProjects: recentWebProjects,
		Reconnect: func(context.Context) error {
			return ensureServer(cmd, hostURL)
		},
		Logger: slog.Default(),
	})
	if err != nil {
		if errors.Is(err, webui.ErrAssetsMissing) {
			return fmt.Errorf("no app renderer build in %s: run `npm run build` in the app directory", renderer)
		}
		return err
	}
	ln, err := gw.Listen(net.JoinHostPort("127.0.0.1", strconv.Itoa(appPort)))
	if err != nil {
		return err
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- gw.Serve(ln) }()

	port, err := strconv.Atoi(gw.Port())
	if err != nil {
		return fmt.Errorf("read the gateway port: %w", err)
	}
	writeAppEvent(cmd.OutOrStdout(), appEvent{Type: "ready", Port: port, Link: gw.LaunchURL(), Version: version.Version})

	shellGone := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, stdin)
		close(shellGone)
	}()
	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt)
	defer signal.Stop(sigch)

	var failed error
	select {
	case <-shellGone:
	case <-sigch:
	case <-ctx.Done():
	case failed = <-serveErr:
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return errors.Join(failed, gw.Shutdown(shutdownCtx))
}

// resolveAppProject returns the absolute directory to open: the path argument
// or, without one, the working directory.
func resolveAppProject(cmd *cobra.Command, args []string) (string, error) {
	project := ""
	if len(args) == 1 {
		project = args[0]
	} else {
		cwd, err := ResolveCwd(cmd)
		if err != nil {
			return "", err
		}
		project = cwd
	}
	abs, err := filepath.Abs(project)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", project, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("open project: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("open project: %s is not a directory", abs)
	}
	return abs, nil
}

// packagedAppExe is the desktop app's executable in a release.
const packagedAppExe = "Atlas.exe"

// desktopShell is a located Electron binary and, for a source checkout, the
// app directory it runs. A packaged app carries its code and has no Dir.
type desktopShell struct {
	Electron string
	Dir      string
}

// arguments are what the shell is started with.
func (s desktopShell) arguments(project, core string) []string {
	args := []string{"--project=" + project, "--atlas-bin=" + core}
	if s.Dir != "" {
		return append([]string{s.Dir}, args...)
	}
	return args
}

// workDir is where the shell starts: its code, or the executable's folder.
func (s desktopShell) workDir() string {
	if s.Dir != "" {
		return s.Dir
	}
	return filepath.Dir(s.Electron)
}

// findDesktopShell looks for the Electron shell. It returns an error that
// says what to do when none is installed.
func findDesktopShell() (desktopShell, error) {
	var dirs []string
	if d := os.Getenv("ATLAS_APP_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		dirs = append(dirs, filepath.Join(exeDir, "app"), filepath.Join(exeDir, "..", "app"))
	}
	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			dirs = append(dirs, filepath.Join(dir, "app"))
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}

	for _, dir := range dirs {
		packaged := filepath.Join(dir, packagedAppExe)
		if info, err := os.Stat(packaged); err == nil && !info.IsDir() {
			return desktopShell{Electron: packaged}, nil
		}
		if !isAtlasAppDir(dir) {
			continue
		}
		bin := os.Getenv("ATLAS_APP_ELECTRON")
		if bin == "" {
			bin = electronBinary(dir)
		}
		if bin == "" {
			continue
		}
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		return desktopShell{Electron: bin, Dir: dir}, nil
	}
	return desktopShell{}, errors.New("the desktop app is not installed: reinstall atlas-agent on Windows, or run `npm install` in a source checkout's app directory")
}

// isAtlasAppDir reports whether dir holds the Atlas Electron shell.
func isAtlasAppDir(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	return err == nil && strings.Contains(string(data), `"name": "atlas-app"`)
}

// electronBinary returns the Electron executable that npm installed under
// dir, or "" when it is missing.
func electronBinary(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "node_modules", "electron", "path.txt"))
	if err != nil {
		return ""
	}
	rel := strings.TrimSpace(string(data))
	if rel == "" {
		return ""
	}
	return filepath.Join(dir, "node_modules", "electron", "dist", filepath.FromSlash(rel))
}

// startDesktopShell starts the app on project and lets it go: the app must
// survive this command and the terminal it ran in. A running app takes the
// request over through its single-instance lock. This binary is passed along
// so the gateway the app starts is the same version.
func startDesktopShell(shell desktopShell, project string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find the atlas-agent binary: %w", err)
	}
	// The shell outlives this command, so it must not hang off its context.
	proc := exec.CommandContext(context.Background(), shell.Electron, shell.arguments(project, exe)...)
	proc.Dir = shell.workDir()
	proc.Env = desktopShellEnv(os.Environ())
	detachDesktopShell(proc)
	if err := proc.Start(); err != nil {
		return fmt.Errorf("start the desktop app: %w", err)
	}
	return proc.Process.Release()
}

// desktopShellEnv drops the variable that would turn Electron into a plain
// Node runtime.
func desktopShellEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(strings.ToUpper(kv), "ELECTRON_RUN_AS_NODE=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
