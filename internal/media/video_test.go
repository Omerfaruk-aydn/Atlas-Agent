package media

import (
	"bytes"
	"context"
	"image/jpeg"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func videoFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed; real video integration requires it")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe is not installed; real video integration requires it")
	}
	path := filepath.Join(t.TempDir(), "two scenes ö.mp4")
	cmd := exec.CommandContext(t.Context(), "ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "color=c=red:s=320x180:r=10:d=2", "-f", "lavfi", "-i", "color=c=blue:s=320x180:r=10:d=2", "-f", "lavfi", "-i", "sine=frequency=440:duration=4", "-filter_complex", "[0:v][1:v]concat=n=2:v=1:a=0[v]", "-map", "[v]", "-map", "2:a", "-c:v", "mpeg4", "-q:v", "3", "-c:a", "aac", "-shortest", path)
	hideWindow(cmd)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, string(output))
	return path
}

func TestVideoSamplingObservesSourceScenesAndExtractsAudio(t *testing.T) {
	t.Parallel()
	path := videoFixture(t)
	result, err := Sample(t.Context(), path, t.TempDir(), Options{Frames: 2, Audio: true})
	require.NoError(t, err)
	require.InDelta(t, 4, result.Probe.Duration, 0.1)
	require.Equal(t, 320, result.Probe.Width)
	require.True(t, result.Probe.HasAudio)
	require.Equal(t, []float64{1, 3}, result.Timestamps)
	sheet, err := jpeg.Decode(bytes.NewReader(result.Image))
	require.NoError(t, err)
	require.Equal(t, 1280, sheet.Bounds().Dx())
	r, g, b, _ := sheet.At(100, 100).RGBA()
	require.Greater(t, r, g*2)
	require.Greater(t, r, b*2, "first sample must observe the red scene")
	r, g, b, _ = sheet.At(740, 100).RGBA()
	require.Greater(t, b, r*2)
	require.Greater(t, b, g*2, "second sample must observe the blue scene")
	audio, err := os.ReadFile(result.AudioPath)
	require.NoError(t, err)
	require.Equal(t, "RIFF", string(audio[:4]))
	require.Greater(t, len(audio), 100000)
	only, err := Sample(t.Context(), path, t.TempDir(), Options{Start: 1, End: 2, Audio: true, AudioOnly: true})
	require.NoError(t, err)
	require.Empty(t, only.Image)
	require.Empty(t, only.Timestamps)
	require.FileExists(t, only.AudioPath)
}

func TestVideoRangesCancellationAndMalformedInputs(t *testing.T) {
	t.Parallel()
	path := videoFixture(t)
	for _, o := range []Options{{Start: -1}, {Start: 5}, {End: 20}, {Start: 2, End: 1}, {Frames: 13}, {Start: math.NaN()}} {
		_, err := Sample(t.Context(), path, t.TempDir(), o)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := Inspect(ctx, path)
	require.ErrorIs(t, err, context.Canceled)
	playlist := filepath.Join(t.TempDir(), "playlist.mp4")
	require.NoError(t, os.WriteFile(playlist, []byte("ffconcat version 1.0\nfile 'https://example.invalid/private.mp4'\n"), 0o600))
	_, err = Inspect(t.Context(), playlist)
	require.Error(t, err, "a disguised playlist must not open network protocols")
}

func TestReadBoundedDoesNotReturnPartialOversizedFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "growing.bin")
	require.NoError(t, os.WriteFile(path, []byte("123456"), 0o600))
	data, err := ReadBounded(path, 5)
	require.Error(t, err)
	require.Nil(t, data)
}
