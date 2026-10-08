package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/clibridge"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/execution"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/qualityflows"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/shell"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/vault"
	"github.com/google/uuid"
)

// CLIBridgeToolName is the name of the profile-driven CLI tool.
const CLIBridgeToolName = "cli_bridge"

//go:embed cli_bridge.md
var cliBridgeDescription string

// CLIBridgeParams is deliberately flat: values travel as name=value strings
// and are typed by the profile at runtime, so no provider has to accept a
// free-form or deeply nested object.
type CLIBridgeParams struct {
	Action     string   `json:"action" enum:"list,inspect,validate,preview,run,status,draft" description:"list profiles; inspect one contract; validate a profile file or arguments; preview the literal argv without running; run through guarded bash; status of a run; draft a profile from --help output."`
	Profile    string   `json:"profile,omitempty" description:"Installed profile: id, project:id or user:id. For validate also a project-relative .json profile file. For draft the new draft id."`
	Operation  string   `json:"operation,omitempty" description:"Operation id inside the profile (validate/preview/run)."`
	Args       []string `json:"args,omitempty" description:"Parameter values as literal name=value strings; repeat a name for list params, e.g. [\"input=clips/a.mov\",\"crf=23\"]."`
	PrepareID  string   `json:"prepare_id,omitempty" description:"run: prepare_id from preview; the run is refused if the profile, executable or argv changed since."`
	Background bool     `json:"background,omitempty" description:"run: start as a background job when the operation allows it; read the result later with status."`
	RunID      string   `json:"run_id,omitempty" description:"status: run_id returned by run."`
	Executable string   `json:"executable,omitempty" description:"draft: bare program name or absolute path whose help text to capture."`
	HelpArgs   []string `json:"help_args,omitempty" description:"draft: literal help arguments; default [\"--help\"]."`
}

// CLIBridgeMetadata keeps previews and drafts out of verification evidence
// while still letting operation accounting treat them as completed calls.
type CLIBridgeMetadata struct {
	Observed bool   `json:"observed"`
	Passed   bool   `json:"passed"`
	Prepared bool   `json:"prepared,omitempty"`
	RunID    string `json:"run_id,omitempty"`
}

// CLIBridgeServices are the shared systems the tool delegates to. The tool
// never starts a process itself: every program, including a help capture,
// runs through Invoke as an ordinary bash argv call.
type CLIBridgeServices struct {
	Root        string
	Store       *engineering.Store
	Permissions permission.Service
	Invoke      ToolInvoker
	Profiles    clibridge.Store
}

// NewCLIBridgeTool builds the cli_bridge tool.
func NewCLIBridgeTool(services CLIBridgeServices) fantasy.AgentTool {
	if services.Profiles.ProjectDir == "" {
		services.Profiles = clibridge.NewStore(services.Root)
	}
	b := &cliBridge{CLIBridgeServices: services}
	return fantasy.NewAgentTool(CLIBridgeToolName, cliBridgeDescription, b.run)
}

type cliBridge struct{ CLIBridgeServices }

func (b *cliBridge) run(ctx context.Context, p CLIBridgeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	switch p.Action {
	case "list":
		return b.list(ctx)
	case "inspect":
		return b.inspect(ctx, p)
	case "validate":
		return b.validate(ctx, p)
	case "preview":
		return b.preview(ctx, p)
	case "run":
		return b.execute(ctx, p, call)
	case "status":
		return b.status(ctx, p)
	case "draft":
		return b.draft(ctx, p, call)
	}
	return fantasy.NewTextErrorResponse("action must be list, inspect, validate, preview, run, status or draft"), nil
}

func jsonResponse(value any, meta CLIBridgeMetadata, isError bool) (fantasy.ToolResponse, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	resp := fantasy.NewTextResponse(string(data))
	resp.IsError = isError
	return fantasy.WithResponseMetadata(resp, meta), nil
}

func prepared(value any) (fantasy.ToolResponse, error) {
	return jsonResponse(value, CLIBridgeMetadata{Prepared: true}, false)
}

func (b *cliBridge) list(ctx context.Context) (fantasy.ToolResponse, error) {
	rows, err := b.Profiles.List()
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	type row struct {
		clibridge.Summary
		Executable clibridge.Status `json:"executable_status,omitempty"`
	}
	out := make([]row, 0, len(rows))
	for _, summary := range rows {
		r := row{Summary: summary}
		if summary.Error == "" {
			if installed, err := b.Profiles.Load(summary.Scope, summary.ID); err == nil {
				_, err := clibridge.Current(ctx, installed)
				r.Executable = clibridge.StatusOf(err)
			}
		}
		out = append(out, r)
	}
	return prepared(map[string]any{"profiles": out, "note": "Listing runs nothing. Use inspect for a full contract."})
}

func (b *cliBridge) find(profile string) (clibridge.Installed, error) {
	ref, err := clibridge.ParseRef(profile)
	if err != nil {
		return clibridge.Installed{}, err
	}
	return b.Profiles.Find(ref)
}

func (b *cliBridge) inspect(ctx context.Context, p CLIBridgeParams) (fantasy.ToolResponse, error) {
	installed, err := b.find(p.Profile)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	identity, exeErr := clibridge.Current(ctx, installed)
	executable := map[string]any{"status": clibridge.StatusOf(exeErr), "identity": identity, "declared": installed.Profile.Executable}
	if exeErr != nil {
		executable["error"] = exeErr.Error()
	}
	var env []clibridge.EnvStatus
	for _, name := range installed.Profile.Env {
		_, present := os.LookupEnv(name)
		env = append(env, clibridge.EnvStatus{Name: name, Present: present})
	}
	return prepared(map[string]any{
		"scope": installed.Scope, "profile_hash": installed.ProfileSHA256, "imported_at": installed.ImportedAt,
		"profile": installed.Profile, "executable": executable, "env": env,
		"note": "Inspection runs nothing. Descriptions are the author's claims; permissions and hooks still decide every run.",
	})
}

func (b *cliBridge) validate(ctx context.Context, p CLIBridgeParams) (fantasy.ToolResponse, error) {
	if strings.HasSuffix(strings.ToLower(p.Profile), ".json") {
		data, err := qualityflows.ReadFile(b.Root, p.Profile, 256*1024)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		profile, err := clibridge.ParseProfile(data)
		if err != nil {
			return jsonResponse(map[string]any{"valid": false, "error": err.Error(), "executed": false}, CLIBridgeMetadata{Prepared: true}, true)
		}
		var ops []string
		for _, op := range profile.Operations {
			ops = append(ops, op.ID)
		}
		return prepared(map[string]any{"valid": true, "profile_id": profile.ID, "profile_hash": profile.Hash(), "operations": ops, "executed": false, "installed": false})
	}
	prep, err := b.prepare(ctx, p)
	if err != nil {
		return jsonResponse(map[string]any{"valid": false, "error": err.Error(), "executed": false}, CLIBridgeMetadata{Prepared: true}, true)
	}
	return prepared(map[string]any{"valid": true, "prepare_id": prep.ID, "argv": prep.DisplayArgv, "executed": false})
}

// prepare resolves the profile, verifies the pinned executable and renders
// the literal argv.
func (b *cliBridge) prepare(ctx context.Context, p CLIBridgeParams) (clibridge.Prepared, error) {
	installed, err := b.find(p.Profile)
	if err != nil {
		return clibridge.Prepared{}, err
	}
	identity, err := clibridge.Current(ctx, installed)
	if err != nil {
		return clibridge.Prepared{}, err
	}
	root, err := filepath.Abs(b.Root)
	if err != nil {
		return clibridge.Prepared{}, err
	}
	return clibridge.Prepare(installed, identity, p.Operation, p.Args, clibridge.RenderOptions{Root: root})
}

func (b *cliBridge) preview(ctx context.Context, p CLIBridgeParams) (fantasy.ToolResponse, error) {
	if b.Store == nil || GetSessionFromContext(ctx) == "" {
		return fantasy.NewTextErrorResponse("engineering store and session required"), nil
	}
	prep, err := b.prepare(ctx, p)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if err := b.recordPrepared(ctx, prep); err != nil {
		return fantasy.NewTextErrorResponse("preview could not be recorded: " + err.Error()), nil
	}
	return prepared(map[string]any{"preview": prep, "executed": false, "note": "Preview runs nothing and is not verification evidence. Pass prepare_id to run to bind the run to this exact argv."})
}

// recordPrepared stores a preparation without its argv or secret values.
// The same preparation is stored once; its identity is its content.
func (b *cliBridge) recordPrepared(ctx context.Context, prep clibridge.Prepared) error {
	namespace := clibridge.PreparedNamespace(GetSessionFromContext(ctx))
	if _, _, err := b.Store.ReadRecord(ctx, namespace, prep.ID); err == nil {
		return nil
	}
	data, err := json.Marshal(prep)
	if err != nil {
		return err
	}
	_, err = b.Store.PutRecord(ctx, namespace, prep.ID, 0, data)
	if errors.Is(err, engineering.ErrRecordRevisionConflict) {
		return nil
	}
	return err
}

func (b *cliBridge) execute(ctx context.Context, p CLIBridgeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	session := GetSessionFromContext(ctx)
	if b.Store == nil || session == "" {
		return fantasy.NewTextErrorResponse("engineering store and session required"), nil
	}
	if execution.IsReadOnly(ctx) {
		return fantasy.NewTextErrorResponse("cli_bridge run is unavailable in read-only mode; a profile's own description cannot make a program read-only"), nil
	}
	if execution.HasBinding(ctx) {
		return fantasy.NewTextErrorResponse("container-required execution is active; CLI Bridge binds host executables and does not run them outside the required container. Use bash under the container policy instead."), nil
	}
	if b.Invoke == nil {
		return fantasy.NewTextErrorResponse("guarded command dispatch unavailable"), nil
	}
	prep, err := b.prepare(ctx, p)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	if p.PrepareID != "" && p.PrepareID != prep.ID {
		return fantasy.NewTextErrorResponse(b.staleReason(ctx, p.PrepareID, prep)), nil
	}
	if p.Background && !prep.BackgroundAllowed {
		return fantasy.NewTextErrorResponse("operation " + prep.Operation + " does not allow background execution"), nil
	}
	if err := checkCLIBridgeOwnership(ctx, b.Root, prep); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	namespace := clibridge.RunNamespace(session)
	if records, err := b.Store.ListRecords(ctx, namespace); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	} else if len(records) >= 128 {
		return fantasy.NewTextErrorResponse("this session already holds 128 CLI Bridge run reports; start a new session to keep recording evidence"), nil
	}
	if err := b.recordPrepared(ctx, prep); err != nil {
		return fantasy.NewTextErrorResponse("run could not be prepared for recording: " + err.Error()), nil
	}
	for _, in := range prep.Inputs {
		if !in.External {
			continue
		}
		if b.Permissions == nil {
			return fantasy.NewTextErrorResponse("permission service unavailable for external input"), nil
		}
		ok, err := b.Permissions.Request(ctx, permission.CreatePermissionRequest{SessionID: session, Path: in.Path, ToolCallID: call.ID, ToolName: CLIBridgeToolName, Action: "read", Description: "Let " + prep.ProfileID + "/" + prep.Operation + " read external file " + in.Path, Params: map[string]string{"profile": prep.ProfileID, "operation": prep.Operation, "path": in.Path}})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !ok {
			report := clibridge.NewReport("run-"+uuid.NewString(), prep, time.Now())
			report.Outcome, report.Detail = clibridge.OutcomeDenied, "external input read permission denied; no process started"
			_ = b.save(ctx, session, report, prep)
			return NewPermissionDeniedResponse(b.Permissions), nil
		}
	}
	redactions := cliBridgeSecrets(ctx, prep)
	for _, value := range redactions {
		if err := vault.Register(session, value); err != nil {
			return fantasy.NewTextErrorResponse("CLI Bridge cannot start without credential protection: " + err.Error()), nil
		}
	}
	runID := "run-" + uuid.NewString()
	started := time.Now()
	report := clibridge.NewReport(runID, prep, started)
	params, _ := json.Marshal(BashParams{Description: "cli_bridge " + prep.ProfileID + "/" + prep.Operation, Argv: prep.Argv, WorkingDir: prep.WorkingDir, RunInBackground: p.Background})
	timeout := time.Duration(prep.TimeoutSeconds) * time.Second
	runCtx := WithArgvOptions(ctx, ArgvOptions{StreamLimit: prep.MaxOutputBytes, JobTimeout: timeout})
	cancel := func() {}
	if !p.Background {
		runCtx, cancel = context.WithTimeout(runCtx, timeout)
	}
	resp, runErr := b.Invoke(context.WithValue(runCtx, cliBridgeOutputContextKey{}, true), fantasy.ToolCall{ID: call.ID + "-cli-bridge", Name: BashToolName, Input: string(params)})
	timedOut := !p.Background && errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil
	cancel()
	var stop *fantasy.ToolResponse
	switch {
	case timedOut:
		report.Outcome, report.Detail = clibridge.OutcomeTimeout, fmt.Sprintf("stopped after the profile limit of %ds; the process was terminated and its partial effects are unknown", prep.TimeoutSeconds)
		report.FinishedAt, report.DurationMS = time.Now().UnixMilli(), time.Since(started).Milliseconds()
	case ctx.Err() != nil || errors.Is(runErr, context.Canceled):
		report.Outcome, report.Detail = clibridge.OutcomeCanceled, "canceled before an exit was observed; the result is unknown and the command is not retried automatically"
		report.FinishedAt, report.DurationMS = time.Now().UnixMilli(), time.Since(started).Milliseconds()
		if err := b.save(ctx, session, report, prep, redactions...); err != nil {
			return fantasy.ToolResponse{}, errors.Join(ctx.Err(), err)
		}
		return fantasy.ToolResponse{}, cmpErr(runErr, ctx.Err())
	case runErr != nil:
		report.Outcome, report.Detail = clibridge.OutcomeError, runErr.Error()
		if strings.Contains(runErr.Error(), "blocked by policy") {
			report.Outcome = clibridge.OutcomeBlocked
		}
	case resp.IsError || resp.StopTurn:
		report.Outcome, report.Detail = classifyRefusal(resp), truncateVerification(resp.Content, 2048)
		if resp.StopTurn {
			stop = &resp
		}
	default:
		var meta BashResponseMetadata
		if err := json.Unmarshal([]byte(resp.Metadata), &meta); err != nil {
			report.Outcome, report.Detail = clibridge.OutcomeError, "no observable command result"
			break
		}
		if meta.Background {
			report.Outcome, report.Background, report.JobID = clibridge.OutcomeRunning, true, meta.ShellID
			report.Detail = "running as background job " + meta.ShellID + "; call cli_bridge status with this run_id for the observed result"
			break
		}
		if meta.ExitCode == nil {
			report.Outcome, report.Detail = clibridge.OutcomeError, "no exit code was observed"
			break
		}
		stdout, stderr := meta.Output, ""
		if meta.Streams != nil {
			stdout, stderr = meta.Streams.Stdout, meta.Streams.Stderr
			report.StdoutBytes, report.StderrBytes, report.Truncated = meta.Streams.StdoutBytes, meta.Streams.StderrBytes, meta.Streams.Truncated
		}
		report.Stdout, report.Stderr = stdout, stderr
		report.Finish(prep, *meta.ExitCode, stdout, stderr, time.Now())
	}
	if err := b.save(ctx, session, report, prep, redactions...); err != nil {
		resp := fantasy.NewTextErrorResponse("the command outcome could not be recorded: " + err.Error())
		resp.StopTurn = stop != nil
		return resp, nil
	}
	if stop != nil {
		return *stop, nil
	}
	report = clibridge.RedactReport(report, redactions)
	report = redactRegisteredBridgeReport(session, report)
	running := report.Outcome == clibridge.OutcomeRunning
	return jsonResponse(report, CLIBridgeMetadata{Observed: report.Observed, Passed: report.Passed, Prepared: running, RunID: report.RunID}, !report.Passed && !running)
}

func cmpErr(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// staleReason explains which binding changed since a preview.
func (b *cliBridge) staleReason(ctx context.Context, id string, current clibridge.Prepared) string {
	_, data, err := b.Store.ReadRecord(ctx, clibridge.PreparedNamespace(GetSessionFromContext(ctx)), id)
	if err != nil {
		return "prepare_id " + id + " is unknown in this session; preview again"
	}
	var old clibridge.Prepared
	if json.Unmarshal(data, &old) != nil {
		return "stored preview is unreadable; preview again"
	}
	var changed []string
	if old.ProfileID != current.ProfileID || old.Operation != current.Operation {
		changed = append(changed, "profile/operation")
	}
	if old.ProfileHash != current.ProfileHash {
		changed = append(changed, "profile contract (sha256 "+shortID(old.ProfileHash)+" -> "+shortID(current.ProfileHash)+")")
	}
	if old.Executable.SHA256 != current.Executable.SHA256 || old.Executable.Path != current.Executable.Path {
		changed = append(changed, "executable identity")
	}
	if old.WorkingDir != current.WorkingDir {
		changed = append(changed, "working directory")
	}
	if len(changed) == 0 {
		changed = append(changed, "literal argv")
	}
	return "refusing to run: " + strings.Join(changed, ", ") + " changed since preview " + id + "; review a new preview before running"
}

func shortID(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// classifyRefusal names why a guarded call did not run.
func classifyRefusal(resp fantasy.ToolResponse) clibridge.Outcome {
	content := resp.Content
	switch {
	case strings.Contains(content, "blocked by hook") || strings.Contains(content, "halted by hook"):
		return clibridge.OutcomeHookDenied
	case strings.Contains(content, "User denied permission") || strings.Contains(content, "PLAN MODE"):
		return clibridge.OutcomeDenied
	case strings.Contains(strings.ToLower(content), "blocked by policy"), strings.Contains(content, "assigned workspace"), strings.Contains(content, "ownership"), strings.Contains(content, "disabled or unavailable"):
		return clibridge.OutcomeBlocked
	}
	return clibridge.OutcomeError
}

// cliBridgeSecrets lists values that must never be persisted: secret
// params and the values of declared environment variables.
func cliBridgeSecrets(ctx context.Context, prep clibridge.Prepared) []string {
	_ = ctx
	values := append([]string(nil), prep.Secrets...)
	for _, env := range prep.Env {
		if value, ok := os.LookupEnv(env.Name); ok && value != "" {
			values = append(values, value)
		}
	}
	return values
}

// checkCLIBridgeOwnership applies the assigned workspace and task ownership
// to output files. The working directory is checked again by the guarded
// bash call itself.
func checkCLIBridgeOwnership(ctx context.Context, root string, prep clibridge.Prepared) error {
	scope := engineering.GetScope(ctx, GetSessionFromContext(ctx))
	base := root
	if scope.WriteRoot != "" {
		base = scope.WriteRoot
	}
	for _, out := range prep.Outputs {
		rel, err := filepath.Rel(base, out.Path)
		if err != nil || !filepath.IsLocal(rel) {
			return fmt.Errorf("output %s is outside the assigned workspace", out.Path)
		}
		if len(scope.OwnedPaths) == 0 {
			continue
		}
		rel = filepath.ToSlash(rel)
		owned := false
		for _, path := range scope.OwnedPaths {
			path = filepath.ToSlash(filepath.Clean(path))
			if path == "." || strings.EqualFold(rel, path) || strings.HasPrefix(strings.ToLower(rel), strings.ToLower(path)+"/") {
				owned = true
				break
			}
		}
		if !owned {
			return fmt.Errorf("output %s is outside this task's declared ownership; hand the change to its owner", rel)
		}
	}
	return nil
}

// save persists a redacted report and links bounded output artifacts.
func (b *cliBridge) save(ctx context.Context, session string, report clibridge.RunReport, prep clibridge.Prepared, redactions ...string) error {
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	report = clibridge.RedactReport(report, append(redactions, prep.Secrets...))
	report = redactRegisteredBridgeReport(session, report)
	report.Stdout, _ = clibridge.BoundText(report.Stdout, prep.MaxOutputBytes)
	report.Stderr, _ = clibridge.BoundText(report.Stderr, prep.MaxOutputBytes)
	var linked []engineering.ArtifactRef
	for _, stream := range []struct {
		text string
		ref  *string
	}{{report.Stdout, &report.StdoutArtifact}, {report.Stderr, &report.StderrArtifact}} {
		if stream.text == "" {
			continue
		}
		ref, err := b.Store.PutArtifact(saveCtx, "cli-bridge-output", []byte(stream.text))
		if err != nil {
			return err
		}
		*stream.ref = ref.Hash
		linked = append(linked, ref)
	}
	namespace := clibridge.RunNamespace(session)
	record, _, err := b.Store.ReadRecord(saveCtx, namespace, report.RunID)
	expected := uint64(0)
	if err == nil {
		expected = record.Revision
	}
	data, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, err = b.Store.PutRecord(saveCtx, namespace, report.RunID, expected, data, linked...)
	return err
}

// redactRegisteredBridgeReport protects every string field using launch-time
// registrations, including expectation details after environment rotation.
func redactRegisteredBridgeReport(session string, report clibridge.RunReport) clibridge.RunReport {
	data, err := json.Marshal(report)
	if err != nil {
		return clibridge.RunReport{Outcome: clibridge.OutcomeError, Detail: "Could not sanitize command report"}
	}
	var sanitized clibridge.RunReport
	if json.Unmarshal([]byte(redactCLIBridgePayload(session, string(data))), &sanitized) != nil {
		return clibridge.RunReport{Outcome: clibridge.OutcomeError, Detail: "Could not sanitize command report"}
	}
	return sanitized
}

func (b *cliBridge) load(ctx context.Context, runID string) (clibridge.RunReport, error) {
	_, data, err := b.Store.ReadRecord(ctx, clibridge.RunNamespace(GetSessionFromContext(ctx)), runID)
	if err != nil {
		return clibridge.RunReport{}, clibridge.ErrNotFound
	}
	var report clibridge.RunReport
	return report, json.Unmarshal(data, &report)
}

func (b *cliBridge) status(ctx context.Context, p CLIBridgeParams) (fantasy.ToolResponse, error) {
	if b.Store == nil || GetSessionFromContext(ctx) == "" {
		return fantasy.NewTextErrorResponse("engineering store and session required"), nil
	}
	report, err := b.load(ctx, p.RunID)
	if err != nil {
		return fantasy.NewTextErrorResponse("run " + p.RunID + ": " + err.Error()), nil
	}
	if report.Outcome == clibridge.OutcomeRunning && report.JobID != "" {
		report, err = b.collect(ctx, report)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
	}
	// Reading a report is a successful observation of stored evidence even
	// when the run it describes failed; observed/passed describe the run.
	return jsonResponse(report, CLIBridgeMetadata{Observed: report.Observed, Passed: report.Passed, Prepared: true, RunID: report.RunID}, false)
}

// collect observes a finished background job. Expectations come from the
// preparation recorded when the run started, never from caller input.
func (b *cliBridge) collect(ctx context.Context, report clibridge.RunReport) (clibridge.RunReport, error) {
	session := GetSessionFromContext(ctx)
	prep := clibridge.Prepared{MaxOutputBytes: 64 * 1024}
	_, data, err := b.Store.ReadRecord(ctx, clibridge.PreparedNamespace(session), report.PrepareID)
	if err != nil || json.Unmarshal(data, &prep) != nil {
		report.Outcome, report.Detail = clibridge.OutcomeError, "the preparation for this run is missing; its result cannot be evaluated"
		return b.saveCollectedReport(ctx, session, report, prep)
	}
	job, ok := shell.GetBackgroundShellManager().Get(report.JobID)
	if !ok {
		report.Outcome, report.Detail = clibridge.OutcomeError, "background job "+report.JobID+" is no longer tracked (Atlas restarted or the job was removed); its result was not observed"
		return b.saveCollectedReport(ctx, session, report, prep)
	}
	if !qualityflows.SameRoot(job.WorkingDir, prep.WorkingDir) {
		return report, fmt.Errorf("background job %s belongs to another working directory", report.JobID)
	}
	stdout, stderr, done, execErr := job.GetOutput()
	if !done {
		return report, nil
	}
	outBytes, errBytes := job.OutputBytes()
	report.StdoutBytes, report.StderrBytes = outBytes, errBytes
	var cutOut, cutErr bool
	report.Stdout, cutOut = clibridge.BoundText(stdout, prep.MaxOutputBytes)
	report.Stderr, cutErr = clibridge.BoundText(stderr, prep.MaxOutputBytes)
	report.Truncated = cutOut || cutErr || int64(len(stdout)) < outBytes || int64(len(stderr)) < errBytes
	switch {
	case job.TimedOut():
		report.Outcome, report.Detail = clibridge.OutcomeTimeout, fmt.Sprintf("the background job exceeded the profile limit of %ds and was terminated; partial effects are unknown", prep.TimeoutSeconds)
		report.FinishedAt = time.Now().UnixMilli()
		report.DurationMS = report.FinishedAt - report.StartedAt
	case !shell.ObservedExit(execErr):
		report.Outcome, report.Detail = clibridge.OutcomeCanceled, "the background job was killed or could not start; no exit was observed"
		if execErr != nil {
			report.Detail += ": " + execErr.Error()
		}
	default:
		report.Finish(prep, shell.ExitCode(execErr), stdout, stderr, time.Now())
	}
	return b.saveCollectedReport(ctx, session, report, prep)
}

// saveCollectedReport returns the same sanitized record that was persisted.
// Secret values captured at launch remain registered after env changes.
func (b *cliBridge) saveCollectedReport(ctx context.Context, session string, report clibridge.RunReport, prep clibridge.Prepared) (clibridge.RunReport, error) {
	if err := b.save(ctx, session, report, prep, cliBridgeSecrets(ctx, prep)...); err != nil {
		return clibridge.RunReport{}, err
	}
	return b.load(ctx, report.RunID)
}

func (b *cliBridge) draft(ctx context.Context, p CLIBridgeParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
	if execution.IsReadOnly(ctx) {
		return fantasy.NewTextErrorResponse("draft runs the program's help and is unavailable in read-only mode"), nil
	}
	if execution.HasBinding(ctx) {
		return fantasy.NewTextErrorResponse("container-required execution is active; help capture of a host executable is not performed"), nil
	}
	if b.Invoke == nil {
		return fantasy.NewTextErrorResponse("guarded command dispatch unavailable"), nil
	}
	declared := clibridge.Executable{Name: p.Executable}
	if filepath.IsAbs(p.Executable) {
		declared = clibridge.Executable{Path: p.Executable}
	}
	identity, err := clibridge.Pin(ctx, declared)
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	helpArgs := p.HelpArgs
	if len(helpArgs) == 0 {
		helpArgs = []string{"--help"}
	}
	if len(helpArgs) > 8 {
		return fantasy.NewTextErrorResponse("at most 8 help arguments"), nil
	}
	argv := append([]string{identity.Path}, helpArgs...)
	if err := shell.ValidateArgv(argv); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	params, _ := json.Marshal(BashParams{Description: "cli_bridge help capture", Argv: argv, WorkingDir: b.Root})
	runCtx, cancel := context.WithTimeout(WithArgvOptions(ctx, ArgvOptions{StreamLimit: clibridge.MaxHelpBytes}), 30*time.Second)
	resp, err := b.Invoke(runCtx, fantasy.ToolCall{ID: call.ID + "-cli-bridge-help", Name: BashToolName, Input: string(params)})
	cancel()
	if err != nil {
		return fantasy.NewTextErrorResponse("help capture did not complete: " + err.Error()), nil
	}
	if resp.IsError || resp.StopTurn {
		return resp, nil
	}
	var meta BashResponseMetadata
	if json.Unmarshal([]byte(resp.Metadata), &meta) != nil || meta.ExitCode == nil {
		return fantasy.NewTextErrorResponse("help capture produced no observable result"), nil
	}
	help := meta.Output
	if meta.Streams != nil {
		help = strings.TrimSpace(meta.Streams.Stdout + "\n" + meta.Streams.Stderr)
	}
	draft, err := clibridge.NewDraft(p.Profile, declared, &identity, help, clibridge.HelpCapture{Source: "program", Argv: argv, ExitCode: meta.ExitCode})
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	data, _ := json.MarshalIndent(draft, "", "  ")
	path := clibridge.DraftPath(b.Root, draft.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	write, _ := json.Marshal(WriteParams{FilePath: path, Content: string(data) + "\n"})
	written, err := b.Invoke(ctx, fantasy.ToolCall{ID: call.ID + "-cli-bridge-draft", Name: WriteToolName, Input: string(write)})
	if err != nil || written.IsError || written.StopTurn {
		return written, err
	}
	return prepared(map[string]any{"draft_path": path, "candidates": len(draft.Candidates), "help_sha256": draft.Help.SHA256, "notes": draft.Notes, "active": false})
}
