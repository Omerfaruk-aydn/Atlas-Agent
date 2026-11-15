package design

// LayoutGrid is a frame's guide grid, as in Figma: columns, rows or a
// square grid the designer aligns to. It is drawn in the editor only, never
// exported.
type LayoutGrid struct {
	Pattern string  `json:"pattern"`          // columns, rows or grid
	Count   int     `json:"count,omitempty"`  // columns and rows: how many
	Size    float64 `json:"size,omitempty"`   // grid: the cell; columns and rows: a fixed track (0 stretches)
	Gutter  float64 `json:"gutter,omitempty"` // between tracks
	Margin  float64 `json:"margin,omitempty"` // before the first and after the last track
	Align   string  `json:"align,omitempty"`  // stretch, start, center or end
	Color   string  `json:"color,omitempty"`  // #RRGGBBAA
	Hidden  bool    `json:"hidden,omitempty"`
}

// maxGrids bounds one frame's grids.
const maxGrids = 8

func sanitizeGrids(t NodeType, grids []LayoutGrid) []LayoutGrid {
	if t != TypeFrame && t != TypeComponent || len(grids) == 0 {
		return nil
	}
	out := make([]LayoutGrid, 0, min(len(grids), maxGrids))
	for _, g := range grids {
		if len(out) == maxGrids {
			break
		}
		g.Pattern = oneOf(g.Pattern, "columns", "columns", "rows", "grid")
		g.Align = oneOf(g.Align, "stretch", "stretch", "start", "center", "end")
		if g.Count <= 0 {
			g.Count = 5 // Figma's default
		}
		g.Count = min(g.Count, 100)
		g.Size = clamp(g.Size, 0, 10000)
		g.Gutter = clamp(g.Gutter, 0, 10000)
		g.Margin = clamp(g.Margin, 0, 10000)
		if g.Pattern == "grid" {
			if g.Size < 1 {
				g.Size = 8
			}
			g.Count, g.Gutter, g.Margin, g.Align = 0, 0, 0, ""
		}
		if !IsColor(g.Color) {
			g.Color = ""
		}
		out = append(out, g)
	}
	return out
}
