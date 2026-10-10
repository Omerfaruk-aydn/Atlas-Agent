//go:build !windows

package isolation

import (
	"context"
	"errors"
	"runtime"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/procown"
)

type otherPlatform struct{}

// DefaultPlatform returns the native platform.
func DefaultPlatform() Platform { return otherPlatform{} }

func (otherPlatform) Probe(context.Context) Facts { return Facts{GOOS: runtime.GOOS} }

func (otherPlatform) Launch(context.Context, string) ([]procown.Identity, error) {
	return nil, errors.New("Windows Sandbox is only available on Windows")
}

func (otherPlatform) Focus([]procown.Identity) error {
	return errors.New("isolated desktops are only available on Windows")
}

func (otherPlatform) HostSnapshot() (HostSnapshot, error) {
	return HostSnapshot{}, errors.New("host input snapshot is only implemented on Windows")
}
