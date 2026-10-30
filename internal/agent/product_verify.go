package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/harnessmetrics"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/webverify"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workplan"
)

// Product verification is a completion gate for web and UI work. Where the
// evaluator reads code, this gate opens the running app in Atlas's own
// browser tool, measures it at three viewports and drives the user flows,
// and sends what it finds back to the agent as fix tasks. Every finding that
// blocks carries evidence (a screenshot, a DOM excerpt, a console line or a
// request), so the agent is never asked to fix an opinion.
//
// Like the other gates it fails open: no browser, a denied permission, no
// running app or a crashed check lets the turn end as it would have without
// the gate, and the reason is logged.

const (
	// productVerifyTimeout bounds one verification run.
	productVerifyTimeout = 5 * time.Minute

	harnessProductPass     harnessmetrics.Kind = "product_verify_pass"
	harnessProductSentBack harnessmetrics.Kind = "product_verify_sent_back"
	harnessProductUnsolved harnessmetrics.Kind = "product_verify_unresolved"
)

func (a *sessionAgent) productVerifyOptions() *config.ProductVerify {
	if a.taskContextConfig == nil {
		return nil
	}
	if cfg := a.taskContextConfig.Config(); cfg != nil && cfg.Options != nil {
		return cfg.Options.ProductVerify
	}
	return nil
}

// productVerifyDecision is why a verification would or would not run.
type productVerifyPlan struct {
	url   string
	spec  *webverify.Spec
	flows []webverify.Flow
	// unreachable is set when the address was configured or written in the
	// spec but nothing answers there.
	unreachable bool
}

// planProductVerify decides whether to verify and what. It returns nil when
// the gate does not apply to this turn. It reads files and probes the
// network, so the caller must not hold the ledger lock.
func planProductVerify(ctx context.Context, opts *config.ProductVerify, root, request string, files []string, commandTexts []string) (*productVerifyPlan, error) {
	mode := opts.VerifyMode()
	if mode == config.ProductVerifyOff {
		return nil, nil
	}
	spec, err := webverify.LoadSpec(root)
	if err != nil {
		return nil, err
	}
	hasFlows := spec != nil && len(spec.Flows) > 0
	if mode == config.ProductVerifyAuto && !hasFlows && !webverify.IsWebTask(request, files) {
		return nil, nil
	}
	explicit := ""
	if opts != nil {
		explicit = strings.TrimSpace(opts.URL)
	}
	if explicit == "" && spec != nil {
		explicit = spec.URL
	}
	url := explicit
	if url == "" {
		url = webverify.DiscoverURL(append([]string{request}, commandTexts...)...)
	}
	if url == "" {
		return nil, nil
	}
	if _, err := webverify.CheckLocal(url); err != nil {
		if explicit != "" {
			return nil, err
		}
		return nil, nil
	}
	plan := &productVerifyPlan{url: url, spec: spec}
	if !webverify.Reachable(ctx, url) {
		plan.unreachable = explicit != ""
		if !plan.unreachable {
			return nil, nil
		}
		return plan, nil
	}
	var hard []webverify.Flow
	if spec != nil {
		hard = spec.Flows
	}
	plan.flows = webverify.MergeFlows(hard, webverify.DeriveFlows(request, planCriteria(root)))
	return plan, nil
}

// planCriteria reads the work plan's features as acceptance criteria.
func planCriteria(root string) []webverify.Criterion {
	plan, _, err := workplan.Load(root)
	if err != nil || plan == nil {
		return nil
	}
	out := make([]webverify.Criterion, 0, len(plan.Features))
	for _, f := range plan.Features {
		out = append(out, webverify.Criterion{ID: f.ID, Title: f.Title, Acceptance: f.Acceptance})
	}
	return out
}

// checkProduct runs the gate for a finished turn. The caller holds
// ledger.mu, which is released while the browser runs and held again on
// return. stop is true when the decision is final for this turn.
func (a *sessionAgent) checkProduct(ctx context.Context, sessionID string, ledger *evaluatorLedger) (evaluatorDecision, bool) {
	opts := a.productVerifyOptions()
	if opts.VerifyMode() == config.ProductVerifyOff || a.isSubAgent || !ledger.changed {
		return evaluatorDecision{}, false
	}
	editMark := ledger.lastEdit + 1
	if ledger.productChecked == editMark {
		return evaluatorDecision{}, false
	}
	root := a.ledgerRoot(ledger)
	request := ledger.request
	files := slices.Clone(ledger.files)
	var texts []string
	for _, c := range ledger.commands {
		texts = append(texts, c.command+"\n"+c.tail)
	}
	round := ledger.productRounds
	limit := opts.Rounds()

	ledger.mu.Unlock()
	decision, stop, note := a.runProductGate(ctx, sessionID, opts, root, request, files, texts, round, limit)
	ledger.mu.Lock()

	if note != "" {
		ledger.productNote = note
	}
	switch {
	case !stop && decision.Continue == "" && decision.Unresolved == "":
		if note != "" {
			// A run that passed need not repeat for the same code.
			ledger.productChecked = editMark
		}
		return decision, false
	case decision.Continue != "":
		ledger.productRounds++
	default:
		// Findings remain after the round limit: do not run again for
		// the same code.
		ledger.productChecked = editMark
	}
	return decision, true
}

// runProductGate plans and runs one verification, unlocked.
func (a *sessionAgent) runProductGate(ctx context.Context, sessionID string, opts *config.ProductVerify, root, request string, files, texts []string, round, limit int) (evaluatorDecision, bool, string) {
	plan, err := planProductVerify(ctx, opts, root, request, files, texts)
	if err != nil {
		// A spec or address that cannot be used is the agent's to fix.
		return a.productSendBack(sessionID, round, limit, "Product verification cannot start: "+err.Error()+
			"\nFix "+webverify.SpecFile+" (or the product_verify.url setting) so it describes a local app, then give your final report again.")
	}
	if plan == nil {
		return evaluatorDecision{}, false, ""
	}
	if plan.unreachable {
		return a.productSendBack(sessionID, round, limit, fmt.Sprintf("Product verification is enabled, but nothing answers at %s. "+
			"Start the dev server (keep it running in the background) so the app can be opened in the browser, then give your final report again.", plan.url))
	}
	browserTool := a.findTool(tools.BrowserToolName)
	if browserTool == nil {
		slog.Info("Product verification skipped: the browser tool is not available", "session_id", sessionID)
		return evaluatorDecision{}, false, ""
	}

	ctx, cancel := context.WithTimeout(ctx, productVerifyTimeout)
	defer cancel()
	scope := engineering.GetScope(ctx, sessionID)
	ctx = engineering.WithScope(ctx, scope.SessionID, scope.TaskID)
	ctx = context.WithValue(ctx, tools.SessionIDContextKey, sessionID)
	ctx = context.WithValue(ctx, tools.SupportsImagesContextKey, true)

	invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		return browserTool.Run(ctx, call)
	}
	driver, closeTab, err := tools.NewWebVerifyDriver(ctx, invoke, "verify-"+sessionID, plan.url)
	if err != nil {
		slog.Info("Product verification skipped: the browser cannot be used", "session_id", sessionID, "error", err)
		return evaluatorDecision{}, false, ""
	}
	defer closeTab()

	runOpts := webverify.Options{
		URL:    plan.url,
		Flows:  plan.flows,
		Driver: driver,
		Root:   root,
		Axe:    webverify.FindAxe(root, opts.AxePathOrEmpty()),
		Budgets: webverify.Budgets{
			ConsoleErrors:  opts.ConsoleErrorBudget(),
			FailedRequests: opts.FailedRequestBudget(),
			LoadMS:         opts.LoadBudget(),
		},
	}
	if plan.spec != nil {
		runOpts.Pages = plan.spec.Pages
	}
	if opts.VisionEnabled() {
		if model, ok := a.productVisionModel(); ok {
			runOpts.Vision = productVision{agent: a, sessionID: sessionID, model: model}
			a.recordHarness(harnessmetrics.VisualReview, sessionID, map[string]string{"source": "product_verify"})
		}
	}

	report, err := webverify.Run(ctx, runOpts)
	if err != nil {
		slog.Warn("Product verification did not finish; ending the turn without it", "session_id", sessionID, "error", err)
		return evaluatorDecision{}, false, ""
	}
	threshold := webverify.ParseSeverity(opts.BlockSeverity(), webverify.SeverityMedium)
	if err := report.Save(root, threshold); err != nil {
		slog.Warn("Failed to save the product verification report", "error", err)
	}
	note := report.Summary(threshold)
	if report.Dir != "" {
		note += " Evidence: " + report.Dir + "/report.md"
	}
	if report.Blocking(threshold) == nil {
		a.recordHarness(harnessProductPass, sessionID, nil)
		slog.Info("Product verification passed", "session_id", sessionID, "findings", len(report.Findings))
		return evaluatorDecision{}, false, note
	}
	if round >= limit {
		a.recordHarness(harnessProductUnsolved, sessionID, nil)
		slog.Warn("Product verification findings remain after the round limit", "session_id", sessionID)
		return evaluatorDecision{Unresolved: productUnresolved(report, threshold)}, true, note
	}
	a.recordHarness(harnessProductSentBack, sessionID, map[string]string{"round": fmt.Sprint(round + 1)})
	return evaluatorDecision{Continue: report.FixPrompt(threshold, round+1, limit)}, true, note
}

// productSendBack turns a setup problem into the agent's next instruction,
// bounded by the same round limit as findings.
func (a *sessionAgent) productSendBack(sessionID string, round, limit int, prompt string) (evaluatorDecision, bool, string) {
	if round >= limit {
		slog.Warn("Product verification could not run after the round limit", "session_id", sessionID)
		return evaluatorDecision{Unresolved: "Product verification could not run:\n" + prompt}, true, ""
	}
	a.recordHarness(harnessProductSentBack, sessionID, map[string]string{"round": fmt.Sprint(round + 1), "setup": "true"})
	return evaluatorDecision{Continue: prompt}, true, ""
}

// productUnresolved is the text that ends the turn when findings remain.
func productUnresolved(report *webverify.Report, threshold webverify.Severity) string {
	var b strings.Builder
	b.WriteString("Product verification findings remain:\n")
	blocking := report.Blocking(threshold)
	for _, f := range blocking[:min(len(blocking), 8)] {
		fmt.Fprintf(&b, "- [%s] %s (%s)\n", f.Severity, f.Title, strings.Join(f.Viewports, ", "))
	}
	if report.Dir != "" {
		b.WriteString("Evidence: " + report.Dir + "/report.md\n")
	}
	return strings.TrimSpace(b.String())
}

// findTool returns the session's tool with the given name.
func (a *sessionAgent) findTool(name string) fantasy.AgentTool {
	for _, tool := range a.tools.Copy() {
		if tool.Info().Name == name {
			return tool
		}
	}
	return nil
}

// productVisionModel picks the model that reviews screenshots: the
// evaluator's when it accepts images, else the session's large model.
func (a *sessionAgent) productVisionModel() (Model, bool) {
	if m := a.evaluatorModel.Get().v; m != nil && m.CatwalkCfg.SupportsImages {
		return *m, true
	}
	if m := a.largeModel.Get(); m.CatwalkCfg.SupportsImages {
		return m, true
	}
	return Model{}, false
}

// productVision reviews screenshots on a vision-capable model with a fresh
// context and no tools.
type productVision struct {
	agent     *sessionAgent
	sessionID string
	model     Model
}

func (v productVision) Review(ctx context.Context, shots []webverify.Shot) (string, error) {
	files := make([]fantasy.FilePart, 0, len(shots))
	for _, shot := range shots {
		data, mediaType := tools.RepairOversizedImage(shot.Data, shot.MediaType)
		if len(data) > tools.MaxImageDeliveryBytes {
			return "", errors.New("a screenshot is too large to review")
		}
		files = append(files, fantasy.FilePart{Filename: shot.Viewport, Data: data, MediaType: mediaType})
	}
	return v.agent.runReviewer(ctx, v.sessionID, v.model, webverify.VisionSystemPrompt, webverify.VisionPrompt(shots), files, nil, 1)
}
