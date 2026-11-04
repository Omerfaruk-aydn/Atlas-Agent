package voice

import (
	"encoding/binary"
	"fmt"
)

// Encoding names a raw audio format: codec plus sample rate, always mono.
type Encoding string

// Encodings. Telephone networks carry G.711 mu-law at 8 kHz; most speech
// services prefer 16-bit little-endian PCM at 16 kHz.
const (
	EncodingMulaw8k  Encoding = "ulaw_8000"
	EncodingPCM8k    Encoding = "pcm_8000"
	EncodingPCM16k   Encoding = "pcm_16000"
	EncodingPCM24k   Encoding = "pcm_24000"
	maxFrameDuration          = 2000 // milliseconds
)

// SampleRate returns the encoding's sample rate in Hz, or 0 if unknown.
func (e Encoding) SampleRate() int {
	switch e {
	case EncodingMulaw8k, EncodingPCM8k:
		return 8000
	case EncodingPCM16k:
		return 16000
	case EncodingPCM24k:
		return 24000
	}
	return 0
}

// BytesPerSample returns 1 for mu-law and 2 for 16-bit PCM.
func (e Encoding) BytesPerSample() int {
	if e == EncodingMulaw8k {
		return 1
	}
	return 2
}

// Valid reports whether e is a known encoding.
func (e Encoding) Valid() bool { return e.SampleRate() > 0 }

// MaxFrameBytes is the largest frame accepted for e (two seconds).
func (e Encoding) MaxFrameBytes() int {
	return e.SampleRate() * e.BytesPerSample() * maxFrameDuration / 1000
}

// AudioFrame is one chunk of mono audio. TurnID is set on synthesized audio
// so a frame that belongs to an abandoned turn can be dropped.
type AudioFrame struct {
	Encoding Encoding
	Data     []byte
	TurnID   string
}

// Validate checks the frame size and alignment.
func (f AudioFrame) Validate() error {
	if !f.Encoding.Valid() {
		return fmt.Errorf("unknown audio encoding %q", f.Encoding)
	}
	if len(f.Data) > f.Encoding.MaxFrameBytes() {
		return fmt.Errorf("audio frame of %d bytes exceeds the %d byte limit", len(f.Data), f.Encoding.MaxFrameBytes())
	}
	if len(f.Data)%f.Encoding.BytesPerSample() != 0 {
		return fmt.Errorf("audio frame is not sample aligned")
	}
	return nil
}

// Convert re-encodes a frame. Supported paths go through 16-bit PCM and a
// linear resampler; it is meant for speech, not music.
func Convert(f AudioFrame, to Encoding) (AudioFrame, error) {
	if f.Encoding == to {
		return f, nil
	}
	if err := f.Validate(); err != nil {
		return AudioFrame{}, err
	}
	if !to.Valid() {
		return AudioFrame{}, fmt.Errorf("unknown audio encoding %q", to)
	}
	samples := decodeSamples(f)
	samples = Resample(samples, f.Encoding.SampleRate(), to.SampleRate())
	return AudioFrame{Encoding: to, Data: encodeSamples(samples, to), TurnID: f.TurnID}, nil
}

func decodeSamples(f AudioFrame) []int16 {
	if f.Encoding == EncodingMulaw8k {
		out := make([]int16, len(f.Data))
		for i, b := range f.Data {
			out[i] = MulawDecode(b)
		}
		return out
	}
	out := make([]int16, len(f.Data)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(f.Data[i*2:]))
	}
	return out
}

func encodeSamples(s []int16, e Encoding) []byte {
	if e == EncodingMulaw8k {
		out := make([]byte, len(s))
		for i, v := range s {
			out[i] = MulawEncode(v)
		}
		return out
	}
	out := make([]byte, len(s)*2)
	for i, v := range s {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out
}

// Resample converts samples between rates with linear interpolation.
func Resample(in []int16, from, to int) []int16 {
	if from == to || len(in) == 0 {
		return in
	}
	n := len(in) * to / from
	out := make([]int16, n)
	for i := range out {
		pos := float64(i) * float64(from) / float64(to)
		j := int(pos)
		frac := pos - float64(j)
		a := float64(in[min(j, len(in)-1)])
		b := float64(in[min(j+1, len(in)-1)])
		out[i] = int16(a + (b-a)*frac)
	}
	return out
}

const (
	mulawBias = 0x84
	mulawClip = 32635
)

// MulawEncode encodes one 16-bit sample as G.711 mu-law.
func MulawEncode(sample int16) byte {
	s := int(sample)
	sign := 0
	if s < 0 {
		s = -s
		sign = 0x80
	}
	if s > mulawClip {
		s = mulawClip
	}
	s += mulawBias
	exponent := 7
	for mask := 0x4000; s&mask == 0 && exponent > 0; mask >>= 1 {
		exponent--
	}
	mantissa := (s >> (exponent + 3)) & 0x0f
	return ^byte(sign | exponent<<4 | mantissa)
}

// MulawDecode decodes one G.711 mu-law byte.
func MulawDecode(b byte) int16 {
	b = ^b
	sign := b & 0x80
	exponent := int(b>>4) & 0x07
	mantissa := int(b & 0x0f)
	s := ((mantissa << 3) + mulawBias) << exponent
	s -= mulawBias
	if sign != 0 {
		return int16(-s)
	}
	return int16(s)
}
