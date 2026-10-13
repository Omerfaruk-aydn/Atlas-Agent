	require.Equal(t, size.Width, w, "monitor bounds must be physical like the capture (got %+v)", monitors)
	require.Equal(t, size.Height, h)
	for _, m := range monitors {
		require.GreaterOrEqual(t, m.DPI, 96)
		require.InDelta(t, float64(m.DPI)/96, m.ScaleFactor, 1e-9)
		require.Equal(t, CoordinateSpacePhysical, m.CoordinateSpace)
	}
}
