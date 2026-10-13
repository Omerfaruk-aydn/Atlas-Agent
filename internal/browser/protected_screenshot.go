}

// ProtectedScreenshot refuses unstable pages rather than delivering pixels
// whose redaction coordinates may belong to a different layout/document.
func (s *chromedpSession) ProtectedScreenshot(ctx context.Context, full, annotated bool, redact func(string) string) ([]byte, int, error) {
	if redact == nil {
		return nil, 0, errors.New("credential_redaction_unavailable")
	}
	restore, err := s.suspendActivity()
	if err != nil {
		return nil, 0, err
	}
	defer restore()
	identity := s.ExecutionIdentity(ctx)
	if identity.Document == "" {
		return nil, 0, errors.New("credential_redaction_unverified_document")
	}
	var elements []SnapshotElement
	if annotated {
		elements, err = s.Snapshot(full)
		if err != nil {
			return nil, 0, err
		}
	}
	before, err := s.privacyLayout(ctx, full)
	if err != nil || before.Width <= 0 {
		return nil, 0, errors.New("credential_redaction_layout_unavailable")
	}
	if before.Width*before.Height*before.Ratio*before.Ratio > 32_000_000 {
		return nil, 0, errors.New("credential_redaction_image_too_large: use a viewport screenshot instead of a full-page capture")
	}
	data, err := s.captureScreenshot(full)
	if err != nil {
		return nil, 0, err
	}
	after, err := s.privacyLayout(ctx, full)
	first, _ := json.Marshal(before)
	last, _ := json.Marshal(after)
	if err != nil || identity != s.ExecutionIdentity(ctx) || sha256.Sum256(first) != sha256.Sum256(last) {
		return nil, 0, errors.New("credential_redaction_stale_layout: observe the stable page before capturing again")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || int64(config.Width)*int64(config.Height) > 32_000_000 {
		return nil, 0, errors.New("credential_redaction_image_too_large_or_invalid")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, img.Bounds().Min, draw.Src)
	scale := float64(img.Bounds().Dx()) / before.Width
	masked := 0
	for _, region := range before.Regions {
		if !region.Mask && redact(region.Text) == region.Text {
			continue
		}
		r := region.Rect
		rect := image.Rect(int(math.Floor(r[0]*scale))-2, int(math.Floor(r[1]*scale))-2, int(math.Ceil((r[0]+r[2])*scale))+2, int(math.Ceil((r[1]+r[3])*scale))+2).Intersect(img.Bounds())
		if !rect.Empty() {
			draw.Draw(canvas, rect, image.NewUniform(color.RGBA{R: 42, G: 44, B: 52, A: 255}), image.Point{}, draw.Src)
			masked++
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, 0, err
	}
	data = buf.Bytes()
	if annotated {
		identities := make(map[string]RefIdentity, len(elements))
		for i := range elements {
			identities[elements[i].Ref] = RefIdentity{Role: elements[i].Role, Name: elements[i].Name}
			elements[i].Name = redact(elements[i].Name)
			elements[i].Value = redact(elements[i].Value)
		}
		if !full {
			visible := elements[:0]
			for _, element := range elements {
				if element.Visible {
					visible = append(visible, element)
				}
			}
			elements = visible
		}
		var refs map[string]RefIdentity
		data, refs, err = annotateScreenshotPNG(data, elements, scale, MaxScreenshotWidth)
		if err != nil || identity != s.ExecutionIdentity(ctx) {
			return nil, 0, errors.New("credential_redaction_stale_annotation")
		}
		// Display redaction must not change the private DOM identity used
		// to validate a ref before input. These names are not image labels.
		for ref := range refs {
			refs[ref] = identities[ref]
		}
		s.mu.Lock()
		s.screenshotIdentity, s.screenshotReferences = identity, refs
		s.mu.Unlock()
	}
	return data, masked, nil
}
