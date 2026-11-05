package cmd

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/client"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/connections"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/dots"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/projects"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/server"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/version"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/voice"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/webui"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

// defaultWebPort is the loopback port `atlas-agent web` listens on.
const defaultWebPort = 7788

// defaultDotsPort is the loopback port `atlas-agent dots` listens on, so it
// can run beside the web UI.
const defaultDotsPort = 7790

var (
	webPort       int
	webOpen       bool
	webDevOrigins []string
	webDots       bool
)

func init() {
	dotsCmd.Flags().IntVar(&webPort, "port", defaultDotsPort, "Loopback port for Atlas Dots (0 picks a free one)")
	dotsCmd.Flags().BoolVar(&webOpen, "open", true, "Open Atlas Dots in the default browser")
	rootCmd.AddCommand(dotsCmd)
}

// dotsCmd opens the web UI straight on Atlas Dots.
var dotsCmd = &cobra.Command{
	Use:   "dots",
	Short: "Open Atlas Dots, your always-on personal agents",
	Long: `Open Atlas Dots in the browser.

Each dot is a personal agent with its own name, look, voice, personality and
permission rules, and its own workspace folder. Dots run on the shared
ATLAS-AGENT server, so their sessions are ordinary Atlas sessions.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		webDots = true
		if !cmd.Flags().Changed("port") {
			webPort = defaultDotsPort
		}
		return runWeb(cmd, args)
	},
}

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

// elevenLabsAccessToken reads the account token saved by the voice
// settings sign-in. It is used only when no ElevenLabs API key is set.
func elevenLabsAccessToken() string {
	dir := filepath.Dir(config.GlobalConfigData())
	store, err := config.Load(dir, dir, false)
	if err != nil || store == nil {
		return ""
	}
	account, ok := store.Config().MCP["elevenlabs"]
	if !ok || account.OAuthToken == nil {
		return ""
	}
	return strings.TrimSpace(account.OAuthToken.AccessToken)
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
			product := "The Atlas web UI"
			if webDots {
				product = "Atlas Dots"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s is already running.\n\n  Open: %s\n\nThe link works once and expires in 10 minutes.\n", product, link)
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

	dataDir := filepath.Dir(config.GlobalConfigData())
	dotStore, err := dots.Open(filepath.Join(dataDir, "dots"))
	if err != nil {
		slog.Warn("Atlas Dots is unavailable", "error", err)
		dotStore = nil
	}
	var automation *dots.Automation
	if dotStore != nil {
		if automation, err = dots.OpenAutomation(filepath.Join(dataDir, "dots")); err != nil {
			slog.Warn("Atlas Dots automation is unavailable", "error", err)
			automation = nil
		}
	}
	connEnv := connections.Env(dataDir)
	voices := &dots.Voices{Key: func() string {
		if key := cmp.Or(connEnv(connections.ElevenLabsAPIKey), os.Getenv("ELEVENLABS_API_KEY")); key != "" {
			return key
		}
		return elevenLabsAccessToken()
	}}

	gw, err := webui.New(webui.Options{
		Dots:           dotStore,
		DotsOnly:       webDots,
		Automation:     automation,
		Notify:         dotNotifier(cwd, dataDir, connEnv),
		Approvals:      dotApprovals(cwd, connEnv),
		Voices:         voices,
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
	out := cmd.OutOrStdout()
	if webDots {
		return runDots(cmd, gw, serveErr, out, serverInfo.Version, dotStore != nil)
	}
	openCtx, cancelOpen := context.WithTimeout(ctx, 2*time.Minute)
	ws, openErr := gw.OpenWorkspace(openCtx, cwd)
	cancelOpen()

	fmt.Fprintf(out, "ATLAS-AGENT web UI\n\n")
	fmt.Fprintf(out, "  Server:    %s (version %s)\n", clientHost, serverInfo.Version)
	if openErr != nil {
		fmt.Fprintf(out, "  Project:   could not open %s: %v\n", cwd, openErr)
	} else {
		fmt.Fprintf(out, "  Project:   %s (workspace %s)\n", ws.Path, ws.ID)
	}
	link := gw.LaunchURL()
	fmt.Fprintf(out, "  Address:   http://127.0.0.1:%s\n\n", gw.Port())
	fmt.Fprintf(out, "  Open: %s\n", link)
	if dotStore != nil {
		fmt.Fprintf(out, "  Dots: http://127.0.0.1:%s/dots (after signing in)\n", gw.Port())
	}
	fmt.Fprintln(out)
	fmt.Fprintf(out, "The link signs this browser in once and expires in 10 minutes.\n")
	fmt.Fprintf(out, "To share live sessions with the terminal UI, start it with\nATLAS_AGENT_CLIENT_SERVER=1; it then joins this same server.\n")
	fmt.Fprintf(out, "Press Ctrl+C to stop the web UI. Runs in progress keep going while\nanother client uses the project; the server is never shut down by this command.\n")
	openWebBrowser(cmd, link)
	return waitAndShutdown(ctx, gw, serveErr)
}

// runDots is the Atlas Dots entry point: its own banner and no project
// opened, since dots bring their own workspaces.
func runDots(cmd *cobra.Command, gw *webui.Gateway, serveErr <-chan error, out io.Writer, serverVersion string, ok bool) error {
	if !ok {
		return errors.New("could not open the Atlas Dots data directory")
	}
	link := gw.LaunchURL()
	fmt.Fprintf(out, "Atlas Dots\n\n")
	fmt.Fprintf(out, "  Address:   http://127.0.0.1:%s\n", gw.Port())
	fmt.Fprintf(out, "  Server:    ATLAS-AGENT %s\n\n", serverVersion)
	fmt.Fprintf(out, "  Open: %s\n\n", link)
	fmt.Fprintf(out, "The link signs this browser in once and expires in 10 minutes.\n")
	fmt.Fprintf(out, "Dots keep their schedules and webhooks while this is running.\nPress Ctrl+C to stop; runs already started keep going on the server.\n")
	openWebBrowser(cmd, link)
	return waitAndShutdown(cmd.Context(), gw, serveErr)
}

func waitAndShutdown(ctx context.Context, gw *webui.Gateway, serveErr <-chan error) error {
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

// dotApprovals returns the Telegram approval channel when a bot and chat
// are configured, and nil otherwise. Unlike notices it is read once, at
// start, because it holds the bot's update offset.
func dotApprovals(cwd string, env func(string) string) webui.ApprovalChannel {
	cfg, err := config.Init(cwd, "", false)
	if err != nil {
		return nil
	}
	vc := voice.Config(cfg.Config().Tools.VoiceCalls).WithDefaults()
	token, err := vc.Telegram.BotToken.Resolve(env)
	if err != nil || token.Reveal() == "" || vc.Telegram.ChatID == "" {
		return nil
	}
	return &webui.TelegramApprovals{Token: token.Reveal(), ChatID: vc.Telegram.ChatID}
}

// dotNotifier returns a function that sends a dot's notice to the owner's
// Telegram chat when the Telegram connection is set up, and does nothing
// otherwise. The configuration is read when a notice is sent, so a
// connection added while Atlas runs takes effect at once.
func dotNotifier(cwd, dataDir string, env func(string) string) func(context.Context, webui.Notice) error {
	return func(ctx context.Context, n webui.Notice) error {
		cfg, err := config.Init(cwd, "", false)
		if err != nil {
			return err
		}
		vc := voice.Config(cfg.Config().Tools.VoiceCalls).WithDefaults()
		token, err := vc.Telegram.BotToken.Resolve(env)
		if err != nil || vc.Telegram.ChatID == "" {
			return nil
		}
		text := n.Title
		if n.Body != "" {
			text += "\n" + n.Body
		}
		return (&voice.TelegramNotifier{Token: token, ChatID: vc.Telegram.ChatID}).Notify(ctx, text)
	}
}
