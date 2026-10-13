		}
	}
	return MonitorObservation{}, false
}

// VirtualBounds is the bounding rectangle of all monitors.
func VirtualBounds(monitors []MonitorObservation) (x, y, w, h int) {
	if len(monitors) == 0 {
		return 0, 0, 0, 0
	}
	minX, minY := monitors[0].X, monitors[0].Y
	maxX, maxY := minX+monitors[0].Width, minY+monitors[0].Height
	for _, m := range monitors[1:] {
		minX, minY = min(minX, m.X), min(minY, m.Y)
		maxX, maxY = max(maxX, m.X+m.Width), max(maxY, m.Y+m.Height)
	}
	return minX, minY, maxX - minX, maxY - minY
}
