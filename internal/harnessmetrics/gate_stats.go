package harnessmetrics

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LoadGateEvents reads bounded retained gate metadata for local stats.
func LoadGateEvents(ctx context.Context, root string, since time.Time) ([]TraceEvent, error) {
	dir := filepath.Join(root, Dir, "traces")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(entries) > 4096 {
		return nil, fmt.Errorf("trace stats exceed 4096 retained files; archive old sessions")
	}
	out := []TraceEvent{}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") && !strings.HasSuffix(entry.Name(), ".jsonl.1") {
			continue
		}
		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, err
			}
			var event TraceEvent
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				_ = file.Close()
				return nil, fmt.Errorf("invalid retained gate trace: %w", err)
			}
			if event.Kind == "gate" && event.Time >= since.UnixMilli() {
				out = append(out, event)
			}
			if len(out) > 100000 {
				_ = file.Close()
				return nil, fmt.Errorf("trace stats exceed 100000 gate events; narrow --days")
			}
		}
		err = errors.Join(scanner.Err(), file.Close())
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func FormatGates(gates []GateSummary) string {
	if len(gates) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nRetained gate observations (bounded history):\n")
	for _, g := range gates {
		fmt.Fprintf(&b, "  %s triggered=%d pass=%d sent_back=%d fail_open=%d unverified=%d failed=%d skipped=%d tokens=%d duration_ms=%d cost=%.6f\n", g.Name, g.Triggered, g.Passed, g.SentBack, g.FailOpen, g.Unverified, g.Failed, g.Skipped, g.Tokens, g.DurationMS, g.Cost)
	}
	return b.String()
}
