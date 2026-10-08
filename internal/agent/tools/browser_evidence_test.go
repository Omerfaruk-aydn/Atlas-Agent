package tools

import (
	"errors"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/stretchr/testify/require"
)

func TestBrowserFieldEvidenceParsesErrorWithBraces(t *testing.T) {
	t.Parallel()
	field := browser.FieldResult{Name: "Width", DocumentID: "doc", Context: "Design", Dispatch: "unknown", Status: browser.FieldUnknown}
	resp := browserJSONError(field, errors.New(`outcome_unknown: last seen "{not JSON}"`))
	targets, changed, uncertain := fieldOperationEvidence("set_field", BrowserParams{}, resp)
	require.False(t, uncertain)
	require.Equal(t, []string{fieldEvidenceKey(field)}, targets)
	require.Equal(t, targets, changed, "uncertain dispatch still expires old evidence")
}

func TestBrowserPartialWriteEvidenceSurvivesFinalTabChange(t *testing.T) {
	t.Parallel()
	key := fieldEvidenceKey(browser.FieldResult{Name: "Width", Context: "Design", DocumentID: "doc-original"})
	earlier := BrowserOp{ID: "before", Seq: 1, Tab: "original", Action: "read_field", TargetKeys: []string{key}}
	later := BrowserOp{ID: "partial", Seq: 2, Tab: "other", Action: "fill_form", ChangedKeys: []string{key}, Epoch: browserProcessEpoch}
	ledger := &browserLedger{ops: []BrowserOp{earlier, later}}
	id, changed := ledger.changedAfter(earlier)
	require.True(t, changed)
	require.Equal(t, later.ID, id)

	root := t.TempDir()
	file := &browserTaskFile{Version: browserTaskVersion, Active: &browserTask{ID: "task", State: taskActive, Created: time.Now(), Requirements: []browserRequirement{{
		ID: "width", Status: ReqVerified, Evidence: []browserEvidence{{Epoch: browserProcessEpoch, Seq: 1, Tab: "original", TargetKeys: []string{key}}},
	}}}}
	require.NoError(t, saveBrowserTasks(root, "cross-tab", file))
	recordBrowserStep(root, "cross-tab", later)
	updated, err := loadBrowserTasks(root, "cross-tab")
	require.NoError(t, err)
	status, _ := updated.Active.effective(updated.Active.Requirements[0])
	require.Equal(t, reqReverify, status)
	require.False(t, resolvedEvidenceChanged([]string{legacyEvidenceKey("Width")}, []string{legacyEvidenceKey("Width")}), "a label alone cannot match another tab")
}

func TestBrowserUnstructuredUncertainWriteExpiresEvidence(t *testing.T) {
	t.Parallel()
	resp := browserJSONError(map[string]any{}, errors.New("outcome_unknown: input was sent"))
	// A lost structured response cannot justify retaining old page evidence.
	resp.Content = "outcome_unknown: input was sent"
	_, _, uncertain := fieldOperationEvidence("fill_form", BrowserParams{}, resp)
	require.True(t, uncertain)
	require.True(t, browserOpInvalidates(BrowserOp{ChangeUncertain: uncertain}, "read_field", "Width", nil))
}
