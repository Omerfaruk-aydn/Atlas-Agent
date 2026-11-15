package design

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQRPathEncodesAScannableGrid(t *testing.T) {
	t.Parallel()
	out, err := QRPath("https://sehirhatlari.istanbul/bilet/0840")
	require.NoError(t, err)
	size := out["size"].(int)
	require.GreaterOrEqual(t, size, 21)
	require.Zero(t, (size-21)%4, "a QR code is 21+4k modules wide")
	// The three finder squares start in the corners.
	d := out["d"].(string)
	require.True(t, strings.HasPrefix(d, "M0 0h7v1"), d[:20])
	_, err = QRPath("  ")
	require.Error(t, err)
}

func TestScriptStyleHelpersDrawAndPassLint(t *testing.T) {
	t.Parallel()
	code := `
colors({ bg: ["#0B0D0C", "#0B0D0C"], surface: ["#16191A", "#16191A"], "surface-2": ["#24282A", "#24282A"], text: ["#F4F6F5", "#F4F6F5"], "text-muted": ["#9AA3A0", "#9AA3A0"], line: ["#2C3133", "#2C3133"], brand: ["#C8F531", "#C8F531"], "on-brand": ["#0B0D0C", "#0B0D0C"], cyan: ["#22E3F0", "#22E3F0"], z1: ["#5BC0F8", "#5BC0F8"], z2: ["#43D17A", "#43D17A"], z3: ["#F7D046", "#F7D046"], z4: ["#F7832A", "#F7832A"], z5: ["#EF4444", "#EF4444"], paper: ["#FFFFFF", "#FFFFFF"], ink: ["#0B1E3F", "#0B1E3F"] });
type({ family: "Geist", styles: { display: { size: 64, weight: 800, line: 68, tracking: -2 }, title: { size: 20, weight: 700, line: 26 }, body: { size: 15, weight: 400, line: 21 }, caption: { size: 12, weight: 500, line: 16 } } });
space([2, 4, 6, 8, 12, 16, 20, 24, 32]);
radius({ sm: 8, md: 16, lg: 24, full: 999 });
place(screen("01 Helpers", { device: "iphone", h: 1400, fill: "bg", gap: 20, pad: [60, 16, 40, 16] }, [
  text("10,42 km", "display"),
  chart("area", [3, 5, 4, 7, 6, 9, 8, 12, 10, 14], { w: 361, h: 120, grid: 4, glow: true }),
  chart("bars", [5.1, 4.9, 5.0, 5.0, 4.8, 4.0, 5.0, 4.6, 5.1, 5.9], { w: 361, h: 150, highlight: "all", behind: true, labels: ["1", "2", "3", "4", "5", "6", "7", "8", "9", "10"], values: ["5:10", "4:58", "5:02", "5:02", "4:48", "4:03", "5:02", "4:38", "5:08", "5:55"], gap: 6 }),
  chart("zones", ["z1", "z2", "z3", "z4", "z5"], { w: 361, value: 0.7, labels: ["Rest", "Warm", "Burn", "Aero", "Max"] }),
  chart("wave", 56, { w: 240, h: 40, color: "text-muted" }),
  col({ name: "Ticket", w: "fill", h: 300, radius: "lg", fill: "paper", clip: true, pad: 20, gap: 12, cross: "center" }, [
    qr("https://example.com/ticket/0840", { size: 140, color: "ink" }),
    notches({ w: 361, y: 220, r: 12, color: "bg" }),
  ]),
  frame({ name: "Photo card", w: "fill", h: 180, radius: "md", fill: "surface-2", clip: true }, [
    scrim({ w: 361, h: 180, absolute: true, x: 0, y: 0 }),
  ]),
  trail([[0.1, 0.8], [0.3, 0.55], [0.6, 0.7], [0.85, 0.35], [0.5, 0.15]], { w: 361, h: 220, width: 24, color: "surface-2", labelStyle: "title",
    nodes: [{ label: 1, color: "z5", ink: "paper" }, { label: 2, color: "z2", ink: "paper" }, { label: 3, color: "cyan", ink: "ink", ring: "paper" }, { label: 4, color: "line", ink: "text-muted" }, { label: 5, color: "line", ink: "text-muted" }] }),
]), { page: "Screens" });`
	res, err := RunScript(context.Background(), New("helpers"), nil, code, ScriptOptions{Name: "h"})
	require.NoError(t, err)
	names := map[string]int{}
	Walk(res.Doc.Pages[0].Children, func(n *Node, _ *Node, _ int) bool {
		names[n.Name]++
		return true
	})
	for _, want := range []string{"Chart/area", "Chart/bars", "Chart/zones", "Chart/wave", "QR code", "Tear line", "Scrim", "Trail", "Marker", "Grid"} {
		require.Positive(t, names[want], want)
	}
	screens := res.Doc.PageByRef("Screens")
	require.NotNil(t, screens)
	require.Equal(t, 1400.0, screens.Children[0].Height, "a scrolling screen keeps its length")
	report, err := Lint(res.Doc, screens.ID, "")
	require.NoError(t, err)
	for _, is := range report.Issues {
		require.NotEqual(t, "error", is.Severity, is.Rule+": "+is.Message)
	}
	if dir := os.Getenv("KIT_PREVIEW"); dir != "" {
		svg, err := SVG(res.Doc, screens, nil)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "helpers.svg"), []byte(svg), 0o644))
	}
}
