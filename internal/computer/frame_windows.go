	defer procDeleteObject.Call(bitmap)
	err := withCaptureBitmap(func() (uintptr, error) {
		old, _, callErr := procSelectObject.Call(memDC, bitmap)
		if old == 0 || old == ^uintptr(0) {
			return 0, fmt.Errorf("computer-use: SelectObject failed: %v", callErr)
		}
		return old, nil
	}, func(old uintptr) error {
		previous, _, callErr := procSelectObject.Call(memDC, old)
		if previous == 0 || previous == ^uintptr(0) {
			return fmt.Errorf("computer-use: bitmap restoration failed: %v", callErr)
		}
		return nil
	}, func() error {
		if ow == w && oh == h {
			ok, _, callErr := procBitBlt.Call(memDC, 0, 0, uintptr(w), uintptr(h), screenDC, uintptr(x), uintptr(y), srccopy)
			if ok == 0 {
				return fmt.Errorf("computer-use: BitBlt failed: %v (%s)", callErr, captureHint())
			}
			return nil
		}
		procSetStretchBltMode.Call(memDC, halftone)
		procSetBrushOrgEx.Call(memDC, 0, 0, 0)
		ok, _, callErr := procStretchBlt.Call(memDC, 0, 0, uintptr(ow), uintptr(oh), screenDC, uintptr(x), uintptr(y), uintptr(w), uintptr(h), srccopy)
		if ok == 0 {
			return fmt.Errorf("computer-use: StretchBlt failed: %v (%s)", callErr, captureHint())
		}
		return nil
	})
	if err != nil {
		return Frame{}, err
	}
	if ok, _, callErr := procGdiFlush.Call(); ok == 0 {
		return Frame{}, fmt.Errorf("computer-use: GdiFlush failed: %v", callErr)
	}
	captured := time.Now()
	pixels := unsafe.Slice((*byte)(bits), 4*ow*oh)
	img := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for i := 0; i < len(pixels); i += 4 {
		img.Pix[i] = pixels[i+2]
		img.Pix[i+1] = pixels[i+1]
		img.Pix[i+2] = pixels[i]
		img.Pix[i+3] = 0xFF
	}
	return Frame{Image: img, Screen: Size{Width: w, Height: h}, Captured: captured}, nil
}
