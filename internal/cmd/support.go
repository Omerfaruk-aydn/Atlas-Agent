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

On one local network a code is enough. Computers on different networks need a
relay you operate ("atlas support relay"); a code alone cannot find a computer
on another network, and Atlas never contacts a relay that is not configured.`,
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
	c.Flags().String("listen", "", "Local network listen address (default: any interface, random port)")
	return c
}

