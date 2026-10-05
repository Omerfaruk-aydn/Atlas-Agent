//go:build windows

package activity

import (
	"math"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type nativeBanner struct {
	surface       nativeEdgeSurface
	glyphs        []byte
	scale         float64
	caption, stop string
	canStop       bool
	tail          int32
	area          overlayRect
}

func (b *nativeBanner) close() { b.surface.close(); *b = nativeBanner{} }

func (b *nativeBanner) prepare(area overlayRect, scale float64, event Event) bool {
	caption, stop := BannerText(event)
	if b.surface.dc != 0 && b.area == area && b.scale == scale && b.caption == caption && b.stop == stop && b.canStop == event.CanStop {
		return true
	}
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
	tail := int32(0)
	if event.CanStop {
		tail = unit(68) + stopWidth
	}
	w, h := min(titleWidth+tail+unit(56), area.Right-area.Left-unit(32)), unit(bannerHeight)
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
	drawText(caption, unit(44), w-tail-unit(12), fonts[0])
	if event.CanStop {
		drawText("Esc", w-tail+unit(12), w-tail+unit(42), fonts[2])
		drawText(stop, w-stopWidth-unit(16), w-unit(16), fonts[1])
	}
	overlayGDI.NewProc("GdiFlush").Call()
	glyphs := make([]byte, int(w*h))
	for i := range glyphs {
		x, y := i%int(w), i/int(w)
		pointer := bannerPointerCoverage((float64(x)+.5)/scale, (float64(y)+.5)/scale, scale)
		glyphs[i] = max(mask.pixels[i*4], mask.pixels[i*4+1], mask.pixels[i*4+2], byte(pointer*255))
	}
	b.close()
	*b = nativeBanner{surface: next, glyphs: glyphs, scale: scale, caption: caption, stop: stop, canStop: event.CanStop, tail: tail, area: area}
	return true
}

func (b *nativeBanner) paint(elapsed time.Duration) {
	s := &b.surface
	paintBanner(s.pixels, s.model.width, s.model.height, b.scale, elapsed)
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
			for c, foreground := range [3]float64{250, 244, 235} {
				s.pixels[i*4+c] = uint8(float64(s.pixels[i*4+c])*(1-a) + foreground*a)
			}
		}
	}
}

func (r *windowsRenderer) drawBanner(scale float64, elapsed time.Duration) {
	event := r.event
	event.CanStop = event.CanStop && r.keyboardHook != 0
	if r.bannerWindow == 0 {
		return
	}
	if !r.banner.prepare(r.edgeArea, scale, event) {
		overlayUser.NewProc("ShowWindow").Call(r.bannerWindow, 0)
		return
	}
	if r.event.ReducedMotion {
		elapsed = 0
	}
	r.banner.paint(elapsed)
	s := &r.banner.surface
	area := r.edgeArea
	position := overlayPoint{area.Left + (area.Right-area.Left-int32(s.model.width))/2, area.Top + int32(math.Round(20*scale))}
	size, origin := overlayPoint{int32(s.model.width), int32(s.model.height)}, overlayPoint{}
	blend := uint32(0x01ff0000)
	ok, _, _ := overlayUser.NewProc("UpdateLayeredWindow").Call(r.bannerWindow, 0, uintptr(unsafe.Pointer(&position)), uintptr(unsafe.Pointer(&size)), s.dc, uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), 2)
	if ok != 0 {
		overlayUser.NewProc("SetWindowPos").Call(r.bannerWindow, r.windows[0], 0, 0, 0, 0, 0x0053)
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
