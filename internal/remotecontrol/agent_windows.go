//go:build windows

package remotecontrol

import (
	"context"
	"io"
	"os"
)

// openAgent connects to the Windows OpenSSH agent's named pipe.
func openAgent(ctx context.Context) (io.ReadWriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.OpenFile(`\\.\pipe\openssh-ssh-agent`, os.O_RDWR, 0)
}
