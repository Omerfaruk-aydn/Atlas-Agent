//go:build windows

package remotecontrol

import (
	"io"
	"os"
)

// openAgent connects to the Windows OpenSSH agent's named pipe.
func openAgent() (io.ReadWriteCloser, error) {
	return os.OpenFile(`\\.\pipe\openssh-ssh-agent`, os.O_RDWR, 0)
}
