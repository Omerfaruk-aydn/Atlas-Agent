package cmd

import (
	"bytes"
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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/spf13/cobra"
)

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Use several computers in one task (pair, serve, list, revoke)",
	Long: `Nodes let one Atlas task run commands, move files and drive desktops on
other computers. Run "atlas node serve" on the machine to control, then run
the printed "atlas node pair" command on the controlling machine.

Every connection is TLS with a pinned certificate fingerprint and a bearer
token issued at pairing. A node binds to loopback unless --lan is given.`,
}

var nodeServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Serve this computer as a node",
	Example: `  atlas node serve --root ~/src/project
  atlas node serve --root D:\work --listen 0.0.0.0:7443 --lan --allow exec,files,desktop`,
	RunE: runNodeServe,
}

var nodePairCmd = &cobra.Command{
	Use:   "pair <https://host:port>",
	Short: "Pair with a node using its one-time code and fingerprint",
	Args:  cobra.ExactArgs(1),
	RunE:  runNodePair,
}

var nodeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List paired nodes and check that they answer",
	RunE:  runNodeList,
}

var nodeRevokeCmd = &cobra.Command{
	Use:   "revoke <name>",
	Short: "Revoke this computer's access to a node and forget it",
	Args:  cobra.ExactArgs(1),
	RunE:  runNodeRevoke,
}

var nodeExecCmd = &cobra.Command{
	Use:   "exec <name> -- <program> [args...]",
	Short: "Run one command on a paired node (literal argv, no shell)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runNodeSteps(cmd, "exec-"+strconv.FormatInt(time.Now().UnixNano(), 36), []nodes.PlanStep{{ID: "exec", Node: args[0], Action: "exec", Argv: args[1:]}})
	},
}

var nodePlanCmd = &cobra.Command{
	Use:   "plan <plan.json>",
	Short: "Run a cross-device plan (JSON array of steps) and print per-node results",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := readProfileFile(args[0])
		if err != nil {
			return err
		}
		// Windows PowerShell 5.1 writes UTF-8 with a byte order mark.
		data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
		var steps []nodes.PlanStep
		if err := json.Unmarshal(data, &steps); err != nil {
			return fmt.Errorf("parse plan %s: %w", args[0], err)
		}
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			id = "plan-" + strconv.FormatInt(time.Now().UnixNano(), 36)
		}
		return runNodeSteps(cmd, id, steps)
	},
}

func runNodeSteps(cmd *cobra.Command, runID string, steps []nodes.PlanStep) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	res, err := tools.RunNodePlan(ctx, cwd, config.GlobalDevicesDir(), runID, steps)
	if err != nil {
		return err
	}
	if err := printJSON(cmd.OutOrStdout(), res); err != nil {
		return err
	}
	if res.State != taskstate.Completed {
		return fmt.Errorf("plan %s: %s", res.State, res.Summary())
	}
	return nil
}

func init() {
	nodeServeCmd.Flags().String("root", "", "Directory commands and transfers are confined to (required)")
	nodeServeCmd.Flags().String("listen", "127.0.0.1:7443", "Address to listen on")
	nodeServeCmd.Flags().Bool("lan", false, "Allow a non-loopback listen address (reachable from the local network)")
	nodeServeCmd.Flags().String("allow", "exec,files", "Capabilities to grant: exec, files, desktop")
	nodeServeCmd.Flags().String("name", "", "Name shown to controllers (default: hostname)")
	nodeServeCmd.Flags().Int64("max-file-mb", 64, "Largest file a transfer may move, in MiB")
	nodeServeCmd.Flags().String("state-dir", "", "Node state directory (default: Atlas devices dir/node)")
	nodeServeCmd.Flags().String("revoke-device", "", "Revoke a paired controller by device id and exit")
	nodeServeCmd.Flags().Bool("list-devices", false, "List paired controllers and exit")
	nodeServeCmd.Flags().Bool("declare-isolated", false, "Declare that this node runs inside a disposable VM or sandbox, so isolated_desktop may drive it")
	nodeServeCmd.Flags().String("isolated-exchange", "", "Internal: run as the control agent inside an Atlas isolated environment")
	_ = nodeServeCmd.Flags().MarkHidden("isolated-exchange")

	nodePairCmd.Flags().String("code", "", "One-time pairing code printed by the node")
	nodePairCmd.Flags().String("fingerprint", "", "Node certificate fingerprint printed by the node")
	nodePairCmd.Flags().String("name", "", "Name to address this node by (lowercase, e.g. wsl, buildbox)")
	nodePairCmd.Flags().Bool("allow-internet", false, "Allow pairing with a node outside loopback and private networks")
	_ = nodePairCmd.MarkFlagRequired("code")
	_ = nodePairCmd.MarkFlagRequired("fingerprint")
	_ = nodePairCmd.MarkFlagRequired("name")

	nodeListCmd.Flags().Bool("json", false, "Print JSON")

	nodePlanCmd.Flags().String("id", "", "Run id; reusing it makes nodes replay recorded results instead of re-running")
	nodeCmd.AddCommand(nodeServeCmd, nodePairCmd, nodeListCmd, nodeRevokeCmd, nodeExecCmd, nodePlanCmd)
	rootCmd.AddCommand(nodeCmd)
}

func parseCapabilities(s string) ([]nodes.Capability, error) {
	var out []nodes.Capability
	for part := range strings.SplitSeq(s, ",") {
		c := nodes.Capability(strings.TrimSpace(part))
		if c == "" {
			continue
		}
		if !slices.Contains([]nodes.Capability{nodes.CapExec, nodes.CapFiles, nodes.CapDesktop}, c) {
			return nil, fmt.Errorf("unknown capability %q (want exec, files, desktop)", c)
		}
		if !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	return out, nil
}

func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isolatedProvision is written by the isolation manager into the exchange
// folder before the guest starts.
type isolatedProvision struct {
	Token   string `json:"token"`
	Name    string `json:"name"`
	Port    int    `json:"port"`
	LeaseID string `json:"lease_id"`
}

// isolatedAgent is written by the guest so the host can connect.
type isolatedAgent struct {
	LeaseID     string   `json:"lease_id"`
	NodeID      string   `json:"node_id"`
	Fingerprint string   `json:"fingerprint"`
	Port        int      `json:"port"`
	Addresses   []string `json:"addresses"`
	Desktop     bool     `json:"desktop"`
	Reason      string   `json:"reason,omitempty"`
	PID         int      `json:"pid"`
	StartedAt   int64    `json:"started_at"`
}

func runNodeServe(cmd *cobra.Command, _ []string) error {
	root, _ := cmd.Flags().GetString("root")
	listen, _ := cmd.Flags().GetString("listen")
	lan, _ := cmd.Flags().GetBool("lan")
	allow, _ := cmd.Flags().GetString("allow")
	name, _ := cmd.Flags().GetString("name")
	maxMB, _ := cmd.Flags().GetInt64("max-file-mb")
	stateDir, _ := cmd.Flags().GetString("state-dir")
	revoke, _ := cmd.Flags().GetString("revoke-device")
	listDevices, _ := cmd.Flags().GetBool("list-devices")
	exchange, _ := cmd.Flags().GetString("isolated-exchange")
	declareIsolated, _ := cmd.Flags().GetBool("declare-isolated")

	var provision isolatedProvision
	if exchange != "" {
		data, err := os.ReadFile(filepath.Join(exchange, "provision.json"))
		if err != nil {
			return fmt.Errorf("isolated agent needs provision.json in the exchange folder: %w", err)
		}
		if err := json.Unmarshal(data, &provision); err != nil {
			return err
		}
		if root == "" {
			root = filepath.Join(exchange, "files")
		}
		if stateDir == "" {
			stateDir = filepath.Join(os.TempDir(), "atlas-isolated-node")
		}
		listen = net.JoinHostPort("0.0.0.0", strconv.Itoa(orDefault(provision.Port, 7443)))
		lan = true
		allow = "exec,files,desktop"
		name = orDefault(provision.Name, "isolated")
	}
	if stateDir == "" {
		stateDir = filepath.Join(config.GlobalDevicesDir(), "node")
	}
	if listDevices || revoke != "" {
		auth, err := nodes.LoadAuth(stateDir)
		if err != nil {
			return err
		}
		if revoke != "" {
			if err := auth.Revoke(revoke); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked %s.\n", revoke)
			return nil
		}
		for _, d := range auth.Devices() {
			state := "active"
			if d.Revoked {
				state = "revoked"
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\tpaired %s\n", d.ID, d.Name, state, time.UnixMilli(d.CreatedAt).Format(time.RFC3339))
		}
		return nil
	}
	if root == "" {
		return errors.New("--root is required: commands and transfers are confined to it")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	if !isLoopbackListen(listen) && !lan {
		return fmt.Errorf("refusing to listen on %s without --lan; the node would be reachable from the network", listen)
	}
	caps, err := parseCapabilities(allow)
	if err != nil {
		return err
	}
	opts := nodes.ServerOptions{StateDir: stateDir, Root: root, Name: name, Capabilities: caps, Isolated: exchange != "" || declareIsolated, MaxFileBytes: maxMB << 20, Logger: slog.Default()}
	if slices.Contains(caps, nodes.CapDesktop) {
		if backend, berr := computer.Open(); berr == nil {
			opts.Desktop = backend
		} else {
			opts.DesktopReason = berr.Error()
		}
	}
	host, _, _ := net.SplitHostPort(listen)
	opts.Hosts = []string{host}
	srv, err := nodes.NewServer(opts)
	if err != nil {
		return err
	}
	if exchange != "" && provision.Token != "" {
		if _, err := srv.Auth().Authenticate(provision.Token); err != nil {
			if _, err := srv.Auth().Provision("isolation-host", provision.Token); err != nil {
				return err
			}
		}
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(cmd.Context(), "tcp", listen)
	if err != nil {
		return err
	}
	httpServer := &http.Server{Handler: srv.Handler(), TLSConfig: srv.TLSConfig(), ReadHeaderTimeout: 15 * time.Second}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	id := srv.Identity()
	port := ln.Addr().(*net.TCPAddr).Port
	out := cmd.OutOrStdout()
	if exchange != "" {
		agent := isolatedAgent{LeaseID: provision.LeaseID, NodeID: id.NodeID, Fingerprint: id.Fingerprint, Port: port, Addresses: localAddresses(), Desktop: opts.Desktop != nil, Reason: opts.DesktopReason, PID: os.Getpid(), StartedAt: time.Now().UnixMilli()}
		if err := taskstate.WriteJSONAtomic(filepath.Join(exchange, "agent.json"), agent); err != nil {
			return fmt.Errorf("publish isolated agent identity: %w", err)
		}
	} else {
		code, expires := srv.Auth().NewPairingCode()
		addr := listen
		if host == "0.0.0.0" || host == "::" || host == "" {
			if addrs := localAddresses(); len(addrs) > 0 {
				addr = net.JoinHostPort(addrs[0], strconv.Itoa(port))
			}
		} else {
			addr = net.JoinHostPort(host, strconv.Itoa(port))
		}
		fmt.Fprintf(out, "Atlas node %s (%s/%s) serving %s\n", id.NodeID, runtime.GOOS, runtime.GOARCH, root)
		fmt.Fprintf(out, "Capabilities: %v", caps)
		if opts.DesktopReason != "" {
			fmt.Fprintf(out, " (desktop unavailable: %s)", opts.DesktopReason)
		}
		fmt.Fprintf(out, "\nFingerprint: %s\n\n", id.Fingerprint)
		fmt.Fprintf(out, "On the controlling computer, run (code valid until %s, single use):\n\n", expires.Format("15:04"))
		fmt.Fprintf(out, "  atlas node pair https://%s --code %s --fingerprint %s --name <name>\n\n", addr, code, id.Fingerprint)
	}
	err = httpServer.ServeTLS(ln, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func orDefault[T comparable](v, fallback T) T {
	var zero T
	if v == zero {
		return fallback
	}
	return v
}

func localAddresses() []string {
	var out []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				out = append(out, ipn.IP.String())
			}
		}
	}
	return out
}

func runNodePair(cmd *cobra.Command, args []string) error {
	code, _ := cmd.Flags().GetString("code")
	fp, _ := cmd.Flags().GetString("fingerprint")
	name, _ := cmd.Flags().GetString("name")
	allowInternet, _ := cmd.Flags().GetBool("allow-internet")
	if !nodes.ValidName(name) {
		return fmt.Errorf("--name %q must be 1-32 lowercase letters, digits or '-' (and not \"local\")", name)
	}
	network := nodes.ClassifyHost(nodes.HostOf(args[0]))
	if network == nodes.NetworkInternet && !allowInternet {
		return fmt.Errorf("%s is not a loopback or private address; pass --allow-internet to pair over the internet deliberately", args[0])
	}
	client, err := nodes.NewClient(args[0], fp, "")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
	defer cancel()
	host, _ := os.Hostname()
	resp, err := client.Pair(ctx, code, "atlas@"+host)
	if err != nil {
		return fmt.Errorf("pairing failed: %w", err)
	}
	entry := nodes.Entry{Name: name, URL: client.BaseURL, Fingerprint: nodes.NormalizeFingerprint(fp), NodeID: resp.Info.NodeID, DeviceID: resp.DeviceID, OS: resp.Info.OS, Arch: resp.Info.Arch, Root: resp.Info.Root, Capabilities: resp.Info.Capabilities, Network: network, Isolated: resp.Info.Isolated, MaxFileBytes: resp.Info.MaxFileBytes}
	if err := nodes.OpenRegistry(config.GlobalDevicesDir()).Put(entry, resp.Token); err != nil {
		// The token is useless if it cannot be stored; revoke it on the node.
		client.Token = resp.Token
		_ = client.Revoke(context.Background())
		return fmt.Errorf("store node credentials: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Paired %q: %s/%s at %s (%s), capabilities %v, root %s\n", name, entry.OS, entry.Arch, entry.URL, network, entry.Capabilities, entry.Root)
	return nil
}

func runNodeList(cmd *cobra.Command, _ []string) error {
	asJSON, _ := cmd.Flags().GetBool("json")
	reg := nodes.OpenRegistry(config.GlobalDevicesDir())
	entries, err := reg.List()
	if err != nil {
		return err
	}
	type row struct {
		nodes.Entry
		Reachable bool   `json:"reachable"`
		Status    string `json:"status"`
	}
	var rows []row
	for _, e := range entries {
		r := row{Entry: e}
		client, _, err := reg.ClientFor(e.Name)
		if err == nil {
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			_, err = client.Info(ctx)
			cancel()
		}
		if err != nil {
			r.Status = err.Error()
		} else {
			r.Reachable, r.Status = true, "ok"
		}
		rows = append(rows, r)
	}
	if asJSON {
		return printJSON(cmd.OutOrStdout(), rows)
	}
	if len(rows) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No paired nodes. Run \"atlas node serve\" on another computer to start.")
	}
	for _, r := range rows {
		fmt.Fprintf(cmd.OutOrStdout(), "%-12s %-8s %-9s %-28s %v  %s\n", r.Name, r.OS, r.Network, r.URL, r.Capabilities, r.Status)
	}
	return nil
}

func runNodeRevoke(cmd *cobra.Command, args []string) error {
	reg := nodes.OpenRegistry(config.GlobalDevicesDir())
	client, _, err := reg.ClientFor(args[0])
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	defer cancel()
	remoteErr := client.Revoke(ctx)
	if err := reg.Remove(args[0]); err != nil {
		return err
	}
	if remoteErr != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Forgot %s locally, but the node did not confirm revocation (%v). Revoke it on the node with \"atlas node serve --list-devices\" and \"--revoke-device <id>\".\n", args[0], remoteErr)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Revoked and forgot %s.\n", args[0])
	return nil
}
