package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/sandbox"
	"github.com/spf13/cobra"
)

var doctorJSON bool

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that this workspace is set up to run",
	Long: "Check the things a session needs before it starts: a writable data directory, a working " +
		"database, a configured provider and model, and the commands the configured MCP and LSP " +
		"servers are supposed to run. Reports what is wrong rather than fixing it. " +
		"Use --json for machine-readable output.",
	Args: cobra.NoArgs,
	RunE: runDoctor,
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output in JSON format")
	rootCmd.AddCommand(doctorCmd)
}

// checkStatus is how a single check came out. A warning is something a
// session can start without; a failure is not.
type checkStatus int

const (
	statusOK checkStatus = iota
	statusWarn
	statusFail
)

func (s checkStatus) String() string {
	switch s {
	case statusWarn:
		return "warn"
	case statusFail:
		return "fail"
	default:
		return "ok"
	}
}

type checkResult struct {
	Name   string
	Status checkStatus
	Detail string
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	cwd, err := ResolveCwd(cmd)
	if err != nil {
		return err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	debug, _ := cmd.Flags().GetBool("debug")

	cfg, err := config.Init(cwd, dataDir, debug)
	if err != nil {
		return fmt.Errorf("failed to initialize config: %w", err)
	}
	return reportDiagnostics(cmd, cfg)
}

// reportDiagnostics is split out from runDoctor for the same reason
// listProviders is split from runProviderList: a test can build a
// *config.ConfigStore directly but cannot inject one into a function that
// calls config.Init itself.
func reportDiagnostics(cmd *cobra.Command, cfg *config.ConfigStore) error {
	return printChecks(cmd.OutOrStdout(), diagnose(cmd.Context(), cfg))
}

// jsonCheckResult is checkResult's wire form: Status renders as its string
// name rather than the underlying int, since a consumer that is not this
// package has no reason to know the iota ordering.
type jsonCheckResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// printChecks writes the report and turns any failure into an error, so a
// script can act on the exit status instead of parsing the output.
func printChecks(out io.Writer, results []checkResult) error {
	failures := countFailures(results)

	if doctorJSON {
		wire := make([]jsonCheckResult, len(results))
		for i, r := range results {
			wire[i] = jsonCheckResult{Name: r.Name, Status: r.Status.String(), Detail: r.Detail}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(wire); err != nil {
			return err
		}
	} else {
		for _, r := range results {
			fmt.Fprintf(out, "[%s] %s: %s\n", r.Status, r.Name, r.Detail)
		}
	}

	if failures > 0 {
		return fmt.Errorf("%d check(s) failed", failures)
	}
	return nil
}

func countFailures(results []checkResult) int {
	var failures int
	for _, r := range results {
		if r.Status == statusFail {
			failures++
		}
	}
	return failures
}

func diagnose(ctx context.Context, cfg *config.ConfigStore) []checkResult {
	results := []checkResult{
		checkDataDirectory(cfg),
		checkDatabase(ctx, cfg),
		checkProviders(cfg),
		checkModels(cfg),
	}
	results = append(results, checkMCPCommands(cfg)...)
	results = append(results, checkLSPCommands(cfg)...)
	if r := checkBrowser(cfg); r != nil {
		results = append(results, *r)
	}
	if r := checkDebugger(cfg); r != nil {
		results = append(results, *r)
	}
	if r := checkComputer(cfg); r != nil {
		results = append(results, *r)
	}
	if r := checkSandbox(cfg); r != nil {
		results = append(results, *r)
	}
	if r := checkTeams(cfg); r != nil {
		results = append(results, *r)
	}
	return results
}

// checkTeams reports whether the team_send/team_read tools are turned
// on. Unlike browser/debugger/sandbox there is nothing external to
// verify -- the mailbox is in-memory -- so this is only ever a status
// line, never a warning or failure, and nothing is reported when the
// tool is off (same opt-in reasoning as the others).
func checkTeams(cfg *config.ConfigStore) *checkResult {
	if !cfg.Config().Tools.Teams.IsEnabled() {
		return nil
	}
	return &checkResult{"teams", statusOK, "in-memory mailbox active"}
}

// browserCandidates are the binary names checkBrowser looks for on PATH
// when the user hasn't set an explicit executable_path.
var browserCandidates = []string{
	"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome", "msedge",
}

// checkBrowser reports whether the browser tool, if turned on, has a
// Chrome/Chromium/Edge binary to launch. It reports nothing when the tool
// is off, since it is opt-in and a missing browser is irrelevant until
// then. A miss is a warning, not a failure: chromedp also probes common
// install directories beyond PATH, so this check can under-report a
// browser that is actually there -- the tool itself gives a clearer error
// the first time it tries to launch one.
func checkBrowser(cfg *config.ConfigStore) *checkResult {
	b := cfg.Config().Tools.Browser
	if !b.IsEnabled() {
		return nil
	}

	if b.ExecutablePath != "" {
		if _, err := os.Stat(b.ExecutablePath); err != nil {
			return &checkResult{"browser", statusFail, fmt.Sprintf("executable_path %s: %s", b.ExecutablePath, err)}
		}
		return &checkResult{"browser", statusOK, b.ExecutablePath}
	}

	for _, name := range browserCandidates {
		if path, err := exec.LookPath(name); err == nil {
			return &checkResult{"browser", statusOK, path}
		}
	}
	// Chrome/Edge installers do not put their binary on PATH by default,
	// so this is the common case, not an edge case: check the well-known
	// per-OS install locations chromedp itself already probes internally
	// before reporting a miss.
	for _, path := range commonBrowserPaths() {
		if _, err := os.Stat(path); err == nil {
			return &checkResult{"browser", statusOK, path}
		}
	}
	return &checkResult{"browser", statusWarn, "no Chrome/Chromium/Edge found on PATH or in the usual install locations; set tools.browser.executable_path if one is installed elsewhere"}
}

// commonBrowserPaths lists the well-known install locations for Chrome,
// Chromium, and Edge outside PATH, for the current OS. Best-effort: not
// exhaustive, just the common installer defaults.
func commonBrowserPaths() []string {
	switch runtime.GOOS {
	case "windows":
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		localAppData := os.Getenv("LOCALAPPDATA")
		var paths []string
		for _, base := range []string{programFiles, programFilesX86, localAppData} {
			if base == "" {
				continue
			}
			paths = append(paths,
				filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(base, "Chromium", "Application", "chrome.exe"),
				filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
		return paths
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	default: // linux and everything else
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/google-chrome-stable",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/snap/bin/chromium",
			"/opt/google/chrome/chrome",
		}
	}
}

// checkDebugger reports whether the debugger tool, if turned on, has a
// `dlv` (Delve) binary to launch. Reports nothing when the tool is off,
// same reasoning as checkBrowser: it's opt-in, so a missing debugger is
// irrelevant until then.
func checkDebugger(cfg *config.ConfigStore) *checkResult {
	d := cfg.Config().Tools.Debugger
	if !d.IsEnabled() {
		return nil
	}

	if d.DlvPath != "" {
		if _, err := os.Stat(d.DlvPath); err != nil {
			return &checkResult{"debugger", statusFail, fmt.Sprintf("dlv_path %s: %s", d.DlvPath, err)}
		}
		return &checkResult{"debugger", statusOK, d.DlvPath}
	}

	if path, err := exec.LookPath("dlv"); err == nil {
		return &checkResult{"debugger", statusOK, path}
	}
	return &checkResult{"debugger", statusWarn, "dlv (Delve) not found on PATH; install it with `go install github.com/go-delve/delve/cmd/dlv@latest` or set tools.debugger.dlv_path"}
}

// checkComputer reports whether the computer-use tool, if turned on,
// has a desktop it can actually drive. Reports nothing when the tool
// is off -- opt-in, same reasoning as the browser and debugger checks.
// Off Windows the tool refuses every call, so an enabled tool there is
// a warning, not an error: the agent explains the miss on first use,
// but surfacing it here saves a wasted round trip.
func checkComputer(cfg *config.ConfigStore) *checkResult {
	if !cfg.Config().Tools.Computer.IsEnabled() {
		return nil
	}
	if runtime.GOOS != "windows" {
		return &checkResult{"computer", statusWarn, fmt.Sprintf("not supported on %s; the computer tool reports itself unavailable there", runtime.GOOS)}
	}
	return &checkResult{"computer", statusOK, "desktop control active"}
}

// checkSandbox reports whether sandbox.enabled will actually take effect
// on this OS. Reports nothing when it's off -- opt-in, same reasoning as
// the browser and debugger checks. Unlike those, an unsupported OS here
// is a warning even though the feature is "on": bash commands still run
// fine without containment, they're just not contained, which is worth
// surfacing rather than silently accepting.
func checkSandbox(cfg *config.ConfigStore) *checkResult {
	sb := cfg.Config().Options.Sandbox
	if sb == nil || !sb.Enabled {
		return nil
	}
	if !sandbox.Supported() {
		return &checkResult{"sandbox", statusWarn, fmt.Sprintf("not supported on %s; shell commands run without containment", runtime.GOOS)}
	}
	return &checkResult{"sandbox", statusOK, fmt.Sprintf("Job Object containment active (max_processes=%d)", sb.MaxProcessesOrDefault())}
}

func checkDataDirectory(cfg *config.ConfigStore) checkResult {
	dir := cfg.Config().Options.DataDirectory
	if dir == "" {
		return checkResult{"data directory", statusFail, "not configured"}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return checkResult{"data directory", statusFail, fmt.Sprintf("%s: %s", dir, err)}
	}

	// Writability is what actually matters, and on Windows the mode bits
	// do not tell us -- so write something and take it away again.
	probe := filepath.Join(dir, ".atlas-doctor")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return checkResult{"data directory", statusFail, fmt.Sprintf("%s is not writable: %s", dir, err)}
	}
	_ = os.Remove(probe)
	return checkResult{"data directory", statusOK, dir}
}

func checkDatabase(ctx context.Context, cfg *config.ConfigStore) checkResult {
	dir := cfg.Config().Options.DataDirectory
	if dir == "" {
		return checkResult{"database", statusFail, "no data directory to open it in"}
	}

	conn, err := db.Connect(ctx, dir)
	if err != nil {
		return checkResult{"database", statusFail, err.Error()}
	}
	defer func() { _ = db.Release(dir) }()

	if err := conn.PingContext(ctx); err != nil {
		return checkResult{"database", statusFail, err.Error()}
	}
	return checkResult{"database", statusOK, filepath.Join(dir, "atlas.db")}
}

func checkProviders(cfg *config.ConfigStore) checkResult {
	var enabled, withKey int
	for _, providerCfg := range cfg.Config().Providers.Seq2() {
		if providerCfg.Disable {
			continue
		}
		enabled++
		if providerCfg.APIKey == "" {
			continue
		}
		if resolved, err := cfg.Resolve(providerCfg.APIKey); err == nil && resolved != "" {
			withKey++
		}
	}

	switch {
	case enabled == 0:
		return checkResult{"providers", statusFail, "none configured; run `atlas login` or set an API key"}
	case withKey == 0:
		// Not a failure: a local endpoint needs no key at all.
		return checkResult{"providers", statusWarn, fmt.Sprintf("%d configured, none with a resolvable API key", enabled)}
	default:
		return checkResult{"providers", statusOK, fmt.Sprintf("%d configured, %d with an API key", enabled, withKey)}
	}
}

func checkModels(cfg *config.ConfigStore) checkResult {
	models := cfg.Config().Models
	large, hasLarge := models[config.SelectedModelTypeLarge]
	if !hasLarge || large.Model == "" {
		return checkResult{"models", statusFail, "no large model selected; run `atlas models`"}
	}
	small, hasSmall := models[config.SelectedModelTypeSmall]
	if !hasSmall || small.Model == "" {
		return checkResult{"models", statusWarn, fmt.Sprintf("large=%s, no small model selected", large.Model)}
	}
	return checkResult{"models", statusOK, fmt.Sprintf("large=%s, small=%s", large.Model, small.Model)}
}

func checkMCPCommands(cfg *config.ConfigStore) []checkResult {
	var names []string
	for name := range cfg.Config().MCP {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []checkResult
	for _, name := range names {
		m := cfg.Config().MCP[name]
		if m.Disabled || m.Type != config.MCPStdio {
			continue
		}
		out = append(out, commandCheck("mcp "+name, m.Command, cfg))
	}
	return out
}

func checkLSPCommands(cfg *config.ConfigStore) []checkResult {
	var names []string
	for name := range cfg.Config().LSP {
		names = append(names, name)
	}
	sort.Strings(names)

	var out []checkResult
	for _, name := range names {
		l := cfg.Config().LSP[name]
		if l.Disabled {
			continue
		}
		out = append(out, commandCheck("lsp "+name, l.Command, cfg))
	}
	return out
}

// commandCheck reports whether a configured server's command can actually be
// run. A missing binary is a warning rather than a failure: a session starts
// fine without one server, it just loses that server's tools.
func commandCheck(name, command string, cfg *config.ConfigStore) checkResult {
	if command == "" {
		return checkResult{name, statusWarn, "no command configured"}
	}

	resolved := command
	if r, err := cfg.Resolve(command); err == nil && r != "" {
		resolved = r
	}

	path, err := exec.LookPath(resolved)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return checkResult{name, statusWarn, fmt.Sprintf("%s not found on PATH", resolved)}
		}
		return checkResult{name, statusWarn, fmt.Sprintf("%s: %s", resolved, err)}
	}
	return checkResult{name, statusOK, path}
}
