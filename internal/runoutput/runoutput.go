// Package runoutput defines the stable machine-readable run contract.
package runoutput

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const (
	ExitSuccess    = 0
	ExitUnverified = 2
	ExitBlocked    = 3
	ExitFailure    = 1
)

// ExitError classifies failures for unattended callers without parsing text.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// StreamWriter writes exactly one JSON object per line. HTML, terminal color
// and terminal control sequences never belong in this stream.
type StreamWriter struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

func NewStreamWriter(output io.Writer) *StreamWriter {
	return &StreamWriter{encoder: json.NewEncoder(output)}
}

type deltaWriter struct{ events *StreamWriter }

func (w deltaWriter) Write(data []byte) (int, error) {
	if err := w.events.Event("message_delta", map[string]any{"delta": string(data)}); err != nil {
		return 0, err
	}
	return len(data), nil
}

// DeltaWriter converts assistant text into JSONL events, never raw text.
func (w *StreamWriter) DeltaWriter() io.Writer { return deltaWriter{events: w} }

func (w *StreamWriter) Event(kind string, fields map[string]any) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	event := make(map[string]any, len(fields)+1)
	event["type"] = kind
	for key, value := range fields {
		event[key] = value
	}
	if err := w.encoder.Encode(event); err != nil {
		return fmt.Errorf("write stream-json event: %w", err)
	}
	return nil
}
