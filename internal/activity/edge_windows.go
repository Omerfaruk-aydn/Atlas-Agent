//go:build windows

package activity

import (
	"time"
	"unsafe"
)

type nativeEdgeSurface struct {
	dc, bitmap, old uintptr
	pixels          []byte
	model           *edgePixels
	painted         bool
	reduced         bool
}

func (s *nativeEdgeSurface) close() {
	if s.dc != 0 {
		overlayGDI.NewProc("SelectObject").Call(s.dc, s.old)
		overlayGDI.NewProc("DeleteObject").Call(s.bitmap)
		overlayGDI.NewProc("DeleteDC").Call(s.dc)
	}
	*s = nativeEdgeSurface{}
}

func (s *nativeEdgeSurface) create(model *edgePixels) bool {
	s.close()
	dc, _, _ := overlayGDI.NewProc("CreateCompatibleDC").Call(0)
	if dc == 0 {
		return false
	}
	var info struct {
		Size                        uint32
		Width, Height               int32
		Planes, BitCount            uint16
		Compression, ImageSize      uint32
		XPels, YPels                int32
		ColorsUsed, ColorsImportant uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	info.Width, info.Height = int32(model.width), -int32(model.height)
	info.Planes, info.BitCount = 1, 32
	var bits unsafe.Pointer
	bitmap, _, _ := overlayGDI.NewProc("CreateDIBSection").Call(dc, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		if bitmap != 0 {
			overlayGDI.NewProc("DeleteObject").Call(bitmap)
		}
		overlayGDI.NewProc("DeleteDC").Call(dc)
		return false
	}
	old, _, _ := overlayGDI.NewProc("SelectObject").Call(dc, bitmap)
	if old == 0 || old == ^uintptr(0) {
		overlayGDI.NewProc("DeleteObject").Call(bitmap)
		overlayGDI.NewProc("DeleteDC").Call(dc)
		return false
	}
	*s = nativeEdgeSurface{dc: dc, bitmap: bitmap, old: old, pixels: unsafe.Slice((*byte)(bits), model.width*model.height*4), model: model}
	return true
}

func (r *windowsRenderer) drawEdges(monitor uintptr, scale float64) {
	if len(r.windows) != 5 {
		return
	}
	var info struct {
		Size          uint32
		Monitor, Work overlayRect
		Flags         uint32
	}
	info.Size = uint32(unsafe.Sizeof(info))
	ok, _, _ := overlayUser.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info)))
	if ok == 0 {
		r.hideEdges()
		return
	}
	area := info.Monitor
	width, height := int(area.Right-area.Left), int(area.Bottom-area.Top)
	if width <= 0 || height <= 0 {
		r.hideEdges()
		return
	}
	if (area != r.edgeArea || scale != r.edgeScale) && !r.replaceEdges(area, scale, (*nativeEdgeSurface).create) {
		r.hideEdges()
		return
	}
	bounds := edgeBounds(int(area.Left), int(area.Top), width, height, scale)
	elapsed := time.Since(r.epoch)
	if r.event.ReducedMotion {
		elapsed = 0
	}
	// Preserve cursor > banner > smoke even between individual surface updates.
	// While a request is open the island sits on top and owns that slot.
	anchor := r.bannerWindow
	if anchor == 0 {
		anchor = r.windows[0]
	} else if !r.island.open {
		placeWindow(anchor, r.windows[0], 0x0013)
	}
	for i := range r.edges {
		s := &r.edges[i]
		b, hwnd := bounds[i], r.windows[i+1]
		if !s.painted || !r.event.ReducedMotion || s.reduced != r.event.ReducedMotion {
			s.model.paint(s.pixels, elapsed)
			position, size, origin := overlayPoint{int32(b.X), int32(b.Y)}, overlayPoint{int32(b.W), int32(b.H)}, overlayPoint{}
			blend := uint32(0x01ff0000)
			ok, _, _ := overlayUser.NewProc("UpdateLayeredWindow").Call(hwnd, 0, uintptr(unsafe.Pointer(&position)), uintptr(unsafe.Pointer(&size)), s.dc, uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), 2)
			if ok == 0 {
				r.hideEdges()
				return
			}
			s.painted, s.reduced = true, r.event.ReducedMotion
		}
		// Show in place; ShowWindow can raise a strip over the banner. The
		// strips form a chain so a settled stack is never reordered.
		placeWindow(hwnd, anchor, 0x0053)
		anchor = hwnd
	}
}

// Replace all monitor surfaces together so a failed resize retains valid ones.
func (r *windowsRenderer) replaceEdges(area overlayRect, scale float64, create func(*nativeEdgeSurface, *edgePixels) bool) bool {
	var next [4]nativeEdgeSurface
	for i := range next {
		if !create(&next[i], newEdgePixels(int(area.Right-area.Left), int(area.Bottom-area.Top), scale, i)) {
			for j := range next {
				next[j].close()
			}
			return false
		}
	}
	for i := range r.edges {
		r.edges[i].close()
	}
	r.edges, r.edgeArea, r.edgeScale = next, area, scale
	return true
}

func (r *windowsRenderer) hideEdges() {
	overlayUser.NewProc("ShowWindow").Call(r.bannerWindow, 0)
	for _, hwnd := range r.windows[1:] {
		overlayUser.NewProc("ShowWindow").Call(hwnd, 0)
	}
}
