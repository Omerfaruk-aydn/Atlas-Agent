package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/stretchr/testify/require"
)

// scriptedScreen serves a sequence of screens; the last repeats.
type scriptedScreen struct {
	t       *testing.T
	screens []color.Gray
	texts   []bool
	calls   int
}

func (s *scriptedScreen) frame(i int) []byte {
	img := image.NewGray(image.Rect(0, 0, 192, 108))
	c := s.screens[min(i, len(s.screens)-1)]
	for y := range 108 {
		for x := range 192 {
			img.SetGray(x, y, c)
		}
	}
	// A blinking caret must not count as a change.
	if i%2 == 1 {
		img.SetGray(5, 5, color.Gray{Y: 255 - c.Y})
	}
	var buf bytes.Buffer
	require.NoError(s.t, png.Encode(&buf, img))
	return buf.Bytes()
}

func (s *scriptedScreen) Observe(context.Context, bool) (nodes.DesktopObservation, error) {
	i := s.calls
	s.calls++
	return nodes.DesktopObservation{ObservationID: "obs", PNG: s.frame(i)}, nil
}

func (s *scriptedScreen) Automation(context.Context, nodes.AutomationRequest) (json.RawMessage, error) {
	i := s.calls
	s.calls++
	if s.texts[min(i, len(s.texts)-1)] {
		return json.RawMessage(`{"elements":[{"name":"Generating"}]}`), nil
	}
	return json.RawMessage(`{"elements":[]}`), nil
}

func TestRemoteWaitStableReturnsWhenGenerationSettles(t *testing.T) {
	t.Parallel()
	g := func(v uint8) color.Gray { return color.Gray{Y: v} }
	s := &scriptedScreen{t: t, screens: []color.Gray{g(10), g(60), g(120), g(200), g(200), g(200), g(200)}}
	res, obs, err := remoteWait(t.Context(), s, "stable", "", "", time.Minute, time.Millisecond)
	require.NoError(t, err)
	require.True(t, res.Met)
	require.Equal(t, 6, res.Samples, "returns at the first settled sample, not at the timeout")
	require.Equal(t, "obs", obs.ObservationID)
}

func TestRemoteWaitChangeIgnoresTheCaret(t *testing.T) {
	t.Parallel()
	g := func(v uint8) color.Gray { return color.Gray{Y: v} }
	s := &scriptedScreen{t: t, screens: []color.Gray{g(40), g(40), g(40), g(40), g(220)}}
	res, _, err := remoteWait(t.Context(), s, "change", "", "", time.Minute, time.Millisecond)
	require.NoError(t, err)
	require.True(t, res.Met)
	require.Equal(t, 5, res.Samples)
}

func TestRemoteWaitTextGoneAndTimeout(t *testing.T) {
	t.Parallel()
	s := &scriptedScreen{t: t, texts: []bool{true, true, false}}
	res, _, err := remoteWait(t.Context(), s, "text_gone", "Generating", "", time.Minute, time.Millisecond)
	require.NoError(t, err)
	require.True(t, res.Met)
	require.Equal(t, 3, res.Samples)

	never := &scriptedScreen{t: t, texts: []bool{true}}
	res, _, err = remoteWait(t.Context(), never, "text_gone", "Generating", "", 30*time.Millisecond, 5*time.Millisecond)
	require.NoError(t, err)
	require.False(t, res.Met, "timeout reports what was seen")

	_, _, err = remoteWait(t.Context(), never, "text", "", "", time.Second, time.Millisecond)
	require.ErrorContains(t, err, "needs name")
	_, _, err = remoteWait(t.Context(), never, "sleep", "", "", time.Second, time.Millisecond)
	require.Error(t, err)
}

func TestRemoteWaitIsCancellable(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	s := &scriptedScreen{t: t, texts: []bool{true}}
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, _, err := remoteWait(ctx, s, "text_gone", "Generating", "", time.Minute, 5*time.Millisecond)
	require.ErrorIs(t, err, context.Canceled)
}
