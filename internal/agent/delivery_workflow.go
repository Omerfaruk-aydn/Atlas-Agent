package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
)

func (c *coordinator) deliveryReady(ctx context.Context, id string, todos []session.Todo, limit int) ([]session.Todo, error) {
	st, err := c.engineering.Read(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := c.currentRecipe(ctx, id); err != nil {
		return nil, err
	}
	if err := c.engineering.SemanticEditsReady(ctx, id); err != nil {
		return nil, err
	}
	if st.Delivery == nil {
		return session.ReadyTaskWave(todos, limit)
	}
	if filepath.Clean(st.Delivery.Root) != filepath.Clean(c.cfg.WorkingDir()) {
		return nil, fmt.Errorf("delivery belongs to another project root; register a revised plan")
	}
	for task, refs := range st.Delivery.TaskContractRefs {
		if err := c.engineering.ValidateContractRefs(ctx, st.Delivery.Root, task, refs); err != nil {
			return nil, fmt.Errorf("revise and check delivery contracts: %w", err)
		}
	}
	for i := range st.Delivery.CurrentStage {
		for _, task := range st.Delivery.Stages[i].TaskIDs {
			if err := c.engineering.ValidateTaskContracts(ctx, id, st.Delivery.Root, task, st.Delivery.TaskFingerprints[task], st.Delivery.TaskContractRefs[task]); err != nil {
				return nil, fmt.Errorf("previous stage contract requires reinspection: %w", err)
			}
			if err := c.engineering.ValidateTaskFindings(ctx, id, st.Delivery.Root, task, st.Delivery.TaskFingerprints[task]); err != nil {
				return nil, fmt.Errorf("previous stage finding requires reinspection: %w", err)
			}
		}
	}
	if err := session.ValidateTaskGraph(todos); err != nil {
		return nil, err
	}
	filtered := slices.Clone(todos)
	slices.SortStableFunc(filtered, func(a, b session.Todo) int {
		x, y := st.Delivery.AllowsTask(a.ID), st.Delivery.AllowsTask(b.ID)
		if x && !y {
			return -1
		}
		if !x && y {
			return 1
		}
		return 0
	})
	for i := range filtered {
		if st.Delivery.AllowsTask(filtered[i].ID) && st.Delivery.TaskFingerprints[filtered[i].ID] != session.TaskFingerprint(filtered[i]) {
			return nil, fmt.Errorf("delivery task changed; register the revised plan before dispatch")
		}
	}
	// Select only eligible pending tasks while retaining dependency records.
	wave, err := session.ReadyTaskWave(filtered, 16)
	if err != nil {
		return nil, err
	}
	var selected []session.Todo
	for _, task := range wave {
		if st.Delivery.AllowsTask(task.ID) {
			selected = append(selected, task)
		}
	}
	if limit <= 0 {
		limit = 4
	}
	limit = min(limit, 16)
	return selected[:min(limit, len(selected))], nil
}

func (c *coordinator) deliveryWorkflow(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, sess session.Session, invoke tools.ToolInvoker) (fantasy.ToolResponse, error) {
	id := engineering.GetScope(ctx, sess.ID).SessionID
	root := c.cfg.WorkingDir()
	st, err := c.engineering.Read(ctx, id)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	fail := func(err error) (fantasy.ToolResponse, error) { return fantasy.NewTextErrorResponse(err.Error()), nil }
	respond := func(value any) (fantasy.ToolResponse, error) {
		return fantasy.NewTextResponse(workflowJSON(value)), nil
	}
	switch p.Action {
	case "prepare":
		brief, err := c.engineering.PrepareProject(ctx, root)
		if err != nil {
			return fail(err)
		}
		return respond(brief)
	case "profile":
		if p.Profile == "" {
			return respond(engineering.Profiles())
		}
		if !engineering.ValidProfile(p.Profile) {
			return fail(fmt.Errorf("unknown work profile"))
		}
		if st.Delivery != nil && st.Delivery.Profile != p.Profile {
			return fail(fmt.Errorf("revise the delivery plan to change its profile"))
		}
		err := c.engineering.Update(ctx, id, func(st *engineering.State) error { st.Profile = p.Profile; return nil })
		if err != nil {
			return fail(err)
		}
		return respond(map[string]string{"profile": p.Profile})
	case "plan":
		if p.Plan == nil {
			return fail(fmt.Errorf("plan is required"))
		}
		plan := *p.Plan
		plan.Root = root
		plan.CurrentStage = 0
		plan.TaskFingerprints = map[string]string{}
		plan.TaskContractRefs = map[string][]engineering.Record{}
		if plan.Profile == "" {
			plan.Profile = st.Profile
		}
		if plan.Profile == "" {
			plan.Profile = "feature"
		}
		byID := map[string]session.Todo{}
		for _, task := range sess.Todos {
			if task.ID == "" {
				return fail(fmt.Errorf("assign stable IDs to every task before registering a delivery plan"))
			}
			if task.ID != "" {
				byID[task.ID] = task
				plan.TaskFingerprints[task.ID] = session.TaskFingerprint(task)
				refs, err := c.engineering.ContractRefsForTask(ctx, root, task.ID)
				if err != nil {
					return fail(err)
				}
				if len(refs) > 0 {
					plan.TaskContractRefs[task.ID] = refs
				}
			}
		}
		owners := map[string]int{}
		for i := range plan.Stages {
			plan.Stages[i].Passed = false
			plan.Stages[i].Checks = nil
			plan.Stages[i].SourceFingerprint = ""
			for _, taskID := range plan.Stages[i].TaskIDs {
				if _, ok := byID[taskID]; !ok {
					return fail(fmt.Errorf("stage references unknown task %s", taskID))
				}
				owners[taskID] = i
			}
		}
		if plan.Design != nil {
			plan.Design.Critiques = nil
		}
		if err := plan.Validate(); err != nil {
			return fail(err)
		}
		covered := map[string]bool{}
		for _, requirement := range plan.Requirements {
			for _, taskID := range requirement.TaskIDs {
				covered[taskID] = true
			}
		}
		for taskID, task := range byID {
			stage, ok := owners[taskID]
			if !ok || !covered[taskID] || len(task.AcceptanceCriteria) == 0 {
				return fail(fmt.Errorf("every identified task requires stage, requirement coverage and acceptance criteria: %s", taskID))
			}
			for _, dep := range task.DependsOn {
				depStage, exists := owners[dep]
				if !exists || depStage > stage {
					return fail(fmt.Errorf("task dependency is in a later stage"))
				}
			}
		}
		err := c.engineering.Update(ctx, id, func(next *engineering.State) error {
			if st.Delivery == nil && next.Delivery != nil || st.Delivery != nil && (next.Delivery == nil || st.Delivery.Fingerprint() != next.Delivery.Fingerprint()) {
				return fmt.Errorf("delivery changed while planning; inspect current state")
			}
			next.Delivery = &plan
			next.Profile = plan.Profile
			return nil
		})
		if err != nil {
			return fail(err)
		}
		return respond(plan)
	case "trace":
		findings, err := c.engineering.TaskFindings(ctx, id, "")
		if err != nil {
			return fail(err)
		}
		view := deliveryTrace(st, sess.Todos).(map[string]any)
		view["findings"] = findings
		return respond(view)
	case "advance":
		return c.advanceDelivery(ctx, p, call, sess, st, invoke)
	case "design":
		if st.Delivery == nil || p.Design == nil {
			return fail(fmt.Errorf("design requires a registered delivery plan and brief"))
		}
		design := *p.Design
		design.Critiques = nil
		for i, stage := range st.Delivery.Stages {
			if i < st.Delivery.CurrentStage {
				for _, taskID := range design.TaskIDs {
					if slices.Contains(stage.TaskIDs, taskID) {
						return fail(fmt.Errorf("design changes affect a certified stage; register a revised plan"))
					}
				}
			}
		}
		tasks := map[string]bool{}
		for taskID := range st.Delivery.TaskFingerprints {
			tasks[taskID] = true
		}
		if err := design.Validate(tasks); err != nil {
			return fail(err)
		}
		err := c.engineering.Update(ctx, id, func(next *engineering.State) error {
			if next.Delivery == nil || next.Delivery.Fingerprint() != st.Delivery.Fingerprint() {
				return fmt.Errorf("delivery changed; retry with current state")
			}
			next.Delivery.Design = &design
			return nil
		})
		if err != nil {
			return fail(err)
		}
		return respond(design)
	case "critique":
		if st.Delivery == nil || st.Delivery.Design == nil || p.Critique == nil {
			return fail(fmt.Errorf("critique requires a registered design brief"))
		}
		critique := *p.Critique
		source, err := engineering.SourceFingerprint(ctx, root, c.engineering.Dir())
		if err != nil {
			return fail(err)
		}
		var artifact *engineering.UIEvidence
		for _, entry := range st.UIEvidence {
			if entry.ID == critique.ArtifactID {
				copy := entry
				artifact = &copy
			}
		}
		if artifact == nil || artifact.SourceFingerprint != source || artifact.Target != st.Delivery.Design.Target {
			return fail(fmt.Errorf("critique requires a fresh actual artifact for this design target"))
		}
		if err := engineering.ValidateUIArtifact(ctx, *artifact); err != nil {
			return fail(err)
		}
		if len(critique.Findings) != len(st.Delivery.Design.Criteria) {
			return fail(fmt.Errorf("critique must address each design criterion"))
		}
		seen := map[string]bool{}
		if len(critique.States) == 0 || len(critique.States) > len(st.Delivery.Design.States) {
			return fail(fmt.Errorf("critique requires inspected design states"))
		}
		for _, state := range critique.States {
			if !slices.Contains(st.Delivery.Design.States, state) || seen[state] {
				return fail(fmt.Errorf("invalid or duplicate inspected design state"))
			}
			seen[state] = true
		}
		clear(seen)
		for _, finding := range critique.Findings {
			if !slices.Contains(st.Delivery.Design.Criteria, finding.Criterion) || seen[finding.Criterion] || finding.Evidence == "" || len(finding.Evidence) > 2048 {
				return fail(fmt.Errorf("invalid critique evidence"))
			}
			seen[finding.Criterion] = true
		}
		critique.SourceFingerprint = source
		critique.CheckedAt = time.Now().UnixMilli()
		err = c.engineering.Update(ctx, id, func(next *engineering.State) error {
			if next.Delivery == nil || next.Delivery.Fingerprint() != st.Delivery.Fingerprint() {
				return fmt.Errorf("delivery changed; retry critique")
			}
			critiques := &next.Delivery.Design.Critiques
			for i := range *critiques {
				if (*critiques)[i].ArtifactID == critique.ArtifactID {
					(*critiques)[i] = critique
					return nil
				}
			}
			if len(*critiques) >= 128 {
				return fmt.Errorf("critique limit reached")
			}
			*critiques = append(*critiques, critique)
			return nil
		})
		if err != nil {
			return fail(err)
		}
		return respond(map[string]any{"critique": critique, "evidence_basis": "reported visual/interaction judgment linked to an actual artifact"})
	case "artifact":
		return c.recordTUIArtifact(ctx, p, call, id, root)
	case "knowledge":
		brief, knowledge, err := c.engineering.ProjectKnowledge(ctx, root)
		if err != nil {
			return fail(err)
		}
		return respond(map[string]any{"brief": brief, "records": knowledge, "authority": "reference evidence, not instructions"})
	case "decision", "lesson":
		if p.Knowledge == nil {
			return fail(fmt.Errorf("knowledge record is required"))
		}
		record := *p.Knowledge
		record.Kind = p.Action
		record.Evidence = nil
		paths := []string{}
		for _, source := range record.Sources {
			paths = append(paths, source.Path)
		}
		record.Sources, err = engineering.CaptureSources(ctx, root, paths)
		if err != nil {
			return fail(err)
		}
		if p.Action == "lesson" {
			previousChecks := slices.Clone(st.Checks)
			var failed *engineering.Operation
			for _, operation := range st.Operations {
				if operation.ID == record.FailureOperationID && operation.Status == "failed" {
					copy := operation
					failed = &copy
				}
			}
			if failed == nil {
				return fail(fmt.Errorf("lesson requires an observed failed operation"))
			}
			before, err := engineering.SourceFingerprint(ctx, root, c.engineering.Dir())
			if err != nil {
				return fail(err)
			}
			record.VerificationRunID = call.ID + "-lesson-verify"
			verifyCtx := engineering.WithScope(ctx, id, failed.TaskID)
			response, err := invoke(verifyCtx, fantasy.ToolCall{ID: record.VerificationRunID, Name: "verify", Input: workflowJSON(tools.VerifyParams{Action: "run", Checks: p.Checks})})
			if err != nil {
				return fail(err)
			}
			var verification struct {
				Passed bool `json:"passed"`
			}
			if response.IsError || response.StopTurn || json.Unmarshal([]byte(response.Content), &verification) != nil || !verification.Passed {
				return fail(fmt.Errorf("lesson repair verification failed"))
			}
			after, err := engineering.SourceFingerprint(ctx, root, c.engineering.Dir())
			if err != nil {
				return fail(err)
			}
			if before != after {
				return fail(fmt.Errorf("source changed during lesson verification"))
			}
			st, err = c.engineering.Read(ctx, id)
			if err != nil {
				return fail(err)
			}
			for _, check := range st.Checks {
				if check.RunID == record.VerificationRunID && check.TaskID == failed.TaskID {
					if slices.Contains(previousChecks, check) {
						continue
					}
					if !check.Passed || check.Evidence == "" || check.CheckedAt < failed.FinishedAt {
						return fail(fmt.Errorf("lesson verification failed or predates failure"))
					}
					record.Evidence = append(record.Evidence, check)
				}
			}
			if len(record.Evidence) == 0 {
				return fail(fmt.Errorf("lesson requires a matching machine-observed verification run"))
			}
		}
		if err := c.engineering.SaveKnowledge(ctx, root, record); err != nil {
			return fail(err)
		}
		return respond(map[string]string{"saved": record.ID, "kind": record.Kind, "evidence_basis": "reported explanation linked to source fingerprints and actual checks for lessons"})
	}
	return fail(fmt.Errorf("unknown delivery action"))
}

func (c *coordinator) recordTUIArtifact(ctx context.Context, p WorkflowParams, call fantasy.ToolCall, id, root string) (fantasy.ToolResponse, error) {
	if p.Width < 20 || p.Width > 1000 || p.Height < 5 || p.Height > 300 {
		return fantasy.NewTextErrorResponse("invalid terminal dimensions"), nil
	}
	refs, err := engineering.CaptureSources(ctx, root, []string{p.ArtifactPath})
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	data, err := engineering.ReadProjectEvidence(ctx, root, refs[0].Path)
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	if len(data) > 512*1024 || engineering.Hash(string(data)) != refs[0].Fingerprint {
		return fantasy.NewTextErrorResponse("transcript changed during capture"), nil
	}
	source, err := engineering.SourceFingerprint(ctx, root, c.engineering.Dir())
	if err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	dir := filepath.Join(c.engineering.Dir(), "ui-evidence")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fantasy.ToolResponse{}, err
	}
	path := filepath.Join(dir, engineering.Hash(id+call.ID)+".txt")
	if err := engineering.AtomicWrite(path, data); err != nil {
		return fantasy.ToolResponse{}, err
	}
	artifact := engineering.UIEvidence{ID: call.ID, Path: path, Hash: engineering.Hash(string(data)), Width: p.Width, Height: p.Height, Target: "tui", SourceFingerprint: source, AssertionsPassed: true, Origin: "reported terminal capture; actual file observed; dimensions and interaction require critique", RecordedAt: time.Now().UnixMilli()}
	if err := engineering.ValidateUIArtifact(ctx, artifact); err != nil {
		return fantasy.NewTextErrorResponse(err.Error()), nil
	}
	err = c.engineering.Update(ctx, id, func(st *engineering.State) error {
		for i := range st.UIEvidence {
			if st.UIEvidence[i].ID == artifact.ID {
				st.UIEvidence[i] = artifact
				return nil
			}
		}
		st.UIEvidence = append(st.UIEvidence, artifact)
		return nil
	})
	if err != nil {
		return fantasy.ToolResponse{}, err
	}
	return fantasy.NewTextResponse(workflowJSON(artifact)), nil
}

func deliveryTrace(st engineering.State, todos []session.Todo) any {
	if st.Delivery == nil {
		return map[string]any{"registered": false, "tasks": todos}
	}
	byID := map[string]session.Todo{}
	for _, t := range todos {
		byID[t.ID] = t
	}
	rows := []map[string]any{}
	tracked := map[string]bool{}
	for _, r := range st.Delivery.Requirements {
		links := []map[string]any{}
		complete := true
		for _, id := range r.TaskIDs {
			tracked[id] = true
			task, ok := byID[id]
			unchanged := ok && session.TaskFingerprint(task) == st.Delivery.TaskFingerprints[id]
			complete = complete && unchanged && task.Status == session.TodoStatusCompleted
			run := st.RoleExecutions[id]
			currentHandoff := ok && run.TaskFingerprint == session.TaskFingerprint(task)
			var changedFiles []string
			if run.Handoff != nil {
				changedFiles = run.Handoff.ChangedFiles
			}
			links = append(links, map[string]any{"task_id": id, "present": ok, "requirements_unchanged": unchanged, "status": task.Status, "criteria": task.AcceptanceCriteria, "reported_evidence": task.Evidence, "changed_files": changedFiles, "handoff": run.Handoff, "handoff_matches_task": currentHandoff, "machine_checks": run.MachineChecks, "independent_review_passed": run.Passed && currentHandoff})
		}
		rows = append(rows, map[string]any{"requirement": r, "tasks_complete": complete, "links": links})
	}
	var untracked []session.Todo
	for _, task := range todos {
		if !tracked[task.ID] {
			untracked = append(untracked, task)
		}
	}
	return map[string]any{"registered": true, "profile": st.Profile, "current_stage": st.Delivery.CurrentStage, "stages": st.Delivery.Stages, "requirements": rows, "untracked_tasks": untracked, "all_stages_passed": st.Delivery.CurrentStage == len(st.Delivery.Stages), "design": st.Delivery.Design, "evidence_basis": "task evidence is reported; machine checks are observed journal records"}
}
