package connections

import (
	"context"
	"net"
)

// listenLoopback opens a port on this machine for an OAuth callback. The
// call carries the request context so a cancelled sign-in does not leave
// the listener behind.
func listenLoopback(ctx context.Context) (net.Listener, error) {
	var cfg net.ListenConfig
	return cfg.Listen(ctx, "tcp", "127.0.0.1:0")
}
