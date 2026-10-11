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

