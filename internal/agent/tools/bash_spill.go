package tools

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Long command output is mostly noise around a few lines that matter, and
// returning it whole fills the context window, which brings compaction
// sooner and buries the failure the model needs to see. Output above
// spillThreshold is saved in full to a log file under the project's .atlas
// directory; the model gets the head, the lines that look like errors, the
// tail, and the file's path to grep or view for anything else.

// SpilledOutputMarker begins the note that says where full output went.
const SpilledOutputMarker = "[Full output saved to "

const (
	// spillThreshold is the combined stdout and stderr size, in bytes,
	// above which output is saved to a file.
	spillThreshold = 12 << 10
	spillHeadLines = 25
	spillTailLines = 50
	// spillSignalLines bounds the error-like lines quoted from the middle.
	spillSignalLines = 40
	spillLineWidth   = 400
	// spillKeepFiles bounds how many logs are kept per project.
	spillKeepFiles = 50
	spillLogDir    = ".atlas/logs"
)

// signalLine matches lines that usually carry the reason for a failure.
var signalLine = regexp.MustCompile(`(?i)(\berror\b|\berr:|\bfail(ed|ure|ing)?\b|\bpanic\b|\bexception\b|\bfatal\b|\btraceback\b|\bassert(ion)?\b|\bundefined\b|\bnot found\b|\bdenied\b|^\s*--- FAIL|^\s*E\s{2,}|\bwarning\b)`)

// spillOutput saves stdout and stderr in full when together they exceed the
// threshold and returns digests to show in their place. It returns the
// inputs unchanged when they are small, when root is empty, or when the log
// cannot be written: losing the full output must never lose the command's
// result.
func spillOutput(root, stdout, stderr string) (string, string) {
	if root == "" || len(stdout)+len(stderr) <= spillThreshold {
		return stdout, stderr
	}
	dir := filepath.Join(root, filepath.FromSlash(spillLogDir))
	path, err := writeSpill(dir, stdout, stderr)
	if err != nil {
		return stdout, stderr
	}
	pruneSpills(dir)
	rel := filepath.ToSlash(filepath.Join(spillLogDir, filepath.Base(path)))
	lines := strings.Count(stdout, "\n") + strings.Count(stderr, "\n")
	header := fmt.Sprintf("%s%s (%d lines, %d KiB). The head, error-like lines and tail follow; grep or view the file for the rest.]\n",
		SpilledOutputMarker, rel, lines, (len(stdout)+len(stderr))>>10)
	digest := header + digestLines(stdout)
	if strings.TrimSpace(stderr) != "" {
		return digest, digestLines(stderr)
	}
	return digest, ""
}

func writeSpill(dir, stdout, stderr string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	name := "cmd-" + time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(suffix) + ".log"
	path := filepath.Join(dir, name)
	content := stdout
	if stderr != "" {
		content += "\n--- stderr ---\n" + stderr
	}
	return path, os.WriteFile(path, []byte(content), 0o644)
}

// pruneSpills deletes the oldest logs beyond spillKeepFiles. Names start
// with a timestamp, so name order is age order.
func pruneSpills(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var logs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "cmd-") && strings.HasSuffix(e.Name(), ".log") {
			logs = append(logs, e.Name())
		}
	}
	slices.Sort(logs)
	for _, name := range logs[:max(0, len(logs)-spillKeepFiles)] {
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// digestLines keeps the first and last lines and, from the middle, the
// lines that look like errors, each with its line number so the model can
// view the surrounding context in the log.
func digestLines(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) <= spillHeadLines+spillTailLines {
		return clipLines(lines)
	}
	var b strings.Builder
	b.WriteString(clipLines(lines[:spillHeadLines]))
	middle := lines[spillHeadLines : len(lines)-spillTailLines]
	var signals []string
	for i, line := range middle {
		if signalLine.MatchString(line) {
			signals = append(signals, fmt.Sprintf("%6d| %s", spillHeadLines+i+1, clipLine(line)))
		}
	}
	omitted := len(middle)
	if len(signals) > 0 {
		shown := signals[:min(len(signals), spillSignalLines)]
		fmt.Fprintf(&b, "\n... error-like lines from the %d omitted (%d of %d shown):\n%s\n", omitted, len(shown), len(signals), strings.Join(shown, "\n"))
	}
	fmt.Fprintf(&b, "\n... [%d lines omitted] ...\n", omitted)
	b.WriteString(clipLines(lines[len(lines)-spillTailLines:]))
	return b.String()
}

func clipLines(lines []string) string {
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = clipLine(line)
	}
	return strings.Join(out, "\n")
}

func clipLine(line string) string {
	if len(line) <= spillLineWidth {
		return line
	}
	cut := spillLineWidth
	for cut > 0 && !utf8.RuneStart(line[cut]) {
		cut--
	}
	return line[:cut] + "…"
}
