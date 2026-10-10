//go:build !windows

package remotecontrol

import (
	"errors"
	"io"
	"net"
	"os"
)

// openAgent connects to the agent named by SSH_AUTH_SOCK.
func openAgent() (io.ReadWriteCloser, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, errors.New("SSH_AUTH_SOCK is not set")
	}
	return net.Dial("unix", sock)
}
