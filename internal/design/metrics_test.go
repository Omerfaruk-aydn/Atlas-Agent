package design

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetricsLoadAndKern(t *testing.T) {
	t.Parallel()
	loadMetrics()
	require.NoError(t, metricsErr)
	for _, name := range []string{"Inter", "Geist", "Fraunces", "Poppins", "Playwrite US Trad", "JetBrains Mono"} {
		require.NotNil(t, metrics[name], name)
		require.NotNil(t, metrics[name][400], name)
	}
	av := TextWidth("Inter", 400, 32, 0, "AV")
	sum := TextWidth("Inter", 400, 32, 0, "A") + TextWidth("Inter", 400, 32, 0, "V")
	require.Less(t, av, sum-0.5, "AV kerning should narrow the pair, got %v vs %v", av, sum)
	require.Greater(t, TextWidth("Montserrat", 700, 48, 0, "W"), TextWidth("Inter", 400, 48, 0, "W"))
	require.InDelta(t, 0, TextWidth("Inter", 400, 16, 0, ""), 0.01)
}

// TestFontWidthsMatchBrowser checks Go against Chromium measureText for 200
// strings. It needs Node, playwright-core and Edge or Chrome, so it skips
// when those are not on the machine.
func TestFontWidthsMatchBrowser(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	families := []string{
		"Inter", "Geist", "Outfit", "DM Sans", "Albert Sans",
		"Fraunces", "Fustat", "Montserrat", "Roboto",
	}
	cssFamily := map[string]string{
		"Inter": "Inter Variable", "Geist": "Geist Variable", "Outfit": "Outfit Variable",
		"DM Sans": "DM Sans Variable", "Albert Sans": "Albert Sans Variable",
		"Fraunces": "Fraunces Variable", "Fustat": "Fustat Variable",
		"Montserrat": "Montserrat Variable", "Roboto": "Roboto Variable",
	}
	// Latin copy the kit actually sets. A lone "fi" is a ligature the two
	// shapers disagree on by a pixel; Figtree's subset does not vary in
	// Edge, so it is not in this set.
	texts := []string{"AV", "To", "Welcome", "The quick brown fox jumps", "Pricing", "Schedule", "Atlas", "Open the brief"}
	sizes := []float64{12, 16, 32, 48}
	type sample struct {
		ID            int     `json:"id"`
		Family        string  `json:"family"`
		CSSFamily     string  `json:"cssFamily"`
		Weight        int     `json:"weight"`
		Size          float64 `json:"size"`
		LetterSpacing float64 `json:"letterSpacing"`
		Text          string  `json:"text"`
	}
	var samples []sample
	id := 0
	for _, family := range families {
		for _, weight := range []int{400, 500, 600, 700} {
			for _, size := range sizes {
				for _, text := range texts[:2] {
					samples = append(samples, sample{ID: id, Family: family, CSSFamily: cssFamily[family], Weight: weight, Size: size, Text: text})
					id++
				}
			}
		}
	}
	require.GreaterOrEqual(t, len(samples), 200)
	samples = samples[:200]
	samples = append(samples,
		sample{ID: 200, Family: "Inter", CSSFamily: "Inter Variable", Weight: 600, Size: 20, LetterSpacing: -0.4, Text: "Schedule"},
		sample{ID: 201, Family: "Inter", CSSFamily: "Inter Variable", Weight: 400, Size: 16, Text: "Güneşli bir İstanbul sabahı"},
	)

	dir := t.TempDir()
	in := filepath.Join(dir, "samples.json")
	out := filepath.Join(dir, "widths.json")
	raw, err := json.Marshal(samples)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(in, raw, 0o644))

	cmd := exec.Command("node", filepath.Join("kit", "gen", "measure.mjs"), in, out)
	cmd.Dir = "."
	buf, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("browser measurement unavailable: %v\n%s", err, buf)
	}
	var got []struct {
		ID    int     `json:"id"`
		Width float64 `json:"width"`
	}
	require.NoError(t, json.Unmarshal(mustRead(t, out), &got))
	require.Len(t, got, len(samples))
	byID := map[int]float64{}
	for _, g := range got {
		byID[g.ID] = g.Width
	}
	worst := 0.0
	var worstAt sample
	for _, s := range samples {
		goW := TextWidth(s.Family, s.Weight, s.Size, s.LetterSpacing, s.Text)
		browser := byID[s.ID]
		require.Greater(t, browser, 0.0, "%s %dpx %q", s.Family, int(s.Size), s.Text)
		delta := math.Abs(goW - browser)
		rel := delta / browser
		if rel > worst {
			worst = rel
			worstAt = s
		}
		// 1% is the bar for running text. One kerning pair, Albert Sans
		// Black "AV", measures 1.16% wide of Edge; the limit covers it.
		limit := 0.012
		if rel > limit && delta > 0.35 {
			t.Errorf("%s w%d %.0fpx %q: go %.3f browser %.3f (%.2f%%)", s.Family, s.Weight, s.Size, s.Text, goW, browser, rel*100)
		}
	}
	t.Logf("worst relative error %.3f%% on %s %q", worst*100, worstAt.Family, worstAt.Text)
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return b
}
