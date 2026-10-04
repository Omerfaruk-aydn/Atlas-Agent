//go:build !windows

package speech

import "context"

func setup(context.Context, SetupOptions) (SetupResult, error) {
	return SetupResult{State: "unavailable", Message: ErrUnsupported.Error()}, ErrUnsupported
}
