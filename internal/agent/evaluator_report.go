package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

type reviewFinding struct {
	Severity string `json:"severity" description:"Finding severity." enum:"low,medium,high,critical"`
	File     string `json:"file" description:"Project-relative file path, or empty for a general finding."`
	Line     int    `json:"line" description:"One-based source line, or zero for a general finding."`
	Summary  string `json:"summary" description:"Concrete defect requiring correction or evidence-based refutation."`
	Evidence string `json:"evidence" description:"Observed command output or source reference supporting the finding."`
}

type structuredReview struct {
	Verdict  string          `json:"verdict" description:"Pass only with no findings; fail requires actionable findings." enum:"pass,fail"`
	Findings []reviewFinding `json:"findings" description:"Evidence-backed defects found during independent review."`
}

const submitReviewToolName = "submit_review"

const structuredReviewFormat = `Finish the review by calling submit_review with this exact verdict contract:
{"verdict":"pass|fail","findings":[{"severity":"low|medium|high|critical","file":"project-relative path or empty for a general finding","line":1,"summary":"concrete defect","evidence":"observed command output or source reference"}]}
A pass requires an empty findings array. A fail requires at least one finding with nonempty summary and evidence. Use line 0 for a general finding. Do not wrap the verdict in Markdown.`

// newSubmitReviewTool gives the provider a typed tool schema instead of
// relying on prose alone. The local decoder remains the final authority.
func newSubmitReviewTool() fantasy.AgentTool {
	return fantasy.NewAgentTool(submitReviewToolName, "Submit the final independent review verdict and evidence-backed findings.", func(ctx context.Context, review structuredReview, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		data, err := json.Marshal(review)
		if err != nil {
			return fantasy.NewTextErrorResponse("Could not encode review verdict"), nil
		}
		if _, err := decodeReview(string(data)); err != nil {
			return fantasy.NewTextErrorResponse("Invalid review verdict: " + err.Error()), nil
		}
		return fantasy.NewTextResponse(string(data)), nil
	})
}

func decodeReview(text string) (structuredReview, error) {
	var r structuredReview
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return r, fmt.Errorf("unexpected trailing review data")
	}
	if (r.Verdict != "pass" && r.Verdict != "fail") || r.Findings == nil || (r.Verdict == "pass") != (len(r.Findings) == 0) || len(r.Findings) > 32 {
		return r, fmt.Errorf("invalid verdict or findings")
	}
	for _, f := range r.Findings {
		if f.Line < 0 || strings.TrimSpace(f.Summary) == "" || strings.TrimSpace(f.Evidence) == "" || len(f.Summary) > 6000 || len(f.Evidence) > 6000 || !strings.Contains("|low|medium|high|critical|", "|"+f.Severity+"|") {
			return r, fmt.Errorf("incomplete finding")
		}
	}
	return r, nil
}

func (r structuredReview) findingsText() string {
	var out []string
	for _, f := range r.Findings {
		out = append(out, fmt.Sprintf("- [%s] %s:%d %s; evidence: %s", f.Severity, f.File, f.Line, f.Summary, f.Evidence))
	}
	return strings.Join(out, "\n")
}
