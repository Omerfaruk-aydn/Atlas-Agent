package agent

import (
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/csync"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/outcome"
	"github.com/stretchr/testify/require"
)

func newRootAgent(workingDir string) *sessionAgent {
	a := newLedgerAgent()
	a.workingDir = workingDir
	a.projectRoots = csync.NewMap[string, string]()
	return a
}

func TestMessageDuringWorkExtendsTheRequest(t *testing.T) {
	t.Parallel()
	a := newRootAgent(t.TempDir())
	a.beginEvaluatorRequest(SessionAgentCall{SessionID: "s", Prompt: "Build it:\n1. a scene\n2. unit tests"})
	ledger, _ := a.evaluatorLedgers.Get("s")

	// The user's next message was already waiting when the turn ended.
	a.markRequestOpen("s", true)
	a.beginEvaluatorRequest(SessionAgentCall{SessionID: "s", Prompt: "put the project on D:"})
	same, _ := a.evaluatorLedgers.Get("s")
	require.Same(t, ledger, same, "the request keeps its ledger")
	require.Equal(t, []string{"a scene", "unit tests", "put the project on D:"}, requestItems(same.request))

	// Once the request ended on its own, the next message is a new one.
	a.markRequestOpen("s", false)
	a.beginEvaluatorRequest(SessionAgentCall{SessionID: "s", Prompt: "something else"})
	fresh, _ := a.evaluatorLedgers.Get("s")
	require.NotSame(t, ledger, fresh)
	require.Equal(t, "something else", fresh.request)
}

func TestRequestAdditionsNeedAListedRequest(t *testing.T) {
	t.Parallel()
	require.Nil(t, requestItems(addToRequest("fix the bug", "and the other one")),
		"prose with an addition still has no checklist")
	require.Equal(t, "x", addToRequest("x", "  "))
}

func TestWorkPlanElsewhereMovesTheProjectRoot(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "Orbitarium")
	a := newRootAgent(home)
	a.beginEvaluatorRequest(SessionAgentCall{SessionID: "s", Prompt: "go"})
	require.Equal(t, home, a.projectRoot("s"))

	a.notePlanWrite("s", filepath.Join(project, "src", "main.ts"))
	require.Equal(t, home, a.projectRoot("s"), "only a work plan moves the root")

	a.notePlanWrite("s", filepath.Join(project, ".atlas", "work.json"))
	require.Equal(t, project, a.projectRoot("s"))
	ledger, _ := a.evaluatorLedgers.Get("s")
	require.Equal(t, project, a.ledgerRoot(ledger))

	a.beginEvaluatorRequest(SessionAgentCall{SessionID: "s", Prompt: "next request"})
	ledger, _ = a.evaluatorLedgers.Get("s")
	require.Equal(t, project, ledger.root, "later requests stay in the project")

	a.notePlanWrite("s", filepath.Join(project, ".atlas", "probes", "F1", ".atlas", "work.json"))
	require.Equal(t, project, a.projectRoot("s"), "scratch worktrees are not a new project")

	a.notePlanWrite("s", filepath.Join(home, ".atlas", "work.json"))
	require.Equal(t, home, a.projectRoot("s"))
}

func TestWorkInputAcceptsTopLevelFields(t *testing.T) {
	t.Parallel()
	raw := `{"action":"work","work_action":"contract_add","kind":"deliverable","origin":"user","source_turns":[2,3],"text":"ship it"}`
	in := workInput(WorkflowParams{WorkAction: "contract_add"}, raw)
	require.Equal(t, "deliverable", in.Kind)
	require.Equal(t, "user", in.Origin)
	require.Equal(t, []int{2, 3}, in.SourceTurns)

	nested := WorkflowParams{WorkAction: "contract_advance", Work: &WorkInput{ItemID: "W1", Status: "verified"}}
	in = workInput(nested, `{"work_action":"contract_advance","item_id":"top","work":{"item_id":"W1","status":"verified"}}`)
	require.Equal(t, "W1", in.ItemID, "the nested field wins")
	require.Equal(t, "verified", in.Status)
}

func TestGreenContractVerifiesAppliedWork(t *testing.T) {
	t.Parallel()
	a := newRootAgent(t.TempDir())
	ledger := a.evaluatorLedgerFor("s")
	reqs := func() []outcome.Requirement {
		return []outcome.Requirement{{ID: "solar-system", Phase: outcome.PhaseApplied}, {ID: "next", Phase: outcome.PhasePlanned}}
	}
	require.Equal(t, outcome.PhaseApplied, a.verifiedByContract("s", reqs())[0].Phase, "no contract run yet")

	ledger.lastEdit = 4
	ledger.greenEdit = ledger.lastEdit + 1
	out := a.verifiedByContract("s", reqs())
	require.Equal(t, outcome.PhaseVerified, out[0].Phase)
	require.Equal(t, outcome.PhasePlanned, out[1].Phase, "planned work is not done by a green contract")

	ledger.lastEdit = 7 // Code changed after the contract passed.
	require.Equal(t, outcome.PhaseApplied, a.verifiedByContract("s", reqs())[0].Phase)
}
