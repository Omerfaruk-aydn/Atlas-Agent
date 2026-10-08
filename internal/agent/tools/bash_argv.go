package tools

import (
	"context"
	"strings"
	"sync"
	"time"
)

// argvCaptureLimit bounds what a literal argv run keeps in memory per
// stream: the first and last halves of this many bytes.
const argvCaptureLimit = 1 << 20

// ArgvStreams are the separately bounded streams of a literal argv run. They
// are only reported when a caller asks for them with WithArgvOptions.
type ArgvStreams struct {
	Stdout      string `json:"stdout"`
	Stderr      string `json:"stderr"`
	StdoutBytes int64  `json:"stdout_bytes"`
	StderrBytes int64  `json:"stderr_bytes"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// ArgvOptions let an internal caller of the bash tool, such as cli_bridge,
// request separate bounded streams and a time limit for a background argv
// job. They are carried in the context and are not part of the model-visible
// schema.
type ArgvOptions struct {
	StreamLimit int
	JobTimeout  time.Duration
}

type argvOptionsKey struct{}

// WithArgvOptions attaches ArgvOptions for nested bash argv calls.
func WithArgvOptions(ctx context.Context, opts ArgvOptions) context.Context {
	return context.WithValue(ctx, argvOptionsKey{}, opts)
}

func argvOptions(ctx context.Context) ArgvOptions {
	opts, _ := ctx.Value(argvOptionsKey{}).(ArgvOptions)
	return opts
}

// headTail keeps the beginning and end of a stream and counts every byte,
// so a program that prints gigabytes cannot exhaust memory.
type headTail struct {
	mu    sync.Mutex
	limit int
	head  []byte
	tail  []byte
	total int64
}

func newHeadTail(limit int) *headTail { return &headTail{limit: limit} }

func (h *headTail) Write(p []byte) (int, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.total += int64(len(p))
	half := h.limit / 2
	rest := p
	if room := half - len(h.head); room > 0 {
		n := min(room, len(rest))
		h.head = append(h.head, rest[:n]...)
		rest = rest[n:]
	}
	if len(rest) > 0 {
		h.tail = append(h.tail, rest...)
		if len(h.tail) > 2*half {
			h.tail = append(h.tail[:0:0], h.tail[len(h.tail)-half:]...)
		}
	}
	return len(p), nil
}

// String returns the retained text, marking any dropped middle.
func (h *headTail) String() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	half := h.limit / 2
	tail := h.tail
	if len(tail) > half {
		tail = tail[len(tail)-half:]
	}
	if int64(len(h.head)+len(tail)) < h.total {
		return strings.ToValidUTF8(string(h.head), "") + "\n…[output truncated]…\n" + strings.ToValidUTF8(string(tail), "")
	}
	return string(h.head) + string(tail)
}

// dropped reports that some of the stream was not retained.
func (h *headTail) dropped() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return int64(len(h.head)+min(len(h.tail), h.limit/2)) < h.total
}

func (h *headTail) Total() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.total
}

// boundStream keeps the head and tail of s within limit bytes.
func boundStream(s string, limit int) (string, bool) {
	if limit <= 0 || len(s) <= limit {
		return s, false
	}
	marker := "\n…[truncated]…\n"
	half := max(0, (limit-len(marker))/2)
	return strings.ToValidUTF8(s[:half], "") + marker + strings.ToValidUTF8(s[len(s)-half:], ""), true
}

func argvStreams(stdout, stderr *headTail, limit int) *ArgvStreams {
	out, a := boundStream(stdout.String(), limit)
	errText, b := boundStream(stderr.String(), limit)
	return &ArgvStreams{
		Stdout: out, Stderr: errText, StdoutBytes: stdout.Total(), StderrBytes: stderr.Total(),
		Truncated: a || b || stdout.dropped() || stderr.dropped(),
	}
}
