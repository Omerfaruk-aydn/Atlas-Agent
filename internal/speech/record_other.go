//go:build !windows

package speech

import "context"

func check(context.Context, DictationOptions) (Status, error) { return Status{}, ErrUnsupported }

func transcribe(context.Context, string, DictationOptions) (string, error) { return "", ErrUnsupported }

func record(_ context.Context, _ string, _ DictationOptions, _ <-chan struct{}, ready chan<- error) error {
	ready <- ErrUnsupported
	return ErrUnsupported
}
