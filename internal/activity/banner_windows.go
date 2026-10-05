//go:build windows

package activity

import (
	"log/slog"
	"math"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type nativeBanner struct {
	surface       nativeEdgeSurface
	glyphs        []byte
	fallback      []byte
	base          []byte // Static layer: background, glyphs and stop box.
	baseActive    bool
	edge          []bannerRimPixel
	slot          []byte
	scale         float64
	caption, stop string
	canStop       bool
	tail          int32
	area          overlayRect
	mascotFailed  bool
	// The status caption and run timer chosen by the renderer. The width
	// floor only grows within a run so changing text never shifts the strip.
	wantCaption, wantTimer string
	timer                  string
	timerGlyphs            []byte
	floor                  int32
}

func (b *nativeBanner) close() {
	b.surface.close()
	*b = nativeBanner{wantCaption: b.wantCaption, wantTimer: b.wantTimer, floor: b.floor}
}

func (b *nativeBanner) prepare(area overlayRect, scale float64, event Event) bool {
	caption, stop := BannerText(event)
	if b.wantCaption != "" {
		caption = b.wantCaption
	}
	timer := b.wantTimer
	if b.surface.dc != 0 && b.area == area && b.scale == scale && b.caption == caption && b.timer == timer && b.stop == stop && b.canStop == event.CanStop {
		return true
	}
	// A failed character stays on the static icon for this banner's life.
	failed := b.mascotFailed
	unit := func(v float64) int32 { return int32(math.Round(v * scale)) }
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	var fonts [3]uintptr
	defer func() {
		for _, font := range fonts {
			if font != 0 {
				overlayGDI.NewProc("DeleteObject").Call(font)
			}
		}
	}()
	for i, spec := range [][2]int32{{13, 600}, {13, 500}, {11, 600}} {
		fonts[i], _, _ = overlayGDI.NewProc("CreateFontW").Call(uintptr(-unit(float64(spec[0]))), 0, 0, 0, uintptr(spec[1]), 0, 0, 0, 1, 0, 0, 4, 0, uintptr(unsafe.Pointer(face)))
		if fonts[i] == 0 {
			return false
		}
	}
	dc, _, _ := overlayGDI.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		return false
	}
	defer overlayGDI.NewProc("DeleteDC").Call(dc)
	old, _, _ := overlayGDI.NewProc("SelectObject").Call(dc, fonts[0])
	defer overlayGDI.NewProc("SelectObject").Call(dc, old)
	measure := func(text string) int32 {
		encoded, _ := syscall.UTF16FromString(text)
		var size overlayPoint
		overlayGDI.NewProc("GetTextExtentPoint32W").Call(dc, uintptr(unsafe.Pointer(&encoded[0])), uintptr(len(encoded)-1), uintptr(unsafe.Pointer(&size)))
		return size.X
	}
	titleWidth := measure(caption)
	overlayGDI.NewProc("SelectObject").Call(dc, fonts[1])
	stopWidth := measure(stop)
	// The timer slot fits its widest form, so ticking never moves text.
	timerSlot := int32(0)
	if timer != "" {
		template := "00:00"
		if len(timer) > 5 {
			template = "0:00:00"
		}
		timerSlot = measure(template) + unit(14)
	}
	tail := int32(0)
	if event.CanStop {
		tail = unit(68) + stopWidth
	}
	w, h := min(max(titleWidth+timerSlot+tail+unit(bannerTextLeft+12), b.floor), area.Right-area.Left-unit(32)), unit(bannerHeight)
	if w < tail+unit(76) || h < 1 {
		return false
	}
	model := &edgePixels{width: int(w), height: int(h)}
	var next, mask nativeEdgeSurface
	if !next.create(model) {
		return false
	}
	if !mask.create(model) {
		next.close()
		return false
	}
	defer mask.close()
	clear(mask.pixels)
	previous, _, _ := overlayGDI.NewProc("SelectObject").Call(mask.dc, fonts[0])
	defer overlayGDI.NewProc("SelectObject").Call(mask.dc, previous)
	overlayGDI.NewProc("SetBkMode").Call(mask.dc, 1)
	overlayGDI.NewProc("SetTextColor").Call(mask.dc, 0xffffff)
	drawText := func(text string, left, right int32, font uintptr) {
		overlayGDI.NewProc("SelectObject").Call(mask.dc, font)
		encoded, _ := syscall.UTF16FromString(text)
		rect := overlayRect{left, 0, right, h}
		flags := uintptr(0x8024)
		if text == "Esc" {
			flags |= 1
		}
		if event.Language == "ar" && text != "Esc" {
			flags |= 0x20000
		}
		overlayUser.NewProc("DrawTextW").Call(mask.dc, uintptr(unsafe.Pointer(&encoded[0])), uintptr(len(encoded)-1), uintptr(unsafe.Pointer(&rect)), flags)
	}
	drawText(caption, unit(bannerTextLeft), w-tail-timerSlot-unit(6), fonts[0])
	if event.CanStop {
		drawText("Esc", w-tail+unit(12), w-tail+unit(42), fonts[2])
		drawText(stop, w-stopWidth-unit(16), w-unit(16), fonts[1])
	}
	overlayGDI.NewProc("GdiFlush").Call()
	glyphs, fallback := make([]byte, int(w*h)), make([]byte, int(w*h))
	for i := range glyphs {
		x, y := i%int(w), i/int(w)
		glyphs[i] = max(mask.pixels[i*4], mask.pixels[i*4+1], mask.pixels[i*4+2])
		fallback[i] = byte(bannerPointerCoverage((float64(x)+.5)/scale, (float64(y)+.5)/scale, scale) * 255)
	}
	var timerGlyphs []byte
	if timer != "" {
		clear(mask.pixels)
		overlayGDI.NewProc("SelectObject").Call(mask.dc, fonts[1])
		encoded, _ := syscall.UTF16FromString(timer)
		rect := overlayRect{w - tail - timerSlot, 0, w - tail - unit(8), h}
		// Right-aligned digits keep their place as the time grows.
		overlayUser.NewProc("DrawTextW").Call(mask.dc, uintptr(unsafe.Pointer(&encoded[0])), uintptr(len(encoded)-1), uintptr(unsafe.Pointer(&rect)), 0x826)
		overlayGDI.NewProc("GdiFlush").Call()
		timerGlyphs = make([]byte, int(w*h))
		for i := range timerGlyphs {
			timerGlyphs[i] = max(mask.pixels[i*4], mask.pixels[i*4+1], mask.pixels[i*4+2])
		}
	}
	slotW, slotH := int(math.Round(MascotWidth*scale)), int(math.Round(MascotHeight*scale))
	wantCaption, wantTimer := b.wantCaption, b.wantTimer
	b.close()
	*b = nativeBanner{surface: next, glyphs: glyphs, fallback: fallback, slot: make([]byte, slotW*slotH*4), scale: scale, caption: caption, stop: stop, canStop: event.CanStop, tail: tail, area: area, mascotFailed: failed, wantCaption: wantCaption, wantTimer: wantTimer, timer: timer, timerGlyphs: timerGlyphs, floor: w}
	return true
}

// paintMascot composites the character into the icon slot. On any render
// failure the banner keeps working with the original static pointer icon.
func (b *nativeBanner) paintMascot(mascot *Mascot) {
	s := &b.surface
	slotW, slotH := int(math.Round(MascotWidth*b.scale)), int(math.Round(MascotHeight*b.scale))
	ox, oy := int(math.Round(bannerMascotLeft*b.scale)), (s.model.height-slotH)/2
	if !b.mascotFailed && mascot != nil {
		if err := mascot.Render(b.slot, slotW, slotH, b.scale); err != nil {
			b.mascotFailed = true
			slog.Warn("Banner character unavailable; using static icon", "error", err)
		}
	}
	if b.mascotFailed || mascot == nil {
		for i, a := range b.fallback {
			if a == 0 {
				continue
			}
			alpha := float64(a) / 255
			for c, foreground := range [3]float64{250, 244, 235} {
				s.pixels[i*4+c] = uint8(float64(s.pixels[i*4+c])*(1-alpha) + foreground*alpha)
			}
		}
		return
	}
	for y := range slotH {
		row := oy + y
		if row < 0 || row >= s.model.height {
			continue
		}
		for x := range slotW {
			col := ox + x
			k := (y*slotW + x) * 4
			if col < 0 || col >= s.model.width || b.slot[k+3] == 0 {
				continue
			}
			i := (row*s.model.width + col) * 4
			keep := 255 - uint16(b.slot[k+3])
			// Premultiplied RGBA over premultiplied BGRA.
			s.pixels[i] = byte(uint16(b.slot[k+2]) + (uint16(s.pixels[i])*keep+127)/255)
			s.pixels[i+1] = byte(uint16(b.slot[k+1]) + (uint16(s.pixels[i+1])*keep+127)/255)
			s.pixels[i+2] = byte(uint16(b.slot[k]) + (uint16(s.pixels[i+2])*keep+127)/255)
			s.pixels[i+3] = byte(uint16(b.slot[k+3]) + (uint16(s.pixels[i+3])*keep+127)/255)
		}
	}
}

// paint updates only what moves: the cycling edge and the character. The
// static layer is rebuilt when the layout or stop state changes.
func (b *nativeBanner) paint(elapsed time.Duration, mascot *Mascot, stopActive bool) {
	s := &b.surface
	if b.base == nil || b.baseActive != stopActive {
		b.paintBase(stopActive)
	}
	copy(s.pixels, b.base)
	paintBannerRim(s.pixels, b.edge, elapsed)
	b.paintMascot(mascot)
}

func (b *nativeBanner) paintBase(stopActive bool) {
	s := &b.surface
	b.edge = paintBanner(s.pixels, s.model.width, s.model.height, b.scale, 0)
	for y := range s.model.height {
		for x := range s.model.width {
			i := y*s.model.width + x
			if b.canStop {
				px, py := (float64(x)+.5)/b.scale, (float64(y)+.5)/b.scale
				seam := float64(s.model.width-int(b.tail)) / b.scale
				left, center := seam+12, float64(bannerHeight)/2
				dx, dy := math.Abs(px-left-15)-11, math.Abs(py-center)-7
				depth := -(math.Hypot(max(0, dx), max(0, dy)) + min(0, max(dx, dy)) - 4)
				mix := float64(0)
				if depth >= 0 {
					mix = .04
					if depth < 1 {
						mix = .2
					}
				}
				if math.Abs(px-seam) < .5 && py >= center-9 && py <= center+9 {
					mix = .14
				}
				for c, foreground := range [3]float64{250, 244, 235} {
					s.pixels[i*4+c] = uint8(float64(s.pixels[i*4+c])*(1-mix) + foreground*mix)
				}
			}
			a := float64(b.glyphs[i]) / 255
			if !stopActive && b.canStop && x >= s.model.width-int(b.tail) {
				// A completed run keeps the stop layout steady but inactive.
				a *= .4
			}
			for c, foreground := range [3]float64{250, 244, 235} {
				s.pixels[i*4+c] = uint8(float64(s.pixels[i*4+c])*(1-a) + foreground*a)
			}
			if b.timerGlyphs != nil {
				// The elapsed time reads as secondary to the status.
				t := float64(b.timerGlyphs[i]) / 255 * .6
				for c, foreground := range [3]float64{250, 244, 235} {
					s.pixels[i*4+c] = uint8(float64(s.pixels[i*4+c])*(1-t) + foreground*t)
				}
			}
		}
	}
	b.base = append(b.base[:0], s.pixels...)
	b.baseActive = stopActive
}

func (r *windowsRenderer) drawBanner(scale float64, elapsed time.Duration) {
	event := r.event
	event.CanStop = event.CanStop && r.keyboardHook != 0
	active := event.CanStop
	if event.Phase == PhaseDone && !event.Persistent && r.banner.canStop && r.banner.surface.dc != 0 {
		// Keep the stop area during the completion motion so text never shifts.
		event.CanStop = true
	}
	if r.bannerWindow == 0 {
		return
	}
	r.prepareBannerText(time.Now())
	if !r.banner.prepare(r.edgeArea, scale, event) {
		overlayUser.NewProc("ShowWindow").Call(r.bannerWindow, 0)
		return
	}
	if r.event.ReducedMotion {
		elapsed = 0
	}
	s := &r.banner.surface
	area := r.edgeArea
	position := overlayPoint{area.Left + (area.Right-area.Left-int32(s.model.width))/2, area.Top + int32(math.Round(20*scale))}
	r.mascot.Advance(r.event, time.Now(), bannerLook(r.event, area, position, scale))
	r.banner.paint(elapsed, r.mascot, active)
	size, origin := overlayPoint{int32(s.model.width), int32(s.model.height)}, overlayPoint{}
	blend := uint32(0x01ff0000)
	ok, _, _ := overlayUser.NewProc("UpdateLayeredWindow").Call(r.bannerWindow, 0, uintptr(unsafe.Pointer(&position)), uintptr(unsafe.Pointer(&size)), s.dc, uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), 2)
	if ok != 0 {
		placeWindow(r.bannerWindow, r.windows[0], 0x0053)
	}
	r.updateStopClick(position, scale)
}

// bannerLook aims the character's gaze from the banner toward the verified
// target point on the same monitor.
func bannerLook(e Event, area overlayRect, position overlayPoint, scale float64) Look {
	if !e.Point {
		return Look{Y: -.25}
	}
	cx := float64(area.Left+area.Right) / 2
	cy := float64(position.Y) + bannerHeight/2*scale
	return Look{
		X: clampUnit((float64(e.X) - cx) / max(1, float64(area.Right-area.Left)/2)),
		Y: clampUnit((cy - float64(e.Y)) / max(1, float64(area.Bottom-area.Top)) * 1.6),
	}
}

type keyboardData struct {
	Key, Scan, Flags, Time uint32
	Extra                  uintptr
}

var keyboardRenderers sync.Map

// Only physical Escape stops control; injected tool key presses pass through.
func physicalEscape(code int32, message uintptr, key keyboardData) bool {
	return code >= 0 && message == 0x100 && key.Key == 0x1b && key.Flags&0x12 == 0
}

var escapeCallback = syscall.NewCallback(func(code int32, message uintptr, data *keyboardData) uintptr {
	if code >= 0 && data != nil && physicalEscape(code, message, *data) {
		physicalEscapeWatchers.Range(func(key, value any) bool {
			if stop := value.(func() func())(); stop != nil {
				go stop()
			}
			return true
		})
		keyboardRenderers.Range(func(key, value any) bool {
			r := key.(*windowsRenderer)
			if e := r.escapeEvent.Load(); e != nil {
				id := e.ID
				go func() {
					r.mu.Lock()
					stop := r.onStop
					r.mu.Unlock()
					if stop != nil {
						stop(id)
					}
				}()
			}
			return true
		})
	}
	next, _, _ := overlayUser.NewProc("CallNextHookEx").Call(0, uintptr(code), message, uintptr(unsafe.Pointer(data)))
	return next
})
