package agent

import (
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// The evaluator is the completion gate. An agent that grades its own work
// skews positive: it finds a real problem, then talks itself into calling
// it minor. Tuning a separate, skeptical reviewer is far more tractable
// than making the author critical of itself, so a turn that changed files
// is not finished until a reviewer with a fresh context has looked at the
// code. A failing verdict goes straight back to the agent as its next
// instruction, so the user receives fixed work instead of a note about
// broken work.
//
// The gate fails open. A reviewer that errors, times out or ignores the
// reply format lets the turn end as it would have without the gate: it is
// a quality aid layered on top of the agent's own verification, not a
// safety boundary.

// evaluatorTimeout bounds one review. The reviewer reads code with tools,
// so it needs more room than the advisor's quick look, but the session is
// held busy while it runs.
const evaluatorTimeout = 3 * time.Minute

// evaluatorMaxSteps bounds the reviewer's own tool use.
const evaluatorMaxSteps = 30

// evaluatorMaxChars truncates the request and the agent's report.
const evaluatorMaxChars = 6000

// evaluatorMaxCommands keeps only the most recent commands as evidence.
const evaluatorMaxCommands = 12

// evaluatorOutputTail is how much of each command's output is shown.
const evaluatorOutputTail = 300

// evaluatorToolNames are the reviewer's tools: read-only, plus git so it
// can see exactly what changed.
var evaluatorToolNames = []string{"glob", "grep", "ls", "view", tools.GitDiffToolName, tools.GitStatusToolName}

// evaluatorFileTools are the tools whose successful calls change project
// files. Bash is deliberately absent: most bash calls build or test, and
// gating every test run would review turns that changed nothing.
var evaluatorFileTools = []string{"edit", "write", "multiedit", "replace_symbol", "rename_symbol", "lsp_rename_file", "undo_feature"}

const evaluatorSystemPrompt = `You are an independent QA reviewer for another AI coding agent. The agent believes it has finished the user's request. Your job is to decide whether that is true by inspecting the actual code, not by trusting the agent's report.

You have read-only tools (glob, grep, ls, view, git_diff, git_status). Start with git_diff or the listed files, then read whatever you need to judge the change in context.

Fail the work only for concrete, material problems:
- A requirement in the request is not implemented, or only partly.
- A bug: wrong logic, an unhandled error or edge case the request implies, a broken call site, code that will not compile.
- The report claims something the code or the command evidence does not support, such as "tests pass" with no test command run, or a command that failed.
- A change the request did not ask for that breaks or removes existing behavior.

Do not fail for style, naming, missing comments or optional improvements. Do not talk yourself out of a real defect because the rest looks good: one material defect fails the work.

Check the request part by part: list every distinct thing the user asked for (use the checklist when one is given) and decide for each whether the code delivers it. A single part missing or only partly done fails the work. When automatic diff checks are listed, decide for each whether the change is justified; an unjustified weakened check or a leftover the agent did not explain is a material problem.

Reply in exactly this format and nothing else:
REQUIREMENTS:
- [met|missing|partial] <requirement>
VERDICT: PASS
or
VERDICT: FAIL
- <finding: what is wrong, where (file:line), and what correct looks like>
- <one line per finding>`

// evaluatorCommand is one command the agent ran, kept as evidence.
type evaluatorCommand struct {
	command string
	failed  bool
	tail    string
	seq     int
	// dir is the working directory the call asked for, empty for the
	// project root; probe runs are told apart by it.
	dir string
}

// contractMaxRuns bounds the distinct commands remembered for the
// verification contract. The oldest are dropped first.
const contractMaxRuns = 64

// contractMaxRounds bounds how often the contract alone sends one request
// back. It is independent of the reviewer's rounds because it costs no model
// call, but a check the agent cannot make pass must still end the turn. The
// rounds escalate: a plain report, then a step back, then two alternative
// approaches, then a last attempt.
const contractMaxRounds = 4

// evaluatorMaxImages bounds the screenshots kept for a visual review.
const evaluatorMaxImages = 3

// evaluatorLedger is what one request did across its automatic
// continuations, so a fix round is judged against the whole change.
type evaluatorLedger struct {
	mu      sync.Mutex
	request string
	// open is set while the request is still being worked on, so a
	// message the user sends meanwhile is added to it.
	open bool
	// root is the project folder the work plan and harness files are
	// read from; see projectRoot.
	root       string
	files      []string
	commands   []evaluatorCommand
	rounds     int
	changed    bool // files changed since the last verdict
	lastFailed bool
	findings   string

	// seq orders edits and commands; lastEdit is the seq of the latest
	// code change, so a check run before it no longer proves anything.
	seq            int
	lastEdit       int
	runs           map[string]evaluatorCommand
	contractRounds int
	// greenEdit is lastEdit+1 when the verification contract passed with
	// no code change since; zero when it has not.
	greenEdit int
	// failStreak counts, per required command, the consecutive contract
	// rounds in which it failed; it drives the escalation.
	failStreak map[string]int
	// probes are pending proofs that a feature's check can fail.
	probes map[string]contractProbe
	// followUps counts rounds spent on probes and breaker passes.
	followUps int
	// checkProblemsRaised is the set of unrunnable plan checks already
	// reported, so the same list is not sent back twice.
	checkProblemsRaised string
	// planReviewed is the review key of the plan already reviewed.
	planReviewed string
	// images are the latest screenshots the agent took, for the visual
	// review; already within provider limits.
	images []fantasy.FilePart

	// baselines hold each file's content before the request's first edit
	// of it, keyed by absolute path, for the diff guard and the corpus.
	baselines map[string]fileBaseline
	// diffRaised are the diff findings already sent back; diffRounds
	// counts those send-backs and diffNotes keeps every finding of the
	// request for the evaluator.
	diffRaised map[string]bool
	diffRounds int
	diffNotes  []string
	// checklistAsked is set once the agent was asked to confirm each part
	// of a multi-part request.
	checklistAsked bool
	// compacted is set when the session was summarized during the request,
	// so the original wording is shown again.
	compacted bool
	// alternatives are worktrees prepared for a check that keeps failing;
	// alternativesTried is set once they were used, and extraRounds is the
	// contract rounds granted for them.
	alternatives      *contractAlternatives
	alternativesTried bool
	extraRounds       int
	// flaky holds run keys that passed and failed with no edit in between;
	// newFlaky are the commands not yet saved to the plan state.
	flaky    map[string]bool
	newFlaky []string
	// edits summarize each code change in order, and pendingErrors the
	// latest failure of each command, so a failure followed by edits and
	// a pass is learned as a fix. learned are fixes not yet saved.
	edits         []editRecord
	pendingErrors map[string]pendingError
	learned       []learnedFix
	// recalled are the error signatures whose remembered fix was shown.
	recalled map[string]bool
}

// editRecord is one code change, summarized.
type editRecord struct {
	seq     int
	path    string
	summary string
}

// evaluatorMaxEdits bounds the edits remembered per request.
const evaluatorMaxEdits = 200

// evaluatorDecision is what the gate concluded for one finished turn.
// Continue holds the next instruction when the work goes back to the
// agent; Unresolved holds findings left once the round limit is reached.
type evaluatorDecision struct {
	Continue   string
	Unresolved string
}

// SetEvaluatorOptions updates the completion gate in place so a
// chat-driven config change reaches an already-running session's next
// turn. A nil model disables the gate. See coordinator.UpdateModels.
func (a *sessionAgent) SetEvaluatorOptions(evaluatorModel *Model, evaluatorTools []fantasy.AgentTool, maxRounds int) {
	a.evaluatorModel.Set(ptrBox[Model]{v: evaluatorModel})
	a.evaluatorTools.SetSlice(evaluatorTools)
	a.evaluatorRounds.Set(maxRounds)
}

func (a *sessionAgent) evaluatorActive() bool {
	return !a.isSubAgent && a.evaluatorModel.Get().v != nil
}

// completionGateActive reports whether finished turns are checked at all:
// against the work plan's verification contract, which costs no model call
// and is always on, and by the reviewer when one is configured.
func (a *sessionAgent) completionGateActive() bool {
	return !a.isSubAgent && (a.workingDir != "" || a.evaluatorActive())
}

// beginEvaluatorRequest starts a fresh ledger when call carries a new
// request rather than an automatic continuation of the current one.
func (a *sessionAgent) beginEvaluatorRequest(call SessionAgentCall) {
	if !a.completionGateActive() {
		return
	}
	if call.internalContinuation && !call.QueueContinuation {
		if _, ok := a.evaluatorLedgers.Get(call.SessionID); ok {
			return
		}
	}
	if ledger, ok := a.evaluatorLedgers.Get(call.SessionID); ok && !call.internalContinuation {
		ledger.mu.Lock()
		open := ledger.open
		if open {
			ledger.request = addToRequest(ledger.request, userRequestText(call))
		}
		ledger.mu.Unlock()
		if open {
			return
		}
	}
	a.evaluatorLedgers.Set(call.SessionID, &evaluatorLedger{request: userRequestText(call), root: a.projectRoot(call.SessionID)})
}

// userRequestText is what the user asked: the prompt and any text they pasted.
// A long paste arrives as a text attachment with an empty prompt, and the
// checklist, the plan review and the evaluator would otherwise judge the
// work against an empty request.
func userRequestText(call SessionAgentCall) string {
	parts := []string{strings.TrimSpace(call.Prompt)}
	for _, attachment := range call.Attachments {
		if !attachment.IsText() || !strings.HasPrefix(attachment.FileName, "paste_") || len(attachment.Content) > maxPastedRequest {
			continue
		}
		parts = append(parts, strings.TrimSpace(string(attachment.Content)))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// maxPastedRequest bounds the pasted text treated as the request.
const maxPastedRequest = 64 << 10

// requestAdditionMarker introduces what the user added to a request while
// the work on it was still going.
const requestAdditionMarker = "\n\nAdded by the user while the work was in progress:\n"

// addToRequest appends a message sent during the work to its request.
func addToRequest(request, addition string) string {
	addition = strings.TrimSpace(addition)
	if addition == "" {
		return request
	}
	return request + requestAdditionMarker + addition
}

// markRequestOpen records whether the request is still being worked on
// when a turn ends: the gate sent it back, or the user's next message was
// already waiting. A request that ended on its own is closed, so the next
// message starts a new one.
func (a *sessionAgent) markRequestOpen(sessionID string, open bool) {
	if a.evaluatorLedgers == nil {
		return
	}
	ledger, ok := a.evaluatorLedgers.Get(sessionID)
	if !ok {
		return
	}
	ledger.mu.Lock()
	ledger.open = open
	ledger.mu.Unlock()
}

func (a *sessionAgent) evaluatorLedgerFor(sessionID string) *evaluatorLedger {
	if ledger, ok := a.evaluatorLedgers.Get(sessionID); ok {
		return ledger
	}
	ledger := &evaluatorLedger{root: a.projectRoot(sessionID)}
	a.evaluatorLedgers.Set(sessionID, ledger)
	return ledger
}

// recordEvaluatorEvidence adds the files changed and commands run in
// steps to the session's ledger.
func (a *sessionAgent) recordEvaluatorEvidence(sessionID string, steps []fantasy.StepResult) {
	if !a.completionGateActive() {
		return
	}
	ledger := a.evaluatorLedgerFor(sessionID)
	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	ledger.record(steps)
}

func (l *evaluatorLedger) record(steps []fantasy.StepResult) {
	for _, step := range steps {
		results := make(map[string]fantasy.ToolResultContent)
		for _, tr := range step.Content.ToolResults() {
			results[tr.ToolCallID] = tr
		}
		for _, tc := range step.Content.ToolCalls() {
			tr, ok := results[tc.ToolCallID]
			if !ok {
				continue
			}
			_, failed := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentError](tr.Result)
			switch {
			case slices.Contains(evaluatorFileTools, tc.ToolName):
				if failed {
					continue
				}
				path := evaluatorCallPath(tc)
				if path != "" && workplan.IsAtlasPath(path) {
					// Updating the plan records progress, and notes or probe
					// scripts under .atlas are not part of what the checks
					// build: neither needs review nor makes earlier checks
					// stale. A probe script there used to send a passing
					// half-hour e2e suite back to run again.
					continue
				}
				l.seq++
				l.lastEdit = l.seq
				l.changed = true
				if path != "" && !slices.Contains(l.files, path) {
					l.files = append(l.files, path)
				}
				if len(l.edits) < evaluatorMaxEdits {
					l.edits = append(l.edits, editRecord{seq: l.seq, path: path, summary: editSummary(tc)})
				}
			case tc.ToolName == "bash":
				command := evaluatorBashCommand(tc.Input)
				if command == "" {
					continue
				}
				exit, finished := bashExitCode(tr.ClientMetadata)
				l.seq++
				run := evaluatorCommand{
					command: command,
					failed:  failed || exit != 0,
					tail:    evaluatorTail(toolResultOutputString(tr.Result)),
					seq:     l.seq,
					dir:     evaluatorBashDir(tc.Input),
				}
				l.commands = append(l.commands, run)
				if len(l.commands) > evaluatorMaxCommands {
					l.commands = l.commands[len(l.commands)-evaluatorMaxCommands:]
				}
				// Only a command that exited counts for the contract: a
				// background job or a call cut off before its exit status
				// proves nothing either way.
				if finished {
					l.noteFlaky(run)
					l.remember(run)
					l.trackError(run, toolResultOutputString(tr.Result))
				}
			}
			l.keepImage(tr.Result)
		}
	}
}

// keepImage remembers a screenshot a tool returned, newest last, for the
// visual review. Images are repaired to provider limits when kept, so a
// review never fails on an oversized one.
func (l *evaluatorLedger) keepImage(result fantasy.ToolResultOutputContent) {
	media, ok := fantasy.AsToolResultOutputType[fantasy.ToolResultOutputContentMedia](result)
	if !ok || !strings.HasPrefix(media.MediaType, "image/") {
		return
	}
	raw, err := base64.StdEncoding.DecodeString(media.Data)
	if err != nil {
		return
	}
	data, mediaType := tools.RepairOversizedImage(raw, media.MediaType)
	if len(data) > tools.MaxImageDeliveryBytes {
		return
	}
	l.images = append(l.images, fantasy.FilePart{Filename: "screenshot", Data: data, MediaType: mediaType})
	if len(l.images) > evaluatorMaxImages {
		l.images = l.images[len(l.images)-evaluatorMaxImages:]
	}
}

func evaluatorBashDir(input string) string {
	var p tools.BashParams
	if json.Unmarshal([]byte(input), &p) != nil {
		return ""
	}
	return strings.TrimSpace(p.WorkingDir)
}

// noteFlaky marks a command whose result changed with no code change since
// its previous run.
func (l *evaluatorLedger) noteFlaky(run evaluatorCommand) {
	prev, ok := l.runs[runKey(run)]
	if !ok || prev.failed == run.failed || l.lastEdit > prev.seq {
		return
	}
	if l.flaky == nil {
		l.flaky = map[string]bool{}
	}
	if !l.flaky[runKey(run)] {
		l.flaky[runKey(run)] = true
		if !workplan.IsScratchDir(run.dir) {
			l.newFlaky = append(l.newFlaky, run.command)
		}
	}
}

// isFlaky reports whether a run satisfying command flipped in this request.
func (l *evaluatorLedger) isFlaky(command string) bool {
	for key := range l.flaky {
		run, ok := l.runs[key]
		if ok && !workplan.IsScratchDir(run.dir) && workplan.Satisfies(run.command, command) {
			return true
		}
	}
	return false
}

// remember keeps the latest run of each distinct command for the contract
// check, which must see a verify command run early in a long turn even
// after many later commands pushed it out of the review's recent list.
func (l *evaluatorLedger) remember(run evaluatorCommand) {
	if l.runs == nil {
		l.runs = map[string]evaluatorCommand{}
	}
	l.runs[runKey(run)] = run
	if len(l.runs) <= contractMaxRuns {
		return
	}
	oldest := ""
	for command, r := range l.runs {
		if oldest == "" || r.seq < l.runs[oldest].seq {
			oldest = command
		}
	}
	delete(l.runs, oldest)
}

// bashExitCode reads a foreground command's exit code from the bash tool's
// metadata. finished is false for background jobs and for calls that never
// produced an exit status, which prove nothing either way.
func bashExitCode(metadata string) (exit int, finished bool) {
	var meta tools.BashResponseMetadata
	if metadata == "" || json.Unmarshal([]byte(metadata), &meta) != nil || meta.ExitCode == nil || meta.Background {
		return 0, false
	}
	return *meta.ExitCode, true
}

func evaluatorCallPath(tc fantasy.ToolCallContent) string {
	var p struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
		NewPath  string `json:"new_path"`
	}
	if json.Unmarshal([]byte(tc.Input), &p) != nil {
		return ""
	}
	for _, path := range []string{p.FilePath, p.NewPath, p.Path} {
		if path != "" {
			return path
		}
	}
	return ""
}

func evaluatorBashCommand(input string) string {
	var p tools.BashParams
	if json.Unmarshal([]byte(input), &p) != nil {
		return ""
	}
	if p.Command != "" {
		return strings.TrimSpace(p.Command)
	}
	return strings.Join(p.Argv, " ")
}

func evaluatorTail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= evaluatorOutputTail {
		return s
	}
	return "…" + s[len(s)-evaluatorOutputTail:]
}

func evaluatorTruncate(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= evaluatorMaxChars {
		return s
	}
	return s[:evaluatorMaxChars] + "… (truncated)"
}

// review builds the reviewer's prompt from the ledger and the agent's
// final report.
func (l *evaluatorLedger) review(report string) string {
	var sb strings.Builder
	sb.WriteString("User request:\n")
	sb.WriteString(cmp.Or(evaluatorTruncate(l.request), "(not recorded)"))
	sb.WriteString("\n\nAgent's final report:\n")
	sb.WriteString(cmp.Or(evaluatorTruncate(report), "(empty)"))
	sb.WriteString("\n\nFiles the agent changed:\n")
	if len(l.files) == 0 {
		sb.WriteString("(none recorded; use git_status)\n")
	}
	for _, f := range l.files {
		sb.WriteString("- " + f + "\n")
	}
	sb.WriteString("\nCommands the agent ran, oldest first:\n")
	if len(l.commands) == 0 {
		sb.WriteString("(none)\n")
	}
	for _, c := range l.commands {
		status := "ok"
		if c.failed {
			status = "failed"
		}
		fmt.Fprintf(&sb, "- [%s] %s\n", status, c.command)
		if c.tail != "" {
			sb.WriteString("  output tail: " + strings.ReplaceAll(c.tail, "\n", "\n  ") + "\n")
		}
	}
	if items := requestItems(l.request); len(items) > 0 {
		sb.WriteString("\nRequirement checklist from the request:\n")
		for i, item := range items {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, item)
		}
	}
	if len(l.diffNotes) > 0 {
		sb.WriteString("\nAutomatic diff checks raised these; the agent was asked to undo or justify each:\n")
		for _, note := range l.diffNotes[:min(len(l.diffNotes), diffGuardMaxShown)] {
			sb.WriteString("- " + note + "\n")
		}
	}
	if l.lastFailed && l.findings != "" {
		fmt.Fprintf(&sb, "\nYour previous verdict (round %d) failed the work with:\n%s\nCheck whether each finding is now fixed or credibly refuted by the agent.\n", l.rounds, l.findings)
	}
	return sb.String()
}

// parseEvaluatorVerdict reads the reviewer's reply. ok is false for a
// reply that does not follow the format, which the gate treats as no
// verdict. A FAIL with no findings is not actionable and counts as PASS.
func parseEvaluatorVerdict(reply string) (pass bool, findings string, ok bool) {
	lines := strings.Split(strings.TrimSpace(reply), "\n")
	for i, line := range lines {
		rest, found := strings.CutPrefix(strings.ToUpper(strings.TrimSpace(line)), "VERDICT:")
		if !found {
			continue
		}
		switch strings.TrimSpace(rest) {
		case "PASS":
			return true, "", true
		case "FAIL":
			findings = strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
			if findings == "" {
				// A reviewer that only marked requirements missing in the
				// checklist still gave actionable findings.
				findings = unmetRequirements(lines[:i])
			}
			return findings == "", findings, true
		default:
			return false, "", false
		}
	}
	return false, "", false
}

// evaluateTurn runs the gate for a turn that ended normally. It returns
// the zero decision when the turn needs no review or the review passes.
func (a *sessionAgent) evaluateTurn(ctx context.Context, sessionID, report string) evaluatorDecision {
	if !a.completionGateActive() {
		return evaluatorDecision{}
	}
	ledger := a.evaluatorLedgerFor(sessionID)
	ledger.mu.Lock()
	a.saveHarnessLearning(sessionID, ledger)
	if !ledger.changed && !ledger.lastFailed {
		ledger.mu.Unlock()
		return evaluatorDecision{}
	}
	if ledger.changed {
		if decision, stop := a.reviewDiff(sessionID, ledger); stop {
			ledger.mu.Unlock()
			return decision
		}
	}
	if decision, stop := a.checkContract(ctx, sessionID, ledger); stop {
		if decision.Unresolved != "" {
			a.saveFailureCase(ctx, sessionID, ledger, "contract", decision.Unresolved)
		}
		ledger.mu.Unlock()
		return decision
	}
	if !a.evaluatorActive() {
		if decision, stop := a.confirmChecklist(sessionID, ledger); stop {
			ledger.mu.Unlock()
			return decision
		}
		ledger.changed = false
		ledger.mu.Unlock()
		return evaluatorDecision{}
	}
	review := ledger.review(report)
	images := slices.Clone(ledger.images)
	ledger.mu.Unlock()

	reply, err := a.runEvaluator(ctx, sessionID, review, images)
	if err != nil {
		slog.Warn("Evaluator review failed; ending the turn without it", "session_id", sessionID, "error", err)
		return evaluatorDecision{}
	}
	pass, findings, ok := parseEvaluatorVerdict(reply)
	if !ok {
		slog.Warn("Evaluator reply had no verdict; ending the turn without it", "session_id", sessionID)
		return evaluatorDecision{}
	}

	ledger.mu.Lock()
	defer ledger.mu.Unlock()
	ledger.changed = false
	ledger.lastFailed = !pass
	ledger.findings = findings
	if pass {
		a.recordHarness(harnessmetrics.EvaluatorPass, sessionID, nil)
		slog.Info("Evaluator accepted the turn", "session_id", sessionID, "rounds", ledger.rounds)
		return evaluatorDecision{}
	}
	a.recordHarness(harnessmetrics.EvaluatorFail, sessionID, nil)
	limit := a.evaluatorRounds.Get()
	if limit <= 0 {
		limit = 2
	}
	if ledger.rounds >= limit {
		slog.Warn("Evaluator findings remain after the round limit", "session_id", sessionID, "rounds", ledger.rounds)
		ledger.lastFailed = false
		a.saveFailureCase(ctx, sessionID, ledger, "evaluator", findings)
		return evaluatorDecision{Unresolved: findings}
	}
	ledger.rounds++
	slog.Info("Evaluator sent the turn back", "session_id", sessionID, "round", ledger.rounds)
	return evaluatorDecision{Continue: evaluatorContinuationPrompt(findings, ledger.rounds, limit)}
}

func evaluatorContinuationPrompt(findings string, round, limit int) string {
	return fmt.Sprintf("An independent evaluator reviewed the code for this request and did not accept it (review %d of %d):\n\n%s\n\n"+
		"Fix each finding that holds and re-run the checks that cover it. If a finding is wrong, show the concrete evidence "+
		"(file and line, or command output) instead of changing code. Then give your final report again.", round, limit, findings)
}

// runEvaluator streams one review on the evaluator model with a fresh
// context and the read-only toolset, charged to the session's budget. With
// screenshots from the turn it also judges what the user will see, against
// the visual rubric.
func (a *sessionAgent) runEvaluator(ctx context.Context, sessionID, review string, images []fantasy.FilePart) (string, error) {
	model := a.evaluatorModel.Get().v
	if model == nil {
		return "", fmt.Errorf("evaluator has no model")
	}
	system := evaluatorSystemPrompt
	if len(images) > 0 && model.CatwalkCfg.SupportsImages {
		system += "\n\n" + visualReviewRubric
		review += fmt.Sprintf("\n\nThe %d attached image(s) are the latest screenshots the agent took of its work, oldest first.", len(images))
		a.recordHarness(harnessmetrics.VisualReview, sessionID, map[string]string{"images": fmt.Sprint(len(images))})
	} else {
		images = nil
	}
	return a.runReviewer(ctx, sessionID, *model, system, review, images, a.evaluatorTools.Copy(), evaluatorMaxSteps)
}

// runReviewer streams one independent review: a fresh context, the given
// system prompt and read-only tools, charged to the session's budget. It
// is shared by the evaluator and the plan review.
func (a *sessionAgent) runReviewer(ctx context.Context, sessionID string, model Model, system, prompt string, files []fantasy.FilePart, reviewTools []fantasy.AgentTool, maxSteps int) (reply string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("reviewer panicked: %v", r)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, evaluatorTimeout)
	defer cancel()

	scope := engineering.GetScope(ctx, sessionID)
	ctx = engineering.WithScope(ctx, scope.SessionID, scope.TaskID)
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	ctx, finish, err := a.engineeringContext(ctx, sessionID)
	if err != nil {
		return "", fmt.Errorf("execution budget: %w", err)
	}
	defer finish()

	agent := fantasy.NewAgent(
		model.Model,
		fantasy.WithSystemPrompt(system),
		fantasy.WithTools(guardTools(reviewTools, a.engineering)...),
		fantasy.WithStopConditions(fantasy.StepCountIs(maxSteps)),
		fantasy.WithUserAgent(userAgent),
	)
	call := a.engineeringReviewCall(ctx, sessionID, model, prompt)
	call.Files = files
	result, err := agent.Stream(ctx, call)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", fmt.Errorf("reviewer returned no result")
	}
	return result.Response.Content.Text(), nil
}

// visualReviewRubric is added to the evaluator's instructions when the turn
// produced screenshots. It follows the criteria that separated distinctive
// work from template output in Anthropic's frontend experiments: identity
// and originality weigh most, because competent craft and working controls
// are what a model produces by default.
const visualReviewRubric = `Screenshots of the result are attached. Judge what the user will actually see, in addition to the code:
- Design quality (weighs most): do color, type, layout and imagery form one coherent whole with a clear identity and hierarchy, or a collection of parts?
- Originality (weighs most): are there deliberate choices fitted to this product, or template defaults (stock gradient hero, identical card grids, generic icons, placeholder copy)?
- Craft: type scale, spacing rhythm, alignment, contrast and color harmony; text that overflows, overlaps, is clipped or unreadable.
- Function: can a user tell what this is, find the primary action and complete it; are empty, loading and error states handled where they are visible?
A visible defect (broken layout, overlap, cut-off or unreadable text, an obviously wrong or missing state, a blank area where content belongs) is a material problem and fails the work. Weak design quality or originality alone fails it only when the request asked for a polished or distinctive design; then say concretely what to change, not just that it is generic.`
