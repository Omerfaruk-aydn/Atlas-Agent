package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
)

// BrowserFlowParams defines or runs a reusable browser flow. A flow is a
// template of workflow steps whose string fields may contain {{input}}
// placeholders; items supply the inputs. Templates resolve targets
// semantically at run time, so they never store refs, viewport ids or
// coordinates.
type BrowserFlowParams struct {
	Name    string                `json:"name" description:"Flow name (letters, digits, - and _). Saved flows are reused by name."`
	Version string                `json:"version,omitempty" description:"major.minor. A saved flow runs only for a request with the same major version."`
	Inputs  []string              `json:"inputs,omitempty" description:"Input names every item must supply."`
	TabID   string                `json:"tab_id,omitempty"`
	Origin  string                `json:"origin,omitempty"`
	Steps   []BrowserWorkflowStep `json:"steps,omitempty" description:"Template steps; omit to run the saved flow with this name."`
	Items   []map[string]string   `json:"items,omitempty" description:"1-50 input sets; each runs the whole template once."`
	Save    bool                  `json:"save,omitempty" description:"Store the template for later runs by name."`
	Rerun   string                `json:"rerun,omitempty" description:"run_id of an earlier run: re-run only its failed and uncertain items with their recorded inputs."`
}

type flowDefinition struct {
	Name    string                `json:"name"`
	Version string                `json:"version"`
	Inputs  []string              `json:"inputs"`
	TabID   string                `json:"tab_id,omitempty"`
	Origin  string                `json:"origin,omitempty"`
	Steps   []BrowserWorkflowStep `json:"steps"`
	SavedAt time.Time             `json:"saved_at"`
}

type flowItemResult struct {
	Index      int               `json:"index"`
	Inputs     map[string]string `json:"inputs"`
	Status     string            `json:"status"`
	FailedStep string            `json:"failed_step,omitempty"`
	Error      string            `json:"error,omitempty"`
	Steps      []PipelineResult  `json:"steps,omitempty"`
	Attempts   int               `json:"attempts"`
}

type flowRun struct {
	RunID   string           `json:"run_id"`
	Name    string           `json:"name"`
	Version string           `json:"version"`
	Items   []flowItemResult `json:"items"`
	Started time.Time        `json:"started"`
	Updated time.Time        `json:"updated"`
}

var (
	flowNamePattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	flowVersionPattern = regexp.MustCompile(`^\d{1,4}\.\d{1,4}$`)
	flowPlaceholder    = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)
)

const maxFlowItems = 50

func flowMajor(version string) string {
	major, _, _ := strings.Cut(version, ".")
	return major
}

// validateFlowTemplate rejects anything that would replay an old page
// state: pointer input, refs, viewport ids and ref selectors.
func validateFlowTemplate(steps []BrowserWorkflowStep) error {
	if len(steps) == 0 || len(steps) > maxWorkflowSteps {
		return fmt.Errorf("a flow template needs 1-%d steps", maxWorkflowSteps)
	}
	for _, s := range steps {
		if strings.HasPrefix(s.Action, "pointer_") {
			return fmt.Errorf("step %s: pointer input cannot be stored in a reusable flow; coordinates belong to one screenshot", s.ID)
		}
		if s.Ref != "" || s.Target.ViewportID != "" || strings.Contains(s.Target.Selector, "data-atlas-ref") || s.Bindings["ref"] != "" {
			return fmt.Errorf("step %s: refs and viewport ids are transient; resolve targets by role, name, label, within or a stable selector", s.ID)
		}
	}
	return nil
}

func flowSubstitute(v any, inputs map[string]string, missing map[string]bool) any {
	switch value := v.(type) {
	case string:
		return flowPlaceholder.ReplaceAllStringFunc(value, func(m string) string {
			name := flowPlaceholder.FindStringSubmatch(m)[1]
			replacement, ok := inputs[name]
			if !ok {
				missing[name] = true
			}
			return replacement
		})
	case map[string]any:
		out := map[string]any{}
		for k, v := range value {
			out[k] = flowSubstitute(v, inputs, missing)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, v := range value {
			out[i] = flowSubstitute(v, inputs, missing)
		}
		return out
	}
	return v
}

// instantiateFlow substitutes one item's inputs into the template through
// its JSON form, so every string field is covered the same way.
func instantiateFlow(def flowDefinition, inputs map[string]string) (BrowserWorkflowParams, error) {
	for _, name := range def.Inputs {
		if _, ok := inputs[name]; !ok {
			return BrowserWorkflowParams{}, fmt.Errorf("item is missing input %q", name)
		}
	}
	for name, value := range inputs {
		if len(value) > 4096 || len(name) > 64 {
			return BrowserWorkflowParams{}, errors.New("flow inputs are limited to 64-character names and 4 KiB values")
		}
	}
	data, err := json.Marshal(BrowserWorkflowParams{TabID: def.TabID, Origin: def.Origin, Steps: def.Steps})
	if err != nil {
		return BrowserWorkflowParams{}, err
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return BrowserWorkflowParams{}, err
	}
	missing := map[string]bool{}
	generic = flowSubstitute(generic, inputs, missing)
	if len(missing) > 0 {
		var names []string
		for name := range missing {
			names = append(names, name)
		}
		slices.Sort(names)
		return BrowserWorkflowParams{}, fmt.Errorf("template uses undeclared inputs %s", strings.Join(names, ", "))
	}
	data, _ = json.Marshal(generic)
	var out BrowserWorkflowParams
	return out, json.Unmarshal(data, &out)
}

func flowRoot(dir string) (*os.Root, error) {
	if dir == "" {
		return nil, errors.New("reusable flows need the workspace state store, which is unavailable here")
	}
	if err := os.MkdirAll(filepath.Join(dir, "browser", "flows", "runs"), 0o700); err != nil {
		return nil, err
	}
	return os.OpenRoot(filepath.Join(dir, "browser", "flows"))
}

func readFlowJSON(root *os.Root, name string, v any) error {
	f, err := root.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4*1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 4*1024*1024 {
		return errors.New("flow record exceeds size limit")
	}
	return json.Unmarshal(data, v)
}

func writeFlowJSON(root *os.Root, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := root.WriteFile(name+".tmp", data, 0o600); err != nil {
		return err
	}
	return root.Rename(name+".tmp", name)
}

func newFlowRunID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return time.Now().UTC().Format("20060102T150405") + "-" + hex.EncodeToString(b[:])
}

// runBrowserFlow resolves the flow definition, runs each selected item
// through the ordinary pipeline executor and stores per-item results.
func runBrowserFlow(ctx context.Context, p BrowserFlowParams, dir string, timeout time.Duration, call fantasy.ToolCall, invoke func(context.Context, fantasy.ToolCall) (fantasy.ToolResponse, error)) (fantasy.ToolResponse, error) {
	if !flowNamePattern.MatchString(p.Name) {
		return fantasy.NewTextErrorResponse("flow name must be 1-64 letters, digits, - or _"), nil
	}
	if p.Version != "" && !flowVersionPattern.MatchString(p.Version) {
		return fantasy.NewTextErrorResponse("flow version must be major.minor, e.g. 1.0"), nil
	}
	root, err := flowRoot(dir)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	defer root.Close()
	defPath := p.Name + ".json"
	var def flowDefinition
	if len(p.Steps) > 0 {
		if err := validateFlowTemplate(p.Steps); err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		def = flowDefinition{Name: p.Name, Version: firstNonEmpty(p.Version, "1.0"), Inputs: p.Inputs, TabID: p.TabID, Origin: p.Origin, Steps: p.Steps, SavedAt: time.Now()}
		if p.Save {
			if err := writeFlowJSON(root, defPath, def); err != nil {
				return fantasy.NewTextErrorResponse("saving flow failed: " + err.Error()), nil
			}
		}
	} else {
		if err := readFlowJSON(root, defPath, &def); err != nil {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("no saved flow %q: supply steps (and save:true) first", p.Name)), nil
		}
		if p.Version != "" && flowMajor(p.Version) != flowMajor(def.Version) {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("flow %q is version %s; the request expects %s. Review the saved template before running it", p.Name, def.Version, p.Version)), nil
		}
		if p.TabID != "" {
			def.TabID = p.TabID
		}
		if p.Origin != "" {
			def.Origin = p.Origin
		}
	}
	run := flowRun{RunID: newFlowRunID(), Name: def.Name, Version: def.Version, Started: time.Now()}
	var selected []flowItemResult
	if p.Rerun != "" {
		if !regexp.MustCompile(`^[0-9T]{15}-[0-9a-f]{12}$`).MatchString(p.Rerun) {
			return fantasy.NewTextErrorResponse("rerun must be a run_id returned by an earlier flow run"), nil
		}
		if err := readFlowJSON(root, filepath.Join("runs", p.Rerun+".json"), &run); err != nil {
			return fantasy.NewTextErrorResponse("unknown flow run " + p.Rerun), nil
		}
		if run.Name != def.Name || flowMajor(run.Version) != flowMajor(def.Version) {
			return fantasy.NewTextErrorResponse("that run belongs to another flow or major version"), nil
		}
		for _, item := range run.Items {
			if item.Status != StepSucceeded {
				selected = append(selected, item)
			}
		}
		if len(selected) == 0 {
			data, _ := json.Marshal(map[string]any{"run_id": run.RunID, "rerun": 0, "items": run.Items, "next": "Every item already succeeded; nothing was run."})
			return fantasy.NewTextResponse(string(data)), nil
		}
	} else {
		if len(p.Items) == 0 || len(p.Items) > maxFlowItems {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("flow runs need 1-%d items", maxFlowItems)), nil
		}
		for i, inputs := range p.Items {
			run.Items = append(run.Items, flowItemResult{Index: i, Inputs: inputs, Status: "pending"})
		}
		selected = run.Items
	}
	// Compile every selected item before running any, so a malformed later
	// item cannot strand earlier mutations.
	compiled := map[int][]PipelineStep{}
	for _, item := range selected {
		workflow, err := instantiateFlow(def, item.Inputs)
		if err == nil {
			compiled[item.Index], err = compileBrowserWorkflow(workflow)
		}
		if err != nil {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("item %d: %s; nothing was run", item.Index, err)), nil
		}
		if resp, invalid := validatePipeline(ctx, compiled[item.Index], nil); invalid {
			return fantasy.NewTextErrorResponse(fmt.Sprintf("item %d: %s; nothing was run", item.Index, resp.Content)), nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var stop *fantasy.ToolResponse
	for _, item := range selected {
		i := slices.IndexFunc(run.Items, func(r flowItemResult) bool { return r.Index == item.Index })
		if err := ctx.Err(); err != nil || stop != nil {
			break
		}
		child := call
		child.ID = call.ID + "/item" + strconv.Itoa(item.Index)
		result, err := executePipeline(ctx, compiled[item.Index], nil, child, invoke)
		if err != nil && ctx.Err() == nil {
			return fantasy.ToolResponse{}, err
		}
		entry := &run.Items[i]
		entry.Attempts++
		entry.Steps, entry.Status, entry.FailedStep, entry.Error = result.results, StepSucceeded, "", ""
		for _, step := range result.results {
			if step.Status == StepUncertain || step.Status == StepFailed || step.Status == StepStopped {
				entry.Status, entry.FailedStep = step.Status, step.ID
				entry.Error = truncateRunes(step.Content, 400)
				if step.Status == StepUncertain {
					break
				}
			}
		}
		switch {
		case err != nil:
			entry.Status, entry.Error = StepFailed, "canceled or timed out: "+err.Error()
		case result.invalid != "":
			entry.Status, entry.Error = StepFailed, result.invalid
		case result.stopTurn != nil:
			entry.Status = StepStopped
			stop = result.stopTurn
		case !result.failed && entry.Status != StepUncertain:
			entry.Status = StepSucceeded
		}
	}
	run.Updated = time.Now()
	if err := writeFlowJSON(root, filepath.Join("runs", run.RunID+".json"), run); err != nil {
		return fantasy.NewTextErrorResponse("storing flow results failed: " + err.Error()), nil
	}
	if stop != nil {
		return *stop, nil
	}
	counts := map[string]int{}
	summary := make([]map[string]any, 0, len(run.Items))
	for _, item := range run.Items {
		counts[item.Status]++
		entry := map[string]any{"index": item.Index, "status": item.Status, "attempts": item.Attempts}
		if item.FailedStep != "" {
			entry["failed_step"], entry["error"] = item.FailedStep, item.Error
		}
		summary = append(summary, entry)
	}
	next := "All items succeeded."
	if counts[StepSucceeded] < len(run.Items) {
		next = "Observe the page for uncertain items before retrying them; then rerun only failed and uncertain items with flow.rerun=" + run.RunID + "."
	}
	data, _ := json.Marshal(map[string]any{"run_id": run.RunID, "flow": def.Name, "version": def.Version, "counts": counts, "items": summary, "next": next})
	if ctx.Err() != nil || counts[StepSucceeded] < len(run.Items) {
		return fantasy.NewTextErrorResponse(string(data)), nil
	}
	return fantasy.NewTextResponse(string(data)), nil
}
