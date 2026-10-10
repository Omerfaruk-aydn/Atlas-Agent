package mobilerelay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

// NewCommand creates the standalone relay CLI without loading an Atlas
// workspace, provider credentials or the agent runtime.
func NewCommand() *cobra.Command {
	root := &cobra.Command{
		Use: "mobile-relay", Short: "Host your own HTTPS phone relay (no phone app or home port forwarding)",
	}
	keygen := &cobra.Command{
		Use: "keygen", Short: "Create a private connector key file without overwriting",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, _ := cmd.Flags().GetString("out")
			if err := GenerateKey(path); err != nil {
				return err
			}
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Connector key file created. Transfer it privately to the desktop; never put it in a phone URL.")
			return err
		},
	}
	keygen.Flags().String("out", "", "Absolute destination outside the workspace")
	_ = keygen.MarkFlagRequired("out")
	serve := &cobra.Command{
		Use: "serve", Short: "Serve one Atlas installation behind trusted HTTPS",
		Args: cobra.NoArgs, RunE: runMobileRelay,
	}
	serve.Flags().String("listen", "127.0.0.1:8787", "Listen address (loopback by default)")
	serve.Flags().String("public-url", "", "Dedicated HTTPS origin, for example https://atlas.example.com")
	serve.Flags().String("token-file", "", "Absolute path to the private connector key")
	serve.Flags().String("tls-cert", "", "TLS certificate chain, when terminating HTTPS directly")
	serve.Flags().String("tls-key", "", "Corresponding TLS private key")
	serve.Flags().Bool("behind-proxy", false, "Explicitly allow HTTP on a private reverse-proxy network; never publish this listener")
	_ = serve.MarkFlagRequired("public-url")
	_ = serve.MarkFlagRequired("token-file")
	root.AddCommand(keygen, serve)
	return root
}

func runMobileRelay(cmd *cobra.Command, _ []string) error {
	listen, _ := cmd.Flags().GetString("listen")
	publicURL, _ := cmd.Flags().GetString("public-url")
	tokenFile, _ := cmd.Flags().GetString("token-file")
	cert, _ := cmd.Flags().GetString("tls-cert")
	key, _ := cmd.Flags().GetString("tls-key")
	proxy, _ := cmd.Flags().GetBool("behind-proxy")
	if (cert == "") != (key == "") {
		return fmt.Errorf("relay TLS certificate and key must be supplied together")
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if cert == "" && !proxy && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback()) {
		return fmt.Errorf("unencrypted relay listeners must bind to a loopback IP; use TLS or an explicitly private reverse-proxy network")
	}
	token, err := ReadToken(tokenFile)
	if err != nil {
		return err
	}
	relay, err := NewServer(publicURL, token)
	if err != nil {
		return err
	}
	defer relay.Close()
	ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", listen)
	if err != nil {
		return err
	}
	defer listener.Close()
	srv := &http.Server{
		Handler: relay, ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, IdleTimeout: time.Minute,
		MaxHeaderBytes: 64 << 10,
	}
	finished := make(chan error, 1)
	go func() {
		if cert != "" {
			finished <- srv.ServeTLS(listener, cert, key)
		} else {
			finished <- srv.Serve(listener)
		}
	}()
	fmt.Fprintf(cmd.OutOrStdout(), "Phone relay listening on %s for %s. Desktop pairing and phone authentication remain required.\n", listener.Addr(), publicURL)
	select {
	case err := <-finished:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		_ = relay.Close()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		return srv.Shutdown(shutdown)
	}
}
