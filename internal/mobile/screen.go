package mobile

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Live screen transport.
//
// One authenticated HTTP GET streams length-prefixed binary frames, read in
// the page with fetch() and a ReadableStream. This was chosen over a
// WebSocket because fetch sends the same SameSite=Strict cookie and can
// carry the X-CSRF-Token header (a browser WebSocket cannot set headers),
// it passes Tailscale Funnel and the relay like the existing event stream,
// and TCP flow control is the backpressure signal. JPEG bytes travel raw,
// never base64.
//
// Frame layout (big endian):
//
//	0  4  magic "ATS1"
//	4  1  kind: 1 JPEG frame, 2 JSON status
//	5  3  reserved
//	8  8  sequence number
//	16 8  capture time, Unix microseconds
//	24 2  width
//	26 2  height
//	28 4  payload length
//	32 …  payload

const (
	screenMagic        = "ATS1"
	screenHeaderSize   = 32
	screenKindFrame    = 1
	screenKindStatus   = 2
	screenTargetFPS    = 24
	screenInterval     = time.Second / screenTargetFPS
	screenMaxViewers   = 4
	screenMaxFrame     = 900 << 10
	screenWriteTimeout = 5 * time.Second
)

// screenWidths are the resolution tiers. The loop steps down when capture
// and encoding cannot keep the frame interval, and back up when they can.
var screenWidths = []int{1600, 1280, 960, 720}

// ScreenSource captures one downscaled desktop frame.
type ScreenSource interface {
	CaptureScreen(maxWidth int) (img *image.RGBA, screenW, screenH int, captured time.Time, err error)
}

// ScreenSourceFunc adapts a function to ScreenSource.
type ScreenSourceFunc func(maxWidth int) (*image.RGBA, int, int, time.Time, error)

func (f ScreenSourceFunc) CaptureScreen(maxWidth int) (*image.RGBA, int, int, time.Time, error) {
	return f(maxWidth)
}

// screenPacket is an encoded frame or status shared by every viewer.
type screenPacket struct {
	kind     byte
	seq      uint64
	captured time.Time
	width    int
	height   int
	payload  []byte
}

func (p *screenPacket) encode() []byte {
	out := make([]byte, screenHeaderSize+len(p.payload))
	copy(out, screenMagic)
	out[4] = p.kind
	binary.BigEndian.PutUint64(out[8:], p.seq)
	binary.BigEndian.PutUint64(out[16:], uint64(p.captured.UnixMicro()))
	binary.BigEndian.PutUint16(out[24:], uint16(p.width))
	binary.BigEndian.PutUint16(out[26:], uint16(p.height))
	binary.BigEndian.PutUint32(out[28:], uint32(len(p.payload)))
	copy(out[screenHeaderSize:], p.payload)
	return out
}

// screenViewer holds at most one undelivered frame: a slow phone always
// gets the newest picture instead of a growing backlog.
type screenViewer struct {
	deviceID string
	slot     chan *screenPacket
	status   chan *screenPacket
	done     chan struct{}
	once     sync.Once
	dropped  atomic.Uint64
	sent     atomic.Uint64
	bytes    atomic.Uint64
}

func (v *screenViewer) end() { v.once.Do(func() { close(v.done) }) }

func (v *screenViewer) offer(p *screenPacket) {
	ch := v.slot
	if p.kind == screenKindStatus {
		ch = v.status
	}
	for {
		select {
		case ch <- p:
			return
		default:
		}
		select {
		case <-ch:
			if p.kind == screenKindFrame {
				v.dropped.Add(1)
			}
		default:
		}
	}
}

// screenStats are measured by the capture loop, not assumed.
type screenStats struct {
	State      string  `json:"state"`
	Message    string  `json:"message,omitempty"`
	CaptureFPS float64 `json:"capture_fps"`
	CaptureMS  float64 `json:"capture_ms"`
	EncodeMS   float64 `json:"encode_ms"`
	FrameBytes int     `json:"frame_bytes"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	ScreenW    int     `json:"screen_width"`
	ScreenH    int     `json:"screen_height"`
	Quality    int     `json:"quality"`
	Viewers    int     `json:"viewers"`
	Missed     uint64  `json:"missed_ticks"`
	Target     int     `json:"target_fps"`
	Changed    string  `json:"quality_change,omitempty"`
	// Per-viewer fields are filled in by each stream.
	SentFPS     float64 `json:"sent_fps,omitempty"`
	BytesPerSec float64 `json:"bytes_per_sec,omitempty"`
	Dropped     uint64  `json:"dropped,omitempty"`
}

// screenHub runs a single capture loop for all viewers of this gateway and
// stops it when the last viewer leaves.
type screenHub struct {
	src     ScreenSource
	log     *slog.Logger
	mu      sync.Mutex
	viewers map[*screenViewer]struct{}
	running bool
	closed  bool
	stats   screenStats
	seq     uint64
}

func newScreenHub(src ScreenSource, log *slog.Logger) *screenHub {
	if log == nil {
		log = slog.Default()
	}
	return &screenHub{src: src, log: log, viewers: map[*screenViewer]struct{}{}}
}

var errScreenBusy = errors.New("too many live screen viewers")

func (h *screenHub) join(deviceID string) (*screenViewer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.src == nil {
		return nil, errors.New("live screen unavailable")
	}
	if len(h.viewers) >= screenMaxViewers {
		return nil, errScreenBusy
	}
	v := &screenViewer{deviceID: deviceID, slot: make(chan *screenPacket, 1), status: make(chan *screenPacket, 1), done: make(chan struct{})}
	h.viewers[v] = struct{}{}
	if !h.running {
		h.running = true
		go h.loop()
	}
	return v, nil
}

func (h *screenHub) leave(v *screenViewer) {
	h.mu.Lock()
	delete(h.viewers, v)
	h.mu.Unlock()
	v.end()
}

// revoke ends the streams of removed devices at once.
func (h *screenHub) revoke(ids []string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for v := range h.viewers {
		if slices.Contains(ids, v.deviceID) {
			v.end()
		}
	}
}

func (h *screenHub) close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for v := range h.viewers {
		v.end()
	}
}

func (h *screenHub) snapshot() (screenStats, []*screenViewer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*screenViewer, 0, len(h.viewers))
	for v := range h.viewers {
		out = append(out, v)
	}
	st := h.stats
	st.Viewers = len(out)
	return st, out
}

func (h *screenHub) publish(p *screenPacket, viewers []*screenViewer) {
	for _, v := range viewers {
		v.offer(p)
	}
}

func (h *screenHub) status(st screenStats, viewers []*screenViewer) {
	data, _ := json.Marshal(st)
	h.mu.Lock()
	h.seq++
	seq := h.seq
	h.mu.Unlock()
	h.publish(&screenPacket{kind: screenKindStatus, seq: seq, captured: time.Now(), payload: data}, viewers)
}

// loop captures on a monotonic schedule. A slow capture is never
// overlapped: the next tick waits for it and missed ticks are counted.
func (h *screenHub) loop() {
	tier := 1
	quality := 62
	next := time.Now()
	var window []time.Time
	var slowRun, fastRun int
	var captureAvg, encodeAvg float64
	var missed uint64
	lastStatus := time.Time{}
	var lastErr string
	var frameBytes int
	var inflight chan encodeResult
	drain := func() {
		if inflight != nil {
			<-inflight
			inflight = nil
		}
	}
	// An encode in flight finishes before the loop exits.
	defer drain()
	for {
		_, viewers := h.snapshot()
		if len(viewers) == 0 {
			drain()
			h.mu.Lock()
			if len(h.viewers) == 0 {
				h.running = false
				h.stats = screenStats{}
				h.mu.Unlock()
				return
			}
			h.mu.Unlock()
			continue
		}
		start := time.Now()
		img, sw, sh, captured, err := h.src.CaptureScreen(screenWidths[tier])
		captureDur := time.Since(start)
		state, msg, changed := "live", "", ""
		if err == nil && uniformBlack(img) {
			// Black content is a valid frame, not proof of a locked desktop.
			state, msg = "blank", "the captured frame is black; desktop availability cannot be inferred from pixel colors"
		}
		if err != nil {
			if state != "blank" {
				state = "capture_error"
			}
			msg = err.Error()
			if msg != lastErr {
				h.log.Warn("Live screen capture failed", "error", err)
				lastErr = msg
			}
			h.mu.Lock()
			h.stats.State, h.stats.Message = state, msg
			st := h.stats
			h.mu.Unlock()
			st.Viewers = len(viewers)
			h.status(st, viewers)
			lastStatus = time.Now()
			// Errors are retried slowly; they are not frames.
			if !sleepUntil(h, time.Now().Add(time.Second)) {
				return
			}
			next = time.Now()
			continue
		}
		lastErr = ""
		// Encoding of this frame overlaps the next capture. GDI readback
		// costs about the same at every width, so running the two stages
		// one after the other would force a lower resolution than either
		// stage needs. At most one encode is in flight, and captures never
		// overlap each other.
		if inflight != nil {
			r := <-inflight
			encodeAvg = ewma(encodeAvg, float64(r.dur.Microseconds())/1000)
			quality, frameBytes = r.quality, r.size
			if r.changed != "" {
				changed = r.changed
			}
			if r.tooBig {
				state, msg = "frame_too_large", "a frame exceeded the size limit and was skipped"
			}
		}
		inflight = make(chan encodeResult, 1)
		go h.encode(img, captured, quality, viewers, inflight)
		now := time.Now()
		window = append(window, now)
		for len(window) > 0 && now.Sub(window[0]) > time.Second {
			window = window[1:]
		}
		captureAvg = ewma(captureAvg, float64(captureDur.Microseconds())/1000)
		work := max(captureAvg, encodeAvg)
		budget := float64(screenInterval.Microseconds()) / 1000
		switch {
		case work > budget*0.95:
			slowRun, fastRun = slowRun+1, 0
		case work < budget*0.5:
			fastRun, slowRun = fastRun+1, 0
		default:
			slowRun, fastRun = 0, 0
		}
		if slowRun >= 24 && tier < len(screenWidths)-1 {
			tier++
			slowRun = 0
			changed = "resolution lowered: capture and encoding could not keep up"
		} else if fastRun >= 96 && tier > 0 {
			tier--
			fastRun = 0
			changed = "resolution raised: capture has headroom"
		}
		b := img.Bounds()
		h.mu.Lock()
		h.stats = screenStats{State: state, Message: msg, CaptureFPS: float64(len(window)), CaptureMS: round1(captureAvg), EncodeMS: round1(encodeAvg), FrameBytes: frameBytes, Width: b.Dx(), Height: b.Dy(), ScreenW: sw, ScreenH: sh, Quality: quality, Missed: missed, Target: screenTargetFPS}
		st := h.stats
		h.mu.Unlock()
		if changed != "" || now.Sub(lastStatus) >= time.Second {
			st.Changed = changed
			st.Viewers = len(viewers)
			h.status(st, viewers)
			lastStatus = now
		}
		next = next.Add(screenInterval)
		if behind := time.Since(next); behind > 0 {
			missed += uint64(behind / screenInterval)
			next = time.Now()
		}
		if !sleepUntil(h, next) {
			return
		}
	}
}

// sleepUntil waits for t unless the hub closes.
// encodeResult reports one finished encode back to the capture loop.
type encodeResult struct {
	dur     time.Duration
	size    int
	quality int
	changed string
	tooBig  bool
}

// encode compresses one captured frame and offers it to the viewers it was
// captured for. Sequence numbers are taken here, so frames keep their
// capture order: only one encode runs at a time.
func (h *screenHub) encode(img *image.RGBA, captured time.Time, quality int, viewers []*screenViewer, out chan<- encodeResult) {
	start := time.Now()
	r := encodeResult{quality: quality}
	var buf bytes.Buffer
	_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality})
	if buf.Len() > screenMaxFrame && quality > 35 {
		r.quality -= 10
		buf.Reset()
		_ = jpeg.Encode(&buf, img, &jpeg.Options{Quality: r.quality})
		r.changed = "jpeg quality lowered to fit the frame size limit"
	}
	r.size = buf.Len()
	if buf.Len() > screenMaxFrame {
		r.tooBig = true
	} else {
		h.mu.Lock()
		h.seq++
		seq := h.seq
		h.mu.Unlock()
		b := img.Bounds()
		h.publish(&screenPacket{kind: screenKindFrame, seq: seq, captured: captured, width: b.Dx(), height: b.Dy(), payload: buf.Bytes()}, viewers)
	}
	r.dur = time.Since(start)
	out <- r
}

func sleepUntil(h *screenHub, t time.Time) bool {
	d := time.Until(t)
	if d > 0 {
		time.Sleep(d)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		h.running = false
		return false
	}
	return true
}

func ewma(prev, v float64) float64 {
	if prev == 0 {
		return v
	}
	return prev*0.85 + v*0.15
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// uniformBlack detects a flat, nearly black frame on a sample grid.
func uniformBlack(img *image.RGBA) bool {
	if img == nil || img.Rect.Empty() {
		return true
	}
	b := img.Rect
	for gy := range 17 {
		for gx := range 17 {
			c := img.RGBAAt(b.Min.X+(b.Dx()-1)*gx/16, b.Min.Y+(b.Dy()-1)*gy/16)
			if c.R > 6 || c.G > 6 || c.B > 6 {
				return false
			}
		}
	}
	return true
}

// handleScreenStream streams the live screen to a phone that has screen
// viewing enabled on the computer. The request must carry the device's
// CSRF token in a header, which only same-origin script can add, so a
// cross-site page cannot start a stream with the phone's cookie.
func (g *Gateway) handleScreenStream(w http.ResponseWriter, r *http.Request) {
	d := device(r)
	if g.opts.Screen == nil || !d.Previews {
		apiError(w, http.StatusForbidden, "previews_off", "live screen is off for this phone; enable screen viewing for this device in Atlas (/mobile)")
		return
	}
	if r.Header.Get("X-CSRF-Token") == "" || r.Header.Get("X-CSRF-Token") != d.CSRF {
		apiError(w, http.StatusForbidden, "bad_csrf", "missing or wrong CSRF token; reload the page")
		return
	}
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		apiError(w, http.StatusForbidden, "bad_origin", "live screen requests must come from this page")
		return
	}
	if g.rateLimited("screen:"+d.ID, 30) {
		apiError(w, http.StatusTooManyRequests, "rate_limited", "too many live screen connections; wait a moment")
		return
	}
	viewer, err := g.screen.join(d.ID)
	if err != nil {
		code := http.StatusConflict
		if errors.Is(err, errScreenBusy) {
			code = http.StatusTooManyRequests
		}
		apiError(w, code, "screen_unavailable", err.Error())
		return
	}
	defer g.screen.leave(viewer)
	rc := http.NewResponseController(w)
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_ = rc.Flush()
	write := func(p *screenPacket) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(screenWriteTimeout))
		data := p.encode()
		if _, err := w.Write(data); err != nil {
			return false
		}
		if rc.Flush() != nil {
			return false
		}
		if p.kind == screenKindFrame {
			viewer.sent.Add(1)
		}
		viewer.bytes.Add(uint64(len(data)))
		return true
	}
	cookie, _ := r.Cookie(cookieName)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var lastSent, lastBytes uint64
	lastTick := time.Now()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-viewer.done:
			return
		case p := <-viewer.slot:
			if !write(p) {
				return
			}
		case p := <-viewer.status:
			var st screenStats
			if json.Unmarshal(p.payload, &st) == nil {
				now := time.Now()
				span := now.Sub(lastTick).Seconds()
				if span > 0 {
					sent, bytes := viewer.sent.Load(), viewer.bytes.Load()
					st.SentFPS = round1(float64(sent-lastSent) / span)
					st.BytesPerSec = float64(int(float64(bytes-lastBytes) / span))
					lastSent, lastBytes, lastTick = sent, bytes, now
				}
				st.Dropped = viewer.dropped.Load()
				p = &screenPacket{kind: p.kind, seq: p.seq, captured: p.captured, payload: mustJSON(st)}
			}
			if !write(p) {
				return
			}
		case <-tick.C:
			// Revocation, logout, expiry and turning viewing off all end
			// the stream within a second.
			dev, err := g.Auth.Authenticate(cookie.Value)
			if err != nil || dev.ID != d.ID || !dev.Previews {
				state := map[bool]string{true: "revoked", false: "previews_off"}[err != nil || dev.ID != d.ID]
				write(&screenPacket{kind: screenKindStatus, captured: time.Now(), payload: mustJSON(screenStats{State: state})})
				return
			}
		}
	}
}

func mustJSON(v any) []byte {
	data, _ := json.Marshal(v)
	return data
}
