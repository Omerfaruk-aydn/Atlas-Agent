package notification

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync"
)

// Kind tells which event a notification announces, so each one can sound
// different.
type Kind string

const (
	KindGeneric    Kind = ""
	KindPermission Kind = "permission"
	KindQuestion   Kind = "question"
	KindFinished   Kind = "finished"
)

// Kinds lists the events that have their own sound.
var Kinds = []Kind{KindPermission, KindQuestion, KindFinished}

const soundRate = 44100

// partial is one overtone of a struck tone: its frequency ratio, level and
// how fast it dies away.
type partial struct {
	ratio, level, decay float64
}

// note is one struck tone inside a sound.
type note struct {
	at, freq, level, decay float64
}

// bell rings brightly and long; it asks for attention.
var bell = []partial{{1, 1, 1}, {2, .32, 1.6}, {3.01, .14, 2.4}, {4.18, .07, 3.5}}

// marimba is a soft mallet tone with a quick wooden overtone.
var marimba = []partial{{1, 1, 1}, {3.99, .22, 6}, {9.9, .05, 14}}

// chime is warm and round, for a calm ending.
var chime = []partial{{1, 1, 1}, {2, .18, 1.8}, {2.76, .06, 2.6}}

// designs are Atlas's own sounds. Permission rises a fifth twice: a firm
// "decide, please". Question climbs a gentle arpeggio that ends open, like a
// question. Finished falls and settles on a chord: done, nothing to do.
var designs = map[Kind]struct {
	timbre []partial
	notes  []note
}{
	KindPermission: {bell, []note{
		{0, 880, .9, .16},
		{.11, 1318.5, 1, .22},
		{.34, 880, .55, .14},
		{.43, 1318.5, .6, .3},
	}},
	KindQuestion: {marimba, []note{
		{0, 523.25, .8, .18}, {.09, 659.25, .85, .18}, {.18, 783.99, .9, .2}, {.29, 987.77, 1, .42},
	}},
	KindFinished: {chime, []note{
		{0, 1318.5, .7, .22},
		{.13, 1046.5, .75, .26},
		{.27, 783.99, .8, .55},
		{.27, 1046.5, .55, .55},
		{.27, 1318.5, .35, .55},
	}},
}

var (
	soundsOnce sync.Once
	sounds     map[Kind][]byte
)

// BuiltinSound returns Atlas's own sound for kind as a WAV file. Generic
// notifications use the finished sound.
func BuiltinSound(kind Kind) []byte {
	soundsOnce.Do(func() {
		sounds = make(map[Kind][]byte, len(designs))
		for k, d := range designs {
			sounds[k] = wav(synthesize(d.timbre, d.notes))
		}
	})
	if s, ok := sounds[kind]; ok {
		return s
	}
	return sounds[KindFinished]
}

// synthesize renders struck notes to samples in [-1, 1].
func synthesize(timbre []partial, notes []note) []float64 {
	end := 0.0
	for _, n := range notes {
		end = max(end, n.at+n.decay*4)
	}
	samples := make([]float64, int(end*soundRate))
	const attack = .004
	for _, n := range notes {
		start := int(n.at * soundRate)
		for i := start; i < len(samples); i++ {
			t := float64(i-start) / soundRate
			envelope := min(1, t/attack)
			v := 0.0
			for _, p := range timbre {
				v += p.level * math.Exp(-t*p.decay/n.decay) * math.Sin(2*math.Pi*n.freq*p.ratio*t)
			}
			samples[i] += n.level * envelope * v
		}
	}
	peak := 0.0
	for _, s := range samples {
		peak = max(peak, math.Abs(s))
	}
	// A short fade keeps the end free of clicks.
	fade := soundRate / 50
	for i := range samples {
		samples[i] = samples[i] / max(peak, 1e-9) * .5
		if left := len(samples) - i; left < fade {
			samples[i] *= float64(left) / float64(fade)
		}
	}
	return samples
}

// wav encodes mono 16-bit PCM.
func wav(samples []float64) []byte {
	var b bytes.Buffer
	size := uint32(len(samples) * 2)
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, 36+size)
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(soundRate), uint32(soundRate * 2), uint16(2), uint16(16)} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, size)
	for _, s := range samples {
		_ = binary.Write(&b, binary.LittleEndian, int16(math.Round(max(-1, min(1, s))*32767)))
	}
	return b.Bytes()
}
