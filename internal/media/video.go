// Package media extracts bounded, timestamped evidence from local video files.
package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const MaxVideoBytes int64 = 2 * 1024 * 1024 * 1024

func Supported(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".mkv", ".mov", ".webm", ".avi", ".m4v", ".mpeg", ".mpg":
		return true
	}
	return false
}

type Probe struct {
	Duration float64 `json:"duration_seconds"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	HasAudio bool    `json:"has_audio"`
}

type Options struct {
	Start     float64
	End       float64
	Frames    int
	Audio     bool
	AudioOnly bool
}

type Result struct {
	Probe      Probe     `json:"video"`
	Start      float64   `json:"start_seconds"`
	End        float64   `json:"end_seconds"`
	Timestamps []float64 `json:"requested_frame_timestamps_seconds"`
	Image      []byte    `json:"-"`
	AudioPath  string    `json:"-"`
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("media subprocess output exceeds its limit")
	}
	return b.Buffer.Write(p)
}

func run(ctx context.Context, name string, limit int, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	hideWindow(cmd)
	out := &boundedBuffer{limit: limit}
	cmd.Stdout = out
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%s failed; install ffmpeg and ffprobe on PATH and verify the media file", name)
	}
	return out.Bytes(), nil
}

func Inspect(ctx context.Context, path string) (Probe, error) {
	var result Probe
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return result, errors.New("video inspection requires a local file, not a network share")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return result, err
	}
	st, err := os.Stat(path)
	if err != nil {
		return result, err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > MaxVideoBytes || !Supported(path) {
		return result, errors.New("video must be a supported local regular file below 2 GB")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	data, err := run(ctx, "ffprobe", 128*1024, "-v", "error", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,avi,mpeg,mpegvideo", "-show_entries", "format=duration:stream=codec_type,width,height,duration", "-of", "json", path)
	if err != nil {
		return result, err
	}
	var probe struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Type     string `json:"codec_type"`
			Width    int    `json:"width"`
			Height   int    `json:"height"`
			Duration string `json:"duration"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return result, errors.New("invalid ffprobe response")
	}
	result.Duration, _ = strconv.ParseFloat(probe.Format.Duration, 64)
	for _, s := range probe.Streams {
		if s.Type == "video" && result.Width == 0 {
			result.Width, result.Height = s.Width, s.Height
			if result.Duration == 0 {
				result.Duration, _ = strconv.ParseFloat(s.Duration, 64)
			}
		}
		if s.Type == "audio" {
			result.HasAudio = true
		}
	}
	if result.Width <= 0 || result.Height <= 0 || result.Width > 16384 || result.Height > 16384 || !finite(result.Duration) || result.Duration <= 0 {
		return result, errors.New("video dimensions or duration are unsupported")
	}
	return result, nil
}

func finite(n float64) bool { return !math.IsNaN(n) && !math.IsInf(n, 0) }

// Sample produces a contact sheet with labels, plus optional bounded WAV audio.
// dir must be a private temporary directory owned and removed by the caller.
func Sample(ctx context.Context, path, dir string, o Options) (Result, error) {
	var result Result
	if !finite(o.Start) || !finite(o.End) || o.Start < 0 || o.End < 0 || o.Frames < 0 || o.Frames > 12 {
		return result, errors.New("invalid video range or frame count (maximum 12)")
	}
	probe, err := Inspect(ctx, path)
	if err != nil {
		return result, err
	}
	if o.Start >= probe.Duration {
		return result, errors.New("video start is beyond the duration")
	}
	if o.End == 0 {
		o.End = math.Min(probe.Duration, o.Start+180)
	}
	if o.End <= o.Start || o.End > probe.Duration || o.End-o.Start > 180 {
		return result, errors.New("video window must be inside the video and at most 180 seconds; inspect longer videos in successive windows")
	}
	if o.Frames == 0 {
		o.Frames = 6
	}
	if o.AudioOnly {
		o.Frames = 0
	}
	result.Probe, result.Start, result.End = probe, o.Start, o.End
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	const w, h, label = 640, 360, 24
	sheet := image.NewRGBA(image.Rect(0, 0, 2*w, ((o.Frames+1)/2)*(h+label)))
	draw.Draw(sheet, sheet.Bounds(), image.NewUniform(color.Black), image.Point{}, draw.Src)
	for i := range o.Frames {
		stamp := o.Start + (float64(i)+0.5)*(o.End-o.Start)/float64(o.Frames)
		data, err := run(ctx, "ffmpeg", 2*1024*1024, "-nostdin", "-v", "error", "-threads", "1", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,avi,mpeg,mpegvideo", "-ss", strconv.FormatFloat(stamp, 'f', 3, 64), "-i", path, "-map", "0:v:0", "-frames:v", "1", "-vf", "scale=640:360:force_original_aspect_ratio=decrease,pad=640:360:(ow-iw)/2:(oh-ih)/2", "-f", "image2pipe", "-c:v", "mjpeg", "-q:v", "4", "pipe:1")
		if err != nil {
			return Result{}, err
		}
		frame, err := jpeg.Decode(bytes.NewReader(data))
		if err != nil {
			return Result{}, errors.New("unable to decode a sampled video frame")
		}
		x, y := (i%2)*w, (i/2)*(h+label)
		draw.Draw(sheet, image.Rect(x, y, x+w, y+h), frame, frame.Bounds().Min, draw.Src)
		d := font.Drawer{Dst: sheet, Src: image.NewUniform(color.White), Face: basicfont.Face7x13, Dot: fixed.P(x+8, y+h+17)}
		d.DrawString(fmt.Sprintf("Frame %d | ~%.3f seconds", i+1, stamp))
		result.Timestamps = append(result.Timestamps, stamp)
	}
	var encoded bytes.Buffer
	if o.Frames > 0 {
		if err := jpeg.Encode(&encoded, sheet, &jpeg.Options{Quality: 80}); err != nil {
			return Result{}, err
		}
	}
	result.Image = encoded.Bytes()
	if o.Audio && probe.HasAudio {
		result.AudioPath = filepath.Join(dir, "audio.wav")
		_, err := run(ctx, "ffmpeg", 1024, "-nostdin", "-v", "error", "-y", "-threads", "1", "-protocol_whitelist", "file,pipe", "-format_whitelist", "mov,matroska,webm,avi,mpeg,mpegvideo", "-ss", strconv.FormatFloat(o.Start, 'f', 3, 64), "-i", path, "-t", strconv.FormatFloat(o.End-o.Start, 'f', 3, 64), "-map", "0:a:0", "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-fs", "24000000", result.AudioPath)
		if err != nil {
			return Result{}, err
		}
	}
	return result, nil
}

// ReadBounded rejects files that grow past the limit while being read.
func ReadBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file exceeds the attachment size limit")
	}
	return data, nil
}
