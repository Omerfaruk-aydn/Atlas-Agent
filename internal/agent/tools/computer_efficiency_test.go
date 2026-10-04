package tools

import (
	"context"
	"encoding/json"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/stretchr/testify/require"
)

type efficientDesktopBackend struct {
	fakeComputerBackend
	call func(context.Context, computer.AutomationRequest) (json.RawMessage, error)
}

func (b *efficientDesktopBackend) Origin() computer.Point { return computer.Point{X: -100} }
func (b *efficientDesktopBackend) Automation(ctx context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
	return b.call(ctx, p)
}

func TestComputerAssertionHonorsExplicitShortWait(t *testing.T) {
	t.Parallel()
	b := &efficientDesktopBackend{call: func(ctx context.Context, _ computer.AutomationRequest) (json.RawMessage, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	s := &computerToolState{backend: b}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	started := time.Now()
	response, err := s.runAutomation(ctx, "assert", ComputerParams{Automation: computer.AutomationRequest{WaitMS: 20}})
	require.NoError(t, err)
	require.Contains(t, response.Content, "condition_timeout")
	require.Less(t, time.Since(started), 500*time.Millisecond)
}

func TestComputerRegionOCRReportsCoordinateOriginAndCleansFile(t *testing.T) {
	t.Parallel()
	var path string
	b := &efficientDesktopBackend{fakeComputerBackend: fakeComputerBackend{screenshot: testPNG(t, 20, 20)}}
	b.call = func(_ context.Context, p computer.AutomationRequest) (json.RawMessage, error) {
		require.Equal(t, "God's Plan", p.Name)
		path = p.ImagePath
		f, err := os.Open(path)
		require.NoError(t, err)
		defer f.Close()
		img, err := png.Decode(f)
		require.NoError(t, err)
		require.Equal(t, 4, img.Bounds().Dx())
		require.Equal(t, 5, img.Bounds().Dy())
		return json.RawMessage(`{"text":"God's Plan","lines":[]}`), nil
	}
	s := &computerToolState{backend: b}
	response, err := s.runAutomation(t.Context(), "ocr", ComputerParams{X: 3, Y: 7, Width: 4, Height: 5, Automation: computer.AutomationRequest{Name: "God's Plan"}})
	require.NoError(t, err)
	require.False(t, response.IsError)
	var result struct {
		ImageOrigin computer.Point `json:"image_origin"`
		ElapsedMS   int64          `json:"elapsed_ms"`
	}
	require.NoError(t, json.Unmarshal([]byte(response.Content), &result))
	require.Equal(t, computer.Point{X: 3, Y: 7}, result.ImageOrigin)
	_, err = os.Stat(path)
	require.True(t, os.IsNotExist(err))
}

func TestComputerRejectsUnboundedObservation(t *testing.T) {
	t.Parallel()
	b := &efficientDesktopBackend{call: func(context.Context, computer.AutomationRequest) (json.RawMessage, error) {
		t.Fatal("Invalid request must not reach the provider")
		return nil, nil
	}}
	s := &computerToolState{backend: b}
	response, err := s.runAutomation(t.Context(), "inspect", ComputerParams{Automation: computer.AutomationRequest{MaxElements: 501}})
	require.NoError(t, err)
	require.True(t, response.IsError)
}
