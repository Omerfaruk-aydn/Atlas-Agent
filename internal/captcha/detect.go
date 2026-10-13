		AnswerSelector:      d.AnswerSelector,
		VerifySelector:      d.VerifySelector,
		FrameID:             d.FrameID,
		RotationHint:        d.RotationHint,
		ClickTargetSelector: d.ClickTarget,
		SliderSelector:      d.SliderSelector,
		Confidence:          d.Confidence,
	}
	return d, ch, nil
}

// Present reports whether the detector found something worth solving.
func (d Detected) Present() bool {
	return d.Kind != KindUnknown && d.Kind != KindNone && d.Kind != ""
}

// KindNone is the script's sentinel for "no challenge".
const KindNone Kind = "none"
