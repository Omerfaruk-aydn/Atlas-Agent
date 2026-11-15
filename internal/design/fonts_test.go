package design

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFontStackPointsAtTheBundledFace(t *testing.T) {
	t.Parallel()
	require.Equal(t, "Fraunces Variable, Fraunces, serif", FontStack("Fraunces"))
	require.Equal(t, "Fraunces Variable, serif", FontStack("Fraunces Variable"))
	require.Equal(t, "Albert Sans Variable, Albert Sans, sans-serif", FontStack("Albert Sans"))
	require.Equal(t, "Inter Variable, Inter, sans-serif", FontStack("Inter"))
	require.Equal(t, "Poppins, sans-serif", FontStack("Poppins"))
	require.Equal(t, "SF Pro, Inter Variable, sans-serif", FontStack("SF Pro"))
	require.True(t, KitFont("geist"))
	require.False(t, KitFont("SF Pro"))
}

func TestSVGWritesTheBundledFamily(t *testing.T) {
	t.Parallel()
	d := New("fonts")
	page := d.Pages[0]
	page.Children = []*Node{{ID: "t", Type: TypeText, Name: "Title", Width: 200, Height: 40, Text: new("48 min"), Font: &Font{Family: "Fraunces", Size: 32, Weight: 600}, Fills: []Paint{{Type: "solid", Color: "#111111"}}}}
	svg, err := SVG(d, page, nil)
	require.NoError(t, err)
	require.Contains(t, svg, `font-family="Fraunces Variable, Fraunces, serif"`)
}

func TestSystemLintWarnsAboutFacesTheRendererLacks(t *testing.T) {
	t.Parallel()
	d := New("fonts")
	d.Pages[0].Children = []*Node{{ID: "t", Type: TypeText, Name: "Title", Width: 200, Height: 40, Text: new("Hi"), Font: &Font{Family: "SF Pro Display", Size: 32, Weight: 600}}}
	var found bool
	for _, is := range SystemLint(d, nil, "") {
		if is.Rule == "font-missing" {
			found = true
			require.True(t, strings.Contains(is.Message, "Fraunces"))
		}
	}
	require.True(t, found)
}
