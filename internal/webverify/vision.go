package webverify

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Shot is one screenshot kept for the visual review.
type Shot struct {
	Viewport  string
	Page      string
	Path      string
	Data      []byte
	MediaType string
}

// VisionReviewer asks a vision-capable model to look at the screenshots and
// returns its raw reply, in the format VisionSystemPrompt describes.
type VisionReviewer interface {
	Review(ctx context.Context, shots []Shot) (string, error)
}

// VisionSystemPrompt instructs the vision model. It asks only for defects
// anyone can see, because a finding that blocks the work must survive being
// looked at again.
const VisionSystemPrompt = `You review screenshots of a web page rendered at several viewport sizes. Report only visible layout defects:
- text or controls that overlap, are cut off, overflow their container or are unreadable against the background
- content wider than the screen, or a horizontal scrollbar
- a blank or mostly empty area where content clearly belongs, or a page that did not render
- images or icons that are broken, stretched or missing
- controls too small or too close together to use on a phone

Do not comment on taste, color preference, copy or originality. Do not report anything you cannot point to in a specific screenshot. Name the screenshot by its viewport label.

Reply in exactly this format and nothing else:
VERDICT: PASS
or
VERDICT: FAIL
- [high|medium|low] <viewport> | <what is wrong and where in the image>
Use high only for a defect that makes the page unusable or clearly broken.`

// VisionPrompt lists the attached screenshots in order.
func VisionPrompt(shots []Shot) string {
	var b strings.Builder
	b.WriteString("Screenshots, in the order attached:\n")
	for i, s := range shots {
		fmt.Fprintf(&b, "%d. viewport %q, page %s\n", i+1, s.Viewport, s.Page)
	}
	return b.String()
}

var visionLine = regexp.MustCompile(`^[-*]\s*\[(high|medium|low)\]\s*([^|]+?)\s*\|\s*(.+)$`)

// ParseVisionReply turns the reviewer's reply into findings. Each is tied to
// the screenshot of the viewport it names; a line naming an unknown viewport
// becomes an advisory finding without evidence, never a blocking one.
func ParseVisionReply(reply string, shots []Shot) []Finding {
	lines := strings.Split(strings.TrimSpace(reply), "\n")
	failed := false
	var findings []Finding
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(strings.ToUpper(line), "VERDICT:"); ok {
			failed = strings.TrimSpace(rest) == "FAIL"
			continue
		}
		m := visionLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		viewport := strings.Trim(strings.ToLower(m[2]), `"' `)
		f := Finding{
			Kind: KindVisual, Source: SourceVision, Severity: ParseSeverity(m[1], SeverityLow),
			Title: "Visual defect: " + clip(m[3], 140), Detail: m[3],
		}
		var matched *Shot
		for i := range shots {
			if strings.EqualFold(shots[i].Viewport, viewport) {
				matched = &shots[i]
				break
			}
		}
		if matched == nil {
			f.Advisory = true
			f.Detail += " (the reviewer named no known screenshot, so this is unconfirmed)"
			f.Evidence.Error = "no screenshot matched viewport " + viewport
		} else {
			f.Page, f.Viewports = matched.Page, []string{matched.Viewport}
			f.Evidence.Screenshot = matched.Path
			if matched.Path == "" {
				// The image could not be kept, so the finding cannot be
				// checked by a reader.
				f.Advisory = true
			}
		}
		findings = append(findings, f)
	}
	if !failed && len(findings) > 0 {
		// Findings listed under a PASS verdict contradict it; keep them
		// as notes only.
		for i := range findings {
			findings[i].Advisory = true
		}
	}
	return findings
}
