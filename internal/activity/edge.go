package activity

import (
	"math"
	"time"
)

const (
	edgeDepth     = 112
	edgeRadius    = 18
	edgeCycle     = 6 * time.Second
	edgeFrame     = time.Second / 60
	edgeSamples   = 4096
	edgePrecision = 16
)

type edgeRect struct{ X, Y, W, H int }

// Edge strips share corners without overlapping their alpha surfaces.
func edgeBounds(x, y, width, height int, scale float64) [4]edgeRect {
	d := max(1, min(int(math.Round(edgeDepth*scale)), min(width, height)/2))
	return [4]edgeRect{
		{x, y, width, d},
		{x + width - d, y + d, d, max(1, height-2*d)},
		{x, y + height - d, width, d},
		{x, y + d, d, max(1, height-2*d)},
	}
}

type edgePixels struct {
	width, height    int
	alpha            []byte
	coordinate       []float64
	indices          []uint16
	colors           [][3]byte
	density          []uint16
	coverage         []byte
	hazeX, hazeY     []byte
	depthX, depthY   []uint16
	weightX, weightY []byte
	rim              []byte
	cloudLift        []int
	ribbonLift       []int
	cloud            [edgeDepth*edgePrecision + 1]byte
	ribbon           [edgeDepth*edgePrecision + 1]byte
}

func newEdgePixels(width, height int, scale float64, side int) *edgePixels {
	b := edgeBounds(0, 0, width, height, scale)[side]
	s := &edgePixels{width: b.W, height: b.H, alpha: make([]byte, b.W*b.H), indices: make([]uint16, b.W*b.H)}
	length := edgeSamples
	s.coordinate = make([]float64, length)
	s.colors = make([][3]byte, length)
	s.density = make([]uint16, length)
	s.coverage = make([]byte, b.W*b.H)
	s.hazeX, s.hazeY = make([]byte, b.W*b.H), make([]byte, b.W*b.H)
	s.depthX, s.depthY = make([]uint16, b.W*b.H), make([]uint16, b.W*b.H)
	s.weightX, s.weightY = make([]byte, b.W*b.H), make([]byte, b.W*b.H)
	s.rim = make([]byte, b.W*b.H)
	s.cloudLift = make([]int, length)
	s.ribbonLift = make([]int, length)
	for i := range s.cloud {
		distance := float64(i) / edgePrecision
		s.cloud[i] = byte(math.Round(110 * math.Exp(-distance*distance/(2*18*18))))
		s.ribbon[i] = byte(math.Round(64 * math.Exp(-distance*distance/(2*10*10))))
	}
	perimeter := float64(2 * (width + height))
	for i := range length {
		s.coordinate[i] = float64(i) / edgeSamples
	}
	for y := range b.H {
		for x := range b.W {
			gx, gy := b.X+x, b.Y+y
			cornerX := float64(min(b.X+x, width-1-b.X-x)) / scale
			cornerY := float64(min(b.Y+y, height-1-b.Y-y)) / scale
			horizontal, vertical := float64(gx), float64(width+gy)
			if gy >= height/2 {
				horizontal = float64(width + height + width - 1 - gx)
			}
			if gx < width/2 {
				vertical = perimeter - float64(gy) - 1
			}
			if math.Abs(horizontal-vertical) > perimeter/2 {
				if horizontal < vertical {
					horizontal += perimeter
				} else {
					vertical += perimeter
				}
			}
			mix := max(0, min(1, 0.5+(cornerY-cornerX)/40))
			mix = mix * mix * (3 - 2*mix)
			position := math.Mod(horizontal*(1-mix)+vertical*mix, perimeter)
			s.indices[y*b.W+x] = uint16(min(edgeSamples-1, int(position*edgeSamples/perimeter)))
			radius := min(float64(edgeRadius), float64(min(width, height))/(2*scale))
			depth := min(cornerX, cornerY)
			if cornerX < radius && cornerY < radius {
				depth = radius - math.Hypot(radius-cornerX, radius-cornerY)
			}
			coverage := max(0, min(1, depth*scale+0.5))
			if coverage == 0 {
				continue
			}
			depth = max(0, depth)
			i := y*b.W + x
			xFeather, yFeather := edgeFeather(cornerX), edgeFeather(cornerY)
			s.hazeX[i] = byte(math.Round(112 * math.Exp(-math.Pow(cornerX/46, 1.35)) * xFeather))
			s.hazeY[i] = byte(math.Round(112 * math.Exp(-math.Pow(cornerY/46, 1.35)) * yFeather))
			s.depthX[i] = uint16(math.Round(min(edgeDepth, cornerX) * edgePrecision))
			s.depthY[i] = uint16(math.Round(min(edgeDepth, cornerY) * edgePrecision))
			s.weightX[i], s.weightY[i] = byte(math.Round(255*xFeather)), byte(math.Round(255*yFeather))
			s.rim[i] = byte(min(240, math.Round(226*math.Exp(-depth/1.1))))
			s.coverage[i] = byte(math.Round(255 * coverage))
			s.alpha[i] = byte(min(uint16(255), uint16(s.rim[i])+fogUnion(uint16(s.hazeX[i]), uint16(s.hazeY[i]))) * uint16(s.coverage[i]) / 255)
		}
	}
	return s
}

// Paint writes premultiplied BGRA directly into a reusable native DIB.
func (s *edgePixels) paint(pixels []byte, elapsed time.Duration) {
	phase := float64(elapsed%edgeCycle) / float64(edgeCycle)
	for i, u := range s.coordinate {
		s.colors[i] = edgeColor(u - phase)
		s.density[i] = uint16(math.Round(256 * (0.82 + 0.18*math.Sin((u*5-phase)*2*math.Pi))))
		s.cloudLift[i] = int(math.Round(edgePrecision * (26 + 12*math.Sin((u*6-phase)*2*math.Pi))))
		s.ribbonLift[i] = int(math.Round(edgePrecision * (48 + 15*math.Sin((u*9+phase)*2*math.Pi))))
	}
	for y := range s.height {
		for x := range s.width {
			i := y*s.width + x
			along := s.indices[i]
			x := min(uint16(255), uint16(s.hazeX[i])+s.smoke(s.depthX[i], s.weightX[i], along))
			y := min(uint16(255), uint16(s.hazeY[i])+s.smoke(s.depthY[i], s.weightY[i], along))
			c, a := s.colors[along], min(uint16(255), uint16(s.rim[i])+fogUnion(x, y))*uint16(s.coverage[i])/255
			j := i * 4
			pixels[j], pixels[j+1], pixels[j+2], pixels[j+3] = byte(uint16(c[2])*a/255), byte(uint16(c[1])*a/255), byte(uint16(c[0])*a/255), byte(a)
		}
	}
}

func edgeFeather(depth float64) float64 {
	return math.Pow(max(0, 1-depth/edgeDepth), 1.25)
}

// Independent edge fog blends as opacity instead of creating a mitered corner.
func fogUnion(x, y uint16) uint16 { return x + y - (x*y+127)/255 }

func (s *edgePixels) smoke(depth uint16, weight byte, along uint16) uint16 {
	cloud := s.cloud[min(edgeDepth*edgePrecision, absEdge(int(depth)-s.cloudLift[along]))]
	ribbon := s.ribbon[min(edgeDepth*edgePrecision, absEdge(int(depth)-s.ribbonLift[along]))]
	return uint16(((uint32(cloud)+uint32(ribbon))*uint32(weight)*uint32(s.density[along]) + 32640) / 65280)
}

func absEdge(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func edgeColor(hue float64) [3]byte {
	palette := [...][3]byte{{255, 90, 129}, {255, 196, 82}, {128, 255, 154}, {74, 239, 255}, {122, 147, 255}, {231, 111, 255}, {255, 90, 129}}
	hue = (hue - math.Floor(hue)) * 6
	i, mix := int(hue), hue-math.Floor(hue)
	mix = mix * mix * (3 - 2*mix)
	var color [3]byte
	for channel := range color {
		color[channel] = byte(math.Round(float64(palette[i][channel])*(1-mix) + float64(palette[i+1][channel])*mix))
	}
	return color
}
