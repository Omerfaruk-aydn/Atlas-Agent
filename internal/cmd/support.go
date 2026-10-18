package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newSupportCommand()) }

func newSupportCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "support",
		Short: "Remote IT support between two computers (relay, headless host, scripted join)",
		Long: `Remote IT support lets one Atlas installation help another after its owner
accepts a verified connection request. Interactive use is /support in the
TUI. These commands cover a headless computer receiving support, a scripted
check from the supporting side, and the relay needed between networks.

On one local network a code is enough. Across networks, the host can use
Tailscale Funnel (--funnel) and share the complete support link, or both
computers can use a relay you operate ("atlas support relay"). A bare code
cannot discover a computer on another network.`,
	}
	root.AddCommand(newSupportRelayCommand(), newSupportHostCommand(), newSupportJoinCommand())
	return root
}

func newSupportRelayCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "relay",
		Short: "Run a support relay that pairs two computers by code slot",
		Example: `  atlas support relay --listen 0.0.0.0:8443 --tls-cert cert.pem --tls-key key.pem --token-file /etc/atlas/support-token
  atlas support relay --listen 127.0.0.1:8790 --behind-proxy --token-file /etc/atlas/support-token`,
		RunE: runSupportRelay,
	}
	c.Flags().String("listen", "127.0.0.1:8790", "Listen address")
	c.Flags().String("token-file", "", "File with the access token both computers present (recommended)")
	c.Flags().String("tls-cert", "", "TLS certificate chain when terminating HTTPS here")
	c.Flags().String("tls-key", "", "TLS private key")
	c.Flags().Bool("behind-proxy", false, "Serve plain HTTP for a TLS reverse proxy on a private network; never publish it directly")
	return c
}

func readTokenFile(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 16 {
		return "", errors.New("the relay token must be at least 16 characters")
	}
	return token, nil
}

func runSupportRelay(cmd *cobra.Command, _ []string) error {
	listen, _ := cmd.Flags().GetString("listen")
	tokenFile, _ := cmd.Flags().GetString("token-file")
	cert, _ := cmd.Flags().GetString("tls-cert")
	key, _ := cmd.Flags().GetString("tls-key")
	proxy, _ := cmd.Flags().GetBool("behind-proxy")
	token, err := readTokenFile(tokenFile)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return fmt.Errorf("listen must be host:port: %w", err)
	}
	loopback := host == "localhost" || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback())
	if (cert == "") != (key == "") {
		return errors.New("--tls-cert and --tls-key go together")
	}
	if cert == "" && !loopback && !proxy {
		return errors.New("plain HTTP is only served on loopback; pass --tls-cert/--tls-key, or --behind-proxy for a private TLS reverse proxy")
	}
	if token == "" {
		fmt.Fprintln(cmd.ErrOrStderr(), "Warning: no --token-file; anyone who can reach this relay can use it to pair (codes are still required and verified end to end).")
	}
	relay := support.NewRelayServer(support.RelayServerOptions{Token: token, Logger: slog.Default()})
	srv := &http.Server{Addr: listen, Handler: relay, ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		relay.Close()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	fmt.Fprintf(cmd.OutOrStdout(), "Support relay listening on %s. It sees code slots and encrypted traffic only, and logs neither.\n", listen)
	if cert != "" {
		err = srv.ListenAndServeTLS(cert, key)
	} else {
		err = srv.ListenAndServe()
	}
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func supportStateDir(cmd *cobra.Command, role string) string {
	if dir, _ := cmd.Flags().GetString("state-dir"); dir != "" {
		return dir
	}
	return filepath.Join(config.GlobalDevicesDir(), "support", role)
}

func supportRelayFlags(cmd *cobra.Command) (*support.RelayConfig, error) {
	url, _ := cmd.Flags().GetString("relay")
	if url == "" {
		return nil, nil
	}
	tokenFile, _ := cmd.Flags().GetString("relay-token-file")
	token, err := readTokenFile(tokenFile)
	if err != nil {
		return nil, err
	}
	return &support.RelayConfig{URL: url, Token: token}, nil
}

func addSupportCommonFlags(c *cobra.Command) {
	c.Flags().String("name", "", "Name shown to the other computer (default: computer name)")
	c.Flags().String("relay", "", "Support relay URL for computers on different networks")
	c.Flags().String("relay-token-file", "", "Relay access token file")
	c.Flags().String("state-dir", "", "Identity, journal and history directory (default: Atlas devices dir)")
	c.Flags().Bool("no-discovery", false, "Do not use UDP discovery on the local network")
}

func newSupportHostCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "host",
		Short: "Offer support access to this computer from a terminal and accept or reject the request",
		Example: `  atlas support host --allow diagnose --folder Work=D:\support
  atlas support host --allow system_info,file_read,file_send,command --folder Desktop=C:\Users\me\Desktop --internet --relay https://support.example.com --relay-token-file token.txt`,
		RunE: runSupportHost,
	}
	addSupportCommonFlags(c)
	c.Flags().String("allow", "diagnose", "Permissions: diagnose, assist, full, or a comma-separated list ("+strings.Join(support.Strings(support.AllCapabilities), ",")+")")
	c.Flags().StringArray("folder", nil, "Allowed folder as Label=absolute-path (repeatable)")
	c.Flags().Duration("duration", 2*time.Hour, "Session length (at most 8h)")
	c.Flags().Bool("lan", true, "Accept a connection on the local network")
	c.Flags().Bool("internet", false, "Accept a connection through --relay")
	c.Flags().Bool("funnel", false, "Use installed Tailscale Funnel; share the complete support link")
	c.Flags().String("listen", "", "Local network listen address (default: any interface, random port)")
	return c
}

func runSupportHost(cmd *cobra.Command, _ []string) error {
	allow, _ := cmd.Flags().GetString("allow")
	caps, err := support.ParseCapabilities(allow)
	if err != nil {
		return err
	}
	var roots []nodes.Root
	folders, _ := cmd.Flags().GetStringArray("folder")
	for _, f := range folders {
		label, path, ok := strings.Cut(f, "=")
		if !ok {
			return fmt.Errorf("--folder %q must be Label=absolute-path", f)
		}
		roots = append(roots, nodes.Root{Label: strings.TrimSpace(label), Path: strings.TrimSpace(path)})
	}
	relay, err := supportRelayFlags(cmd)
	if err != nil {
		return err
	}
	name, _ := cmd.Flags().GetString("name")
	listen, _ := cmd.Flags().GetString("listen")
	lan, _ := cmd.Flags().GetBool("lan")
	internet, _ := cmd.Flags().GetBool("internet")
	funnel, _ := cmd.Flags().GetBool("funnel")
	if funnel && (internet || cmd.Flags().Changed("lan") && lan) {
		return errors.New("--funnel cannot be combined with --internet or explicit --lan=true")
	}
	duration, _ := cmd.Flags().GetDuration("duration")
	noDiscovery, _ := cmd.Flags().GetBool("no-discovery")
	discovery := 0
	if noDiscovery {
		discovery = -1
	}
	desk, reason := computer.OpenOrNil(), ""
	if desk == nil {
		reason = "desktop control is not available in this session"
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	out := cmd.OutOrStdout()
	changed := make(chan struct{}, 1)
	h, err := support.StartHost(ctx, support.HostOptions{
		StateDir: supportStateDir(cmd, "host"), Name: name, LANListen: listen, DiscoveryPort: discovery, Relay: relay,
		Desktop: desk, DesktopReason: reason, Overlay: &support.ActivityOverlay{}, Logger: slog.Default(), Executor: tools.NewSupportExecutor(supportStateDir(cmd, "host"), desk, config.ToolBrowser{}),
		OnChange: func() {
			select {
			case changed <- struct{}{}:
			default:
			}
		},
	})
	if err != nil {
		return err
	}
	defer h.Close()
	opts := support.OfferOptions{Capabilities: caps, Roots: roots, SessionDuration: duration, LAN: lan, Internet: internet}
	var info support.OfferInfo
	if funnel {
		info, err = h.OfferFunnel(ctx, opts)
	} else {
		info, err = h.Offer(opts)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Support code: %s (single use, valid until %s)\n", info.Code, info.ExpiresAt.Format("15:04:05"))
	fmt.Fprintf(out, "This computer: %s, device %s, fingerprint %s\n", h.Status().Name, h.NodeID(), support.ShortFingerprint(h.Fingerprint()))
	fmt.Fprintf(out, "Permissions: %s\n", strings.Join(support.Strings(caps), ", "))
	for _, a := range info.LANAddresses {
		fmt.Fprintf(out, "Local network: atlas support join %s@%s\n", info.Code, a)
	}
	if info.RelayURL != "" {
		fmt.Fprintf(out, "Internet: through %s\n", info.RelayURL)
	}
	if funnel {
		fmt.Fprintf(out, "Internet support link (share privately): %s\n", info.Link)
	}
	fmt.Fprintln(out, "Waiting for a connection request. Ctrl+C ends support.")

	stdin := bufio.NewReader(cmd.InOrStdin())
	decided := map[string]bool{}
	var last support.HostStatus
	for {
		select {
		case <-ctx.Done():
			h.Disconnect("the owner ended support from the terminal")
			fmt.Fprintln(out, "Support ended.")
			return nil
		case <-changed:
		case <-time.After(time.Second):
		}
		st := h.Status()
		if p := st.Pending; p != nil && !decided[p.ID] {
			decided[p.ID] = true
			fmt.Fprintf(out, "\nConnection request from %q (name sent by the device, unverified)\n", p.Peer.Name)
			fmt.Fprintf(out, "Verified device %s, %s/%s, via %s from %s\n", support.ShortFingerprint(p.Peer.Fingerprint), p.Peer.OS, p.Peer.Arch, p.Network, p.RemoteAddr)
			fmt.Fprintf(out, "Verification code %s must match the supporter's screen. Accept? [y/N] ", p.Verification)
			answer, readErr := readSupportAnswer(ctx, stdin)
			if readErr != nil {
				if ctx.Err() != nil {
					return nil
				}
				return fmt.Errorf("read support decision: %w", readErr)
			}
			accept := strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes")
			if err := h.Decide(p.ID, accept, nil); err != nil {
				fmt.Fprintln(out, err)
			}
			continue
		}
		if st.State != last.State || (st.Session != nil && last.Session != nil && st.Session.Active != last.Session.Active) {
			fmt.Fprintf(out, "[%s] %s", time.Now().Format("15:04:05"), st.State)
			if st.Reason != "" {
				fmt.Fprintf(out, " (%s)", st.Reason)
			}
			if st.Session != nil && st.Session.Active != nil {
				fmt.Fprintf(out, " · now: %s", st.Session.Active.Summary)
			}
			fmt.Fprintln(out)
		}
		last = st
		if st.State.Terminal() && st.Session == nil {
			return nil
		}
	}
}

// readSupportAnswer leaves status output in the caller's single render loop
// and allows Ctrl+C to end support while the owner is answering.
func readSupportAnswer(ctx context.Context, reader *bufio.Reader) (string, error) {
	type answer struct {
		text string
		err  error
	}
	result := make(chan answer, 1)
	go func() {
		text, err := reader.ReadString('\n')
		result <- answer{text: text, err: err}
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case value := <-result:
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return value.text, value.err
	}
}

func newSupportJoinCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "join <code | CODE@host:port | atlas-support:// link>",
		Short: "Connect to a computer offering support and run scripted checks",
		Long: `join connects, shows the verification code to compare with the other screen,
waits for the owner to accept and then runs the requested steps in order,
printing one JSON result per step. Every result names the remote device. A
step whose connection drops reports an unknown outcome and is not resent.`,
		Example: `  atlas support join ABCD-EFGH-JKMN@192.168.1.20:51515 --sysinfo
  atlas support join ABCD-EFGH-JKMN --push report.txt:Work/report.txt --pull Work/log.txt:log.txt --exec "notepad.exe"`,
		Args: cobra.ExactArgs(1),
		RunE: runSupportJoin,
	}
	addSupportCommonFlags(c)
	c.Flags().Bool("sysinfo", false, "Read system information")
	c.Flags().StringArray("push", nil, "Send local:remote (remote starts with a folder label)")
	c.Flags().StringArray("pull", nil, "Fetch remote:local into the current directory")
	c.Flags().StringArray("exec", nil, "Run a command line (split on spaces, no shell)")
	c.Flags().StringArray("launch", nil, "Open a program on the remote desktop (split on spaces)")
	c.Flags().Bool("observe", false, "Read the remote screen geometry and windows")
	c.Flags().Bool("wait", false, "Stay connected until Ctrl+C after the steps")
	return c
}

func runSupportJoin(cmd *cobra.Command, args []string) error {
	relay, err := supportRelayFlags(cmd)
	if err != nil {
		return err
	}
	name, _ := cmd.Flags().GetString("name")
	noDiscovery, _ := cmd.Flags().GetBool("no-discovery")
	discovery := 0
	if noDiscovery {
		discovery = -1
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	out := cmd.OutOrStdout()
	c, err := support.NewController(ctx, support.ControllerOptions{StateDir: supportStateDir(cmd, "controller"), Name: name, Relay: relay, DiscoveryPort: discovery, Logger: slog.Default()})
	if err != nil {
		return err
	}
	defer c.Close()
	fmt.Fprintf(out, "This device: %s\n", support.ShortFingerprint(c.Fingerprint()))
	remote, err := c.Join(ctx, args[0], func(p support.JoinPrompt) {
		fmt.Fprintf(out, "Found %s (%s/%s) via %s. Verification code: %s — compare it with the other screen. Waiting for acceptance…\n", p.Host.Name, p.Host.OS, p.Host.Arch, p.Network, p.Verification)
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Connected to %s, device %s, %s/%s, session %s, connection %d, permissions %s, folders %s\n",
		remote.RemoteName, remote.RemoteDeviceID, remote.OS, remote.Arch, remote.SessionID, remote.Generation, strings.Join(support.Strings(remote.Capabilities), ","), strings.Join(remote.Roots, ","))
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	step := 0
	emit := func(action string, result any, err error) {
		step++
		r, _, _ := c.Target()
		rec := map[string]any{"step": step, "action": action, "remote_device_id": r.RemoteDeviceID, "session_id": r.SessionID, "generation": r.Generation}
		if op, ok := result.(nodes.Operation); ok && err == nil {
			err = supportCommandOperationError(op)
			if err != nil {
				rec["result"] = op
			}
		}
		if err != nil {
			rec["ok"], rec["error"] = false, support.Classify(err, r.RemoteDeviceID, "")
		} else {
			rec["ok"], rec["result"] = true, result
		}
		_ = enc.Encode(rec)
	}
	client := func() (*nodes.Client, error) {
		_, cl, err := c.Target()
		return cl, err
	}
	opID := func(kind string, i int) string {
		return nodes.OperationID("cli", remote.SessionID, kind, fmt.Sprint(i), fmt.Sprint(time.Now().UnixNano()))
	}
	if ok, _ := cmd.Flags().GetBool("sysinfo"); ok {
		cl, err := client()
		if err == nil {
			var info nodes.SystemInfo
			info, err = cl.SystemInfo(ctx)
			emit("system_info", info, err)
		} else {
			emit("system_info", nil, err)
		}
	}
	pushes, _ := cmd.Flags().GetStringArray("push")
	for i, p := range pushes {
		local, rel, ok := strings.Cut(p, ":")
		if ok && len(local) == 1 {
			// A Windows drive letter: split at the next colon instead.
			if j := strings.Index(rel, ":"); j >= 0 {
				local, rel = local+":"+rel[:j], rel[j+1:]
			}
		}
		cl, err := client()
		if err != nil {
			emit("push", nil, err)
			continue
		}
		res, err := cl.Upload(ctx, opID("push", i), local, rel, transfer.ConflictError, 0, nil)
		emit("push", res, err)
	}
	pulls, _ := cmd.Flags().GetStringArray("pull")
	for _, p := range pulls {
		rel, local, ok := strings.Cut(p, ":")
		if !ok {
			emit("pull", nil, fmt.Errorf("--pull %q must be remote:local", p))
			continue
		}
		cl, err := client()
		if err != nil {
			emit("pull", nil, err)
			continue
		}
		wd, _ := os.Getwd()
		res, err := cl.Pull(ctx, rel, wd, local, false, 0)
		emit("pull", res, err)
	}
	execs, _ := cmd.Flags().GetStringArray("exec")
	for i, line := range execs {
		cl, err := client()
		if err != nil {
			emit("exec", nil, err)
			continue
		}
		op, err := cl.Exec(ctx, nodes.ExecRequest{OperationID: opID("exec", i), Argv: strings.Fields(line)})
		emit("exec", op, err)
	}
	launches, _ := cmd.Flags().GetStringArray("launch")
	for i, line := range launches {
		cl, err := client()
		if err != nil {
			emit("launch", nil, err)
			continue
		}
		op, err := cl.Launch(ctx, opID("launch", i), strings.Fields(line))
		emit("launch", op, err)
	}
	if ok, _ := cmd.Flags().GetBool("observe"); ok {
		cl, err := client()
		if err == nil {
			var obs nodes.DesktopObservation
			obs, err = cl.Observe(ctx, false)
			emit("observe", obs, err)
		} else {
			emit("observe", nil, err)
		}
	}
	if wait, _ := cmd.Flags().GetBool("wait"); wait {
		fmt.Fprintln(out, "Connected. Ctrl+C disconnects.")
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
				if st := c.Status(); st.State.Terminal() {
					fmt.Fprintf(out, "Session ended: %s (%s)\n", st.State, st.Reason)
					return nil
				}
			}
		}
	}
	return nil
}

// supportCommandOperationError interprets the journal after HTTP success.
func supportCommandOperationError(op nodes.Operation) error {
	if op.State == "completed" && op.Effect != "unknown" {
		return nil
	}
	code := support.Code("operation_failed")
	if !op.State.Terminal() || op.Effect == "unknown" {
		code = support.CodeOutcomeUnknown
	}
	return &support.Error{Code: code, Message: "Remote operation " + string(op.State) + ": " + op.Error, OperationID: op.ID, InputSent: op.Effect != "none", Effect: support.Effect(op.Effect)}
}
