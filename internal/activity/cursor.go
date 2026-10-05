package activity

import (
	_ "embed"
	"image"
	"image/color"
	"math"
)

// CursorSVG is the browser counterpart of the native alpha cursor surface.
//
//go:embed cursor.svg
var CursorSVG string

const (
	cursorSize    = 112
	cursorHotspot = 48
)

// cursorImage produces premultiplied pixels for a layered Windows surface.
func cursorImage() *image.RGBA { return cursorImageAtScale(1) }

func cursorImageAtScale(scale float64) *image.RGBA {
	return cursorStateImage(scale, false, false)
}

var cursorBodyPoints = func() [][2]float64 {
	points := [][2]float64{{48, 48}, {70.8, 62.25}}
	curve := func(control, end [2]float64) {
		start := points[len(points)-1]
		for i := 1; i <= 8; i++ {
			t := float64(i) / 8
			points = append(points, [2]float64{(1-t)*(1-t)*start[0] + 2*(1-t)*t*control[0] + t*t*end[0], (1-t)*(1-t)*start[1] + 2*(1-t)*t*control[1] + t*t*end[1]})
		}
	}
	curve([2]float64{72, 63}, [2]float64{70.6, 63.25})
	points = append(points, [2]float64{61.7, 64.87})
	curve([2]float64{61, 65}, [2]float64{60.65, 65.6})
	points = append(points, [2]float64{56.6, 72.97})
	curve([2]float64{56, 74}, [2]float64{55.65, 72.85})
	return points
}()

func cursorStateImage(scale float64, pressed, held bool) *image.RGBA {
	scale = max(1, min(4, scale))
	size := int(math.Round(cursorSize * scale))
	im := image.NewRGBA(image.Rect(0, 0, size, size))
	factor := float64(1)
	if held {
		factor = .97
	} else if pressed {
		factor = .86
	}
	for y := range size {
		for x := range size {
			var channels [4]float64
			for sy := range 4 {
				for sx := range 4 {
					px, py := (float64(x)+float64(sx)/4+.125)/scale, (float64(y)+float64(sy)/4+.125)/scale
					var sample [4]float64
					over := func(r, g, b, a float64) {
						sample = [4]float64{r*a + sample[0]*(1-a), g*a + sample[1]*(1-a), b*a + sample[2]*(1-a), a + sample[3]*(1-a)}
					}
					for _, glow := range [][5]float64{{53, 61, 255, 90, 129}, {66, 59, 74, 239, 255}, {57, 72, 255, 196, 82}} {
						d := math.Hypot((px-glow[0])/24, (py-glow[1])/22)
						if d < 1 {
							a := .07 * (1 - d) / .45
							if d < .55 {
								a = .16 - .09*d/.55
							}
							over(glow[2]/255, glow[3]/255, glow[4]/255, a)
						}
					}
					if pressed && math.Abs(math.Hypot(px-60, py-61)-21) < .9 {
						over(230.0/255, 242.0/255, 250.0/255, .75)
					}
					bx, by := 48+(px-48)/factor, 48+(py-48)/factor
					if bx >= 44 && bx <= 76 && by >= 44 && by <= 79 {
						inside, distance := cursorPolygon(bx, by, cursorBodyPoints)
						shadowInside, shadowDistance := cursorPolygon(bx, by-1.2, cursorBodyPoints)
						if shadowInside {
							shadowDistance = 0
						}
						over(0, 0, 0, .24*math.Exp(-shadowDistance*shadowDistance/(2*1.1*1.1)))
						outline := .75
						if held {
							outline = .85
						}
						if distance <= outline {
							if held {
								over(244.0/255, 248.0/255, 251.0/255, 1)
							} else {
								over(230.0/255, 242.0/255, 250.0/255, 1)
							}
						} else if inside {
							t := min(1, max(0, (by-48)/26))
							if held {
								over(67.0/255, 86.0/255, 97.0/255, 1)
							} else {
								over((51-14*t)/255, (66-18*t)/255, (78-19*t)/255, 1)
							}
						}
					}
					for i := range sample {
						channels[i] += sample[i]
					}
				}
			}
			im.SetRGBA(x, y, color.RGBA{uint8(channels[0] * 255 / 16), uint8(channels[1] * 255 / 16), uint8(channels[2] * 255 / 16), uint8(channels[3] * 255 / 16)})
		}
	}
	return im
}

func cursorPolygon(x, y float64, points [][2]float64) (bool, float64) {
	inside, distance := false, math.Inf(1)
	for i, a := range points {
		b := points[(i+1)%len(points)]
		if (a[1] > y) != (b[1] > y) && x < (b[0]-a[0])*(y-a[1])/(b[1]-a[1])+a[0] {
			inside = !inside
		}
		dx, dy := b[0]-a[0], b[1]-a[1]
		t := max(0, min(1, ((x-a[0])*dx+(y-a[1])*dy)/(dx*dx+dy*dy)))
		distance = min(distance, math.Hypot(x-a[0]-t*dx, y-a[1]-t*dy))
	}
	return inside, distance
}
