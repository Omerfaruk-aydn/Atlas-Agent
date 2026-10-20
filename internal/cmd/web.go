package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/client"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/projects"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/server"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/version"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/webui"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

// defaultWebPort is the loopback port `atlas-agent web` listens on.
const defaultWebPort = 7788

var (
	webPort       int
	webOpen       bool
	webDevOrigins []string
)

func init() {
	webCmd.Flags().IntVar(&webPort, "port", defaultWebPort, "Loopback port for the web UI (0 picks a free one)")
	webCmd.Flags().BoolVar(&webOpen, "open", true, "Open the web UI in the default browser")
	webCmd.Flags().StringSliceVar(&webDevOrigins, "dev-origin", nil, "Extra origin allowed to call the API, e.g. a Vite dev server")
	_ = webCmd.Flags().MarkHidden("dev-origin")
	rootCmd.AddCommand(webCmd)
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Open the ATLAS-AGENT web UI",
	Long: `Serve the ATLAS-AGENT web UI on a loopback address.

The web UI is a client of the shared ATLAS-AGENT server, like the terminal UI
started with ATLAS_AGENT_CLIENT_SERVER=1: sessions, runs, questions and
permission requests are the same ones the terminal sees. The command starts
the server if none is running and opens the current directory as a project.

Sign-in links work once and expire after ten minutes. Running the command
again while the web UI is up prints a fresh link instead of starting a
second copy.`,
	Example: `
# Open the web UI for the current directory
atlas-agent web

# Use another port and only print the link
atlas-agent web --port 8080 --open=false
  `,
	Args: cobra.NoArgs,
	RunE: runWeb,
}

func runWeb(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	if webPort < 0 || webPort > 65535 {
		return fmt.Errorf("invalid port %d", webPort)
	}
	stateDir := config.GlobalCacheDir()

	// A gateway already serving this port only needs a new link.
	if webPort != 0 {
		if link, err := webui.RequestLaunchURL(ctx, stateDir, strconv.Itoa(webPort)); err == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "The Atlas web UI is already running.\n\n  Open: %s\n\nThe link works once and expires in 10 minutes.\n", link)
			openWebBrowser(cmd, link)
			return nil
		}
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
	serverInfo, err := c.VersionInfo(ctx)
	if err != nil {
		return fmt.Errorf("the Atlas server at %s does not answer: %w", clientHost, err)
	}

	gw, err := webui.New(webui.Options{
		Upstream:       c,
		ServerAddress:  clientHost,
		Version:        version.Version,
		DevOrigins:     webDevOrigins,
		Env:            os.Environ(),
		RecentProjects: recentWebProjects,
		Reconnect: func(context.Context) error {
			return ensureServer(cmd, hostURL)
		},
		Logger: slog.Default(),
	})
	if err != nil {
		return err
	}
	ln, err := gw.Listen(net.JoinHostPort("127.0.0.1", strconv.Itoa(webPort)))
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "listen" {
			return fmt.Errorf("port %d is in use by another program; choose one with --port (or --port 0 for any free port)", webPort)
		}
		return err
	}
	removeControl, err := gw.WriteControlFile(stateDir)
	if err != nil {
		slog.Warn("Failed to write web control file", "error", err)
		removeControl = func() {}
	}
	defer removeControl()

	serveErr := make(chan error, 1)
	go func() { serveErr <- gw.Serve(ln) }()

	// Open the working directory as the first project, so the browser
	// lands somewhere useful.
	openCtx, cancelOpen := context.WithTimeout(ctx, 2*time.Minute)
	ws, openErr := gw.OpenWorkspace(openCtx, cwd)
	cancelOpen()

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "ATLAS-AGENT web UI\n\n")
	fmt.Fprintf(out, "  Server:    %s (version %s)\n", clientHost, serverInfo.Version)
	if openErr != nil {
		fmt.Fprintf(out, "  Project:   could not open %s: %v\n", cwd, openErr)
	} else {
		fmt.Fprintf(out, "  Project:   %s (workspace %s)\n", ws.Path, ws.ID)
	}
	link := gw.LaunchURL()
	fmt.Fprintf(out, "  Address:   http://127.0.0.1:%s\n\n", gw.Port())
	fmt.Fprintf(out, "  Open: %s\n\n", link)
	fmt.Fprintf(out, "The link signs this browser in once and expires in 10 minutes.\n")
	fmt.Fprintf(out, "To share live sessions with the terminal UI, start it with\nATLAS_AGENT_CLIENT_SERVER=1; it then joins this same server.\n")
	fmt.Fprintf(out, "Press Ctrl+C to stop the web UI. Runs in progress keep going while\nanother client uses the project; the server is never shut down by this command.\n")
	openWebBrowser(cmd, link)

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt)
	defer signal.Stop(sigch)
	select {
	case <-sigch:
	case <-ctx.Done():
	case err := <-serveErr:
		if err != nil {
			return err
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return gw.Shutdown(shutdownCtx)
}

func openWebBrowser(cmd *cobra.Command, link string) {
	if !webOpen {
		return
	}
	if err := browser.OpenURL(link); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Could not open a browser (%v); open the link above yourself.\n", err)
	}
}

// recentWebProjects lists the projects Atlas has been used in, most
// recent first.
func recentWebProjects() []webui.Project {
	list, err := projects.List()
	if err != nil {
		return nil
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].LastAccessed.After(list[j].LastAccessed) })
	out := make([]webui.Project, 0, len(list))
	for _, p := range list {
		if info, err := os.Stat(p.Path); err != nil || !info.IsDir() {
			continue
		}
		out = append(out, webui.Project{Path: p.Path, Name: filepath.Base(p.Path), LastUsed: p.LastAccessed.UnixMilli()})
		if len(out) == 50 {
			break
		}
	}
	return out
}
