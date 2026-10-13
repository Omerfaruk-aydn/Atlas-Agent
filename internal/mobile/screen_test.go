package mobile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// syntheticScreen is a stand-in desktop: each capture paints a new frame
// so tests can tell frames apart. It is not a measurement of real capture.
type syntheticScreen struct {
	calls  atomic.Int64
	mu     sync.Mutex
	width  int
	height int
	err    error
	black  bool
	delay  time.Duration
}

func (s *syntheticScreen) CaptureScreen(maxWidth int) (*image.RGBA, int, int, time.Time, error) {
	n := s.calls.Add(1)
	s.mu.Lock()
	w, h, err, black, delay := s.width, s.height, s.err, s.black, s.delay
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if err != nil {
		return nil, 0, 0, time.Now(), err
	}
	ow, oh := w, h
	if ow > maxWidth {
		oh = h * maxWidth / w
		ow = maxWidth
	}
	img := image.NewRGBA(image.Rect(0, 0, ow, oh))
	c := color.RGBA{R: uint8(40 + n%200), G: 90, B: 160, A: 255}
	if black {
		c = color.RGBA{A: 255}
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img, w, h, time.Now(), nil
}

func (s *syntheticScreen) set(f func(*syntheticScreen)) { s.mu.Lock(); f(s); s.mu.Unlock() }

func newScreenGateway(t *testing.T, src ScreenSource) (*Gateway, *fakeBackend) {
	t.Helper()
	b := &fakeBackend{pending: map[string]bool{}, questions: map[string]bool{}, revision: "r1"}
	g, err := Start(t.Context(), Options{StateDir: t.TempDir(), Listen: "127.0.0.1:0", Backend: b, Language: "tr", Screen: src})
	require.NoError(t, err)
	t.Cleanup(func() { _ = g.Close() })
	return g, b
}

func enablePreviews(t *testing.T, g *Gateway, on bool) string {
	t.Helper()
	devices := g.Auth.Devices()
	require.NotEmpty(t, devices)
	id := devices[len(devices)-1].ID
	require.NoError(t, g.Auth.SetPreviews(id, on))
	return id
}

type streamPacket struct {
	kind     byte
	seq      uint64
	captured time.Time
	w, h     int
	payload  []byte
}

type stream struct {
	resp *http.Response
	r    *bufio.Reader
}

func (p *phone) openStream(ctx context.Context, hdr map[string]string) (*http.Response, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, p.base+"/api/screen/stream", nil)
	req.Header.Set("X-CSRF-Token", p.csrf)
	for k, v := range hdr {
		if v == "" {
			req.Header.Del(k)
		} else {
			req.Header.Set(k, v)
		}
	}
	return p.client.Do(req)
}

func (s *stream) next(t *testing.T) streamPacket {
	t.Helper()
	head := make([]byte, screenHeaderSize)
	_, err := io.ReadFull(s.r, head)
	require.NoError(t, err)
	require.Equal(t, screenMagic, string(head[:4]))
	n := binary.BigEndian.Uint32(head[28:])
	require.LessOrEqual(t, n, uint32(screenMaxFrame*2))
	payload := make([]byte, n)
	_, err = io.ReadFull(s.r, payload)
	require.NoError(t, err)
	return streamPacket{kind: head[4], seq: binary.BigEndian.Uint64(head[8:]), captured: time.UnixMicro(int64(binary.BigEndian.Uint64(head[16:]))), w: int(binary.BigEndian.Uint16(head[24:])), h: int(binary.BigEndian.Uint16(head[26:])), payload: payload}
}

func (s *stream) nextFrame(t *testing.T) streamPacket {
	t.Helper()
	for range 200 {
		pk := s.next(t)
		if pk.kind == screenKindFrame {
			return pk
		}
	}
	t.Fatal("no frame")
	return streamPacket{}
}

func (s *stream) nextStatus(t *testing.T, state string) screenStats {
	t.Helper()
	for range 400 {
		pk := s.next(t)
		if pk.kind != screenKindStatus {
			continue
		}
		var st screenStats
		require.NoError(t, json.Unmarshal(pk.payload, &st))
		if state == "" || st.State == state {
			return st
		}
	}
	t.Fatalf("no %q status", state)
	return screenStats{}
}

func openPhoneStream(t *testing.T, p *phone) (*stream, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	resp, err := p.openStream(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, 200, resp.StatusCode)
	require.Equal(t, "no-store", resp.Header.Get("Cache-Control"))
	require.Equal(t, "application/octet-stream", resp.Header.Get("Content-Type"))
	t.Cleanup(func() { cancel(); _ = resp.Body.Close() })
	return &stream{resp: resp, r: bufio.NewReader(resp.Body)}, cancel
}

func TestScreenStreamRequiresPairingPreviewsAndCSRF(t *testing.T) {
	src := &syntheticScreen{width: 1920, height: 1080}
	g, _ := newScreenGateway(t, src)
	p := newPhone(t, g)
	resp, err := p.openStream(t.Context(), nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 401, resp.StatusCode, "no frames before authentication")

	p.pair(true)
	resp, err = p.openStream(t.Context(), nil)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 403, resp.StatusCode, "screen viewing is off by default for a device")

	enablePreviews(t, g, true)
	resp, err = p.openStream(t.Context(), map[string]string{"X-CSRF-Token": ""})
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 403, resp.StatusCode, "a cross-site page cannot add the CSRF header")
	resp, err = p.openStream(t.Context(), map[string]string{"Sec-Fetch-Site": "cross-site"})
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, 403, resp.StatusCode)
	require.Zero(t, src.calls.Load(), "nothing is captured for a refused viewer")

	s, _ := openPhoneStream(t, p)
	f1 := s.nextFrame(t)
	f2 := s.nextFrame(t)
	require.Greater(t, f2.seq, f1.seq, "sequence numbers increase")
	require.Equal(t, 1280, f1.w, "frames are downscaled to the starting resolution tier")
	require.Equal(t, 720, f1.h)
	img, err := jpeg.Decode(bytes.NewReader(f1.payload))
	require.NoError(t, err)
	require.Equal(t, 1280, img.Bounds().Dx())
	require.WithinDuration(t, time.Now(), f1.captured, 5*time.Second)
	st := s.nextStatus(t, "live")
	require.Equal(t, screenTargetFPS, st.Target)
	require.Equal(t, 1920, st.ScreenW)
}

func TestScreenStreamEndsOnRevokeAndPreviewsOff(t *testing.T) {
	src := &syntheticScreen{width: 800, height: 600}
	g, _ := newScreenGateway(t, src)
	p := newPhone(t, g)
	p.pair(true)
	id := enablePreviews(t, g, true)
	s, _ := openPhoneStream(t, p)
	s.nextFrame(t)
	require.NoError(t, g.Auth.SetPreviews(id, false))
	st := s.nextStatus(t, "previews_off")
	require.Equal(t, "previews_off", st.State)
	_, err := s.r.ReadByte()
	require.ErrorIs(t, err, io.EOF, "the stream ends when viewing is turned off")

	require.NoError(t, g.Auth.SetPreviews(id, true))
	s2, _ := openPhoneStream(t, p)
	s2.nextFrame(t)
	start := time.Now()
	require.NoError(t, g.Auth.Revoke(id))
	for {
		_, err := s2.r.ReadByte()
		if err != nil {
			break
		}
	}
	require.Less(t, time.Since(start), 3*time.Second, "revocation ends the stream at once")
}

func TestScreenSharesOneCaptureLoopAndStopsAfterLastViewer(t *testing.T) {
	src := &syntheticScreen{width: 640, height: 360}
	g, b := newScreenGateway(t, src)
	p1 := newPhone(t, g)
	p1.pair(true)
	enablePreviews(t, g, true)
	p2 := newPhone(t, g)
	p2.pair(true)
	enablePreviews(t, g, true)

	s1, cancel1 := openPhoneStream(t, p1)
	s2, cancel2 := openPhoneStream(t, p2)
	before := src.calls.Load()
	var seen1, seen2 []uint64
	for range 12 {
		seen1 = append(seen1, s1.nextFrame(t).seq)
		seen2 = append(seen2, s2.nextFrame(t).seq)
	}
	captures := src.calls.Load() - before
	// Two viewers see the same frames from one loop; two loops would
	// capture about twice as often.
	require.LessOrEqual(t, captures, int64(20), "one capture loop serves every viewer")
	common := map[uint64]bool{}
	for _, s := range seen1 {
		common[s] = true
	}
	shared := 0
	for _, s := range seen2 {
		if common[s] {
			shared++
		}
	}
	require.Greater(t, shared, 6, "both viewers received the same encoded frames")

	cancel1()
	cancel2()
	require.Eventually(t, func() bool {
		g.screen.mu.Lock()
		defer g.screen.mu.Unlock()
		return !g.screen.running && len(g.screen.viewers) == 0
	}, 3*time.Second, 20*time.Millisecond, "capture stops after the last viewer leaves")
	stopped := src.calls.Load()
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, stopped, src.calls.Load())
	require.Zero(t, b.runs.Load(), "watching never starts or changes agent work")
	b.mu.Lock()
	require.Empty(t, b.controls)
	b.mu.Unlock()
}

func TestScreenSlowViewerGetsNewestFrameOnly(t *testing.T) {
	h := newScreenHub(&syntheticScreen{width: 320, height: 200}, nil)
	defer h.close()
	v, err := h.join("phone")
	require.NoError(t, err)
	// Never read: a slow phone. Its queue holds one frame at most.
	time.Sleep(400 * time.Millisecond)
	require.LessOrEqual(t, len(v.slot), 1)
	require.Positive(t, v.dropped.Load(), "older frames are dropped, not queued")
	newest := <-v.slot
	time.Sleep(100 * time.Millisecond)
	next := <-v.slot
	require.Greater(t, next.seq, newest.seq, "a late reader never gets an older frame after a newer one")
	h.leave(v)
}

func TestScreenCaptureFailureAndBlackDesktopAreNotFrames(t *testing.T) {
	src := &syntheticScreen{width: 640, height: 360, err: errors.New("BitBlt failed")}
	g, _ := newScreenGateway(t, src)
	p := newPhone(t, g)
	p.pair(true)
	enablePreviews(t, g, true)
	s, _ := openPhoneStream(t, p)
	st := s.nextStatus(t, "capture_error")
	require.Contains(t, st.Message, "BitBlt")

	src.set(func(s *syntheticScreen) { s.err = nil; s.black = true })
	st = s.nextStatus(t, "blank")
	require.Contains(t, st.Message, "black")
	black1 := s.nextFrame(t)
	black2 := s.nextFrame(t)
	require.Greater(t, black2.seq, black1.seq, "valid black frames continue streaming")
	src.set(func(s *syntheticScreen) { s.black = false })
	f := s.nextFrame(t)
	require.Positive(t, f.seq)
}

func TestScreenFollowsResolutionAndOrientationChanges(t *testing.T) {
	src := &syntheticScreen{width: 1280, height: 720}
	g, _ := newScreenGateway(t, src)
	p := newPhone(t, g)
	p.pair(true)
	enablePreviews(t, g, true)
	s, _ := openPhoneStream(t, p)
	f := s.nextFrame(t)
	require.Equal(t, [2]int{1280, 720}, [2]int{f.w, f.h})
	src.set(func(s *syntheticScreen) { s.width, s.height = 1080, 1920 })
	for range 50 {
		f = s.nextFrame(t)
		if f.w == 1080 {
			break
		}
	}
	require.Equal(t, [2]int{1080, 1920}, [2]int{f.w, f.h}, "portrait monitor")
}

func TestScreenSlowCaptureIsNeverOverlapped(t *testing.T) {
	src := &syntheticScreen{width: 320, height: 200, delay: 80 * time.Millisecond}
	var inFlight, maxInFlight atomic.Int32
	wrapped := ScreenSourceFunc(func(w int) (*image.RGBA, int, int, time.Time, error) {
		n := inFlight.Add(1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		defer inFlight.Add(-1)
		return src.CaptureScreen(w)
	})
	h := newScreenHub(wrapped, nil)
	defer h.close()
	v, err := h.join("p")
	require.NoError(t, err)
	time.Sleep(600 * time.Millisecond)
	h.leave(v)
	require.EqualValues(t, 1, maxInFlight.Load())
	st, _ := h.snapshot()
	require.LessOrEqual(t, st.CaptureFPS, 13.0, "a slow source is reported at its real rate, not 24")
}

func TestScreenViewerLimit(t *testing.T) {
	h := newScreenHub(&syntheticScreen{width: 64, height: 64}, nil)
	defer h.close()
	var vs []*screenViewer
	for range screenMaxViewers {
		v, err := h.join("p")
		require.NoError(t, err)
		vs = append(vs, v)
	}
	_, err := h.join("p")
	require.ErrorIs(t, err, errScreenBusy)
	for _, v := range vs {
		h.leave(v)
	}
}
