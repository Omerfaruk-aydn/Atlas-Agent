//go:build windows

package computer

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCaptureBitmapRestoresBeforeReadAndOnFailure(t *testing.T) {
	t.Parallel()
	for _, fail := range []bool{false, true} {
		var calls []string
		boom := errors.New("blit failed")
		err := withCaptureBitmap(func() (uintptr, error) {
			calls = append(calls, "select")
			return 7, nil
		}, func(old uintptr) error {
			require.Equal(t, uintptr(7), old)
			calls = append(calls, "restore")
			return nil
		}, func() error {
			calls = append(calls, "blit")
			if fail {
				return boom
			}
			return nil
		})
		require.Equal(t, []string{"select", "blit", "restore"}, calls)
		if fail {
			require.ErrorIs(t, err, boom)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestWindowsDesktopScreenshot(t *testing.T) {
	if os.Getenv("ATLAS_DESKTOP_FIXTURE") != "1" {
		t.Skip("Set ATLAS_DESKTOP_FIXTURE=1 for interactive desktop capture")
	}
	b := &windowsBackend{}
	for range 3 {
		data, err := b.Screenshot()
		require.NoError(t, err)
		img, err := png.Decode(bytes.NewReader(data))
		require.NoError(t, err)
		size, err := b.ScreenSize()
		require.NoError(t, err)
		require.Equal(t, size.Width, img.Bounds().Dx())
		require.Equal(t, size.Height, img.Bounds().Dy())
	}
}

func TestCaptureBitmapStopsOnSelectionAndRestorationFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("selection failed")
	err := withCaptureBitmap(func() (uintptr, error) { return 0, boom },
		func(uintptr) error { t.Fatal("Must not restore an unselected bitmap"); return nil },
		func() error { t.Fatal("Must not blit an unselected bitmap"); return nil })
	require.ErrorIs(t, err, boom)
	err = withCaptureBitmap(func() (uintptr, error) { return 7, nil },
		func(uintptr) error { return boom }, func() error { return nil })
	require.ErrorIs(t, err, boom)
}

func TestWindowsFocusRejectsInvalidTargetsWithoutUIA(t *testing.T) {
	t.Parallel()
	b := &windowsBackend{}
	for _, id := range []string{"", "abc", "0", "-1", "18446744073709551616"} {
		_, err := b.Automation(t.Context(), AutomationRequest{Action: "focus", WindowID: id})
		require.ErrorContains(t, err, "invalid_target")
	}
	_, err := b.Automation(t.Context(), AutomationRequest{Action: "focus", WindowID: "1"})
	require.ErrorContains(t, err, "target_missing")
}
