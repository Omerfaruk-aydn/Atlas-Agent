package support

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/computer"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/transfer"
	"github.com/stretchr/testify/require"
)

// The test binary doubles as a helper program for remote commands, so
// tests need no platform-specific executables.
func TestMain(m *testing.M) {
	for _, arg := range os.Args[1:] {
		mode, ok := strings.CutPrefix(arg, "-support-helper=")
		if !ok {
			continue
		}
		switch mode {
		case "echo":
			_, _ = os.Stdout.WriteString("hello from the supported computer\n")
		case "mark":
			f, err := os.OpenFile("marks.txt", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
			if err == nil {
				_, _ = f.WriteString("ran\n")
				_ = f.Close()
			}
			time.Sleep(1500 * time.Millisecond)
		case "sleep":
			time.Sleep(time.Minute)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helper(t *testing.T, mode string) []string {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	return []string{exe, "-support-helper=" + mode}
}

type fakeDesktop struct {
	mu     sync.Mutex
	size   computer.Size
	clicks atomic.Int32
}

func (f *fakeDesktop) ScreenSize() (computer.Size, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.size, nil
}

func (f *fakeDesktop) Screenshot() ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(3, 3, color.White)
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes(), nil
}
func (f *fakeDesktop) CursorPosition() (computer.Point, error) { return computer.Point{}, nil }
func (f *fakeDesktop) MoveTo(int, int) error                   { return nil }
func (f *fakeDesktop) Click(int, int, computer.MouseButton) error {
	f.clicks.Add(1)
	return nil
}
