package tools

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/browser"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

func researchTestOp(t *testing.T, action, content string, document string) BrowserOp {
	t.Helper()
	_, op := recordBrowserOp(t.Name(), action, BrowserParams{}, fantasy.NewTextResponse(content), time.Now(), browser.ExecutionIdentity{Instance: "browser", TabID: "tab", Document: document})
	return op
}

func researchTestTask(t *testing.T, requirements ...BrowserRequirementInput) *browserTaskFile {
	t.Helper()
	file := &browserTaskFile{}
	require.NoError(t, planTask(file, BrowserTaskParams{TaskID: "research", Goal: "Research and deliver", Requirements: requirements}))
	return file
}

func TestBrowserResearchEvidenceSurvivesNavigationButLiveEvidenceExpires(t *testing.T) {
	t.Parallel()
	file := researchTestTask(t,
		BrowserRequirementInput{ID: "history", Text: "Atlas was released in 2024", Verify: "observation", Subject: "Atlas", Source: "https://example.test/atlas", Expect: "released in 2024"},
		BrowserRequirementInput{ID: "live", Text: "Atlas page is open", Verify: "structural"},
	)
	op := researchTestOp(t, "observe", "observation_id=o1 mode=full\nurl=https://example.test/atlas title=\"Atlas\"\n(page)\nAtlas released in 2024", "doc1")
	require.NoError(t, updateTask(file.Active, t.Name(), BrowserTaskParams{ID: "history", Status: ReqVerified, Evidence: []string{op.ID}}))
	require.NoError(t, updateTask(file.Active, t.Name(), BrowserTaskParams{ID: "live", Status: ReqVerified, Evidence: []string{op.ID}}))
	root := t.TempDir()
	require.NoError(t, saveBrowserTasks(root, t.Name(), file))
	later := researchTestOp(t, "observe", "observation_id=o2 mode=full\nurl=https://example.test/other title=\"Other\"\n(page)\nOther article", "doc2")
	recordBrowserStep(root, t.Name(), later)
	updated, err := loadBrowserTasks(root, t.Name())
	require.NoError(t, err)
	status, reason := updated.Active.effective(updated.Active.Requirements[0])
	require.Equal(t, ReqVerified, status)
	require.Empty(t, reason)
	status, reason = updated.Active.effective(updated.Active.Requirements[1])
	require.Equal(t, reqReverify, status)
	require.Equal(t, "document_changed", reason)
	// Historical evidence can also be attached after visiting another page.
	require.NoError(t, updateTask(updated.Active, t.Name(), BrowserTaskParams{ID: "history", Status: ReqVerified, Evidence: []string{op.ID}}))
	require.ErrorContains(t, updateTask(updated.Active, t.Name(), BrowserTaskParams{ID: "live", Status: ReqVerified, Evidence: []string{op.ID}}), "document_changed")
	// Restarting cannot turn historical quotes into live browser authority.
	updated.Active.Requirements[0].Evidence[0].Epoch = "earlier-process"
	status, reason = updated.Active.effective(updated.Active.Requirements[0])
	require.Equal(t, ReqVerified, status)
	require.Empty(t, reason)
	updated.Active.Requirements[0].Evidence[0].Kind = ""
	status, reason = updated.Active.effective(updated.Active.Requirements[0])
	require.Equal(t, reqReverify, status)
	require.Equal(t, "missing_provenance", reason)
}

func TestBrowserResearchEvidenceRejectsUnrelatedSubjectsAndSources(t *testing.T) {
	t.Parallel()
	file := researchTestTask(t, BrowserRequirementInput{ID: "claim", Text: "Atlas claim", Verify: "observation", Subject: "Atlas", Source: "https://example.test/atlas", Expect: "released in 2024"})
	for _, tc := range []struct {
		name, content string
	}{
		{"wrong subject", `{"sources":[{"final_url":"https://example.test/atlas","title":"Other","excerpt":"Other released in 2024"}]}`},
		{"claim on another source", `{"sources":[{"final_url":"https://example.test/atlas","title":"Atlas","excerpt":"Atlas released in 2023"},{"final_url":"https://example.test/other","title":"Other","excerpt":"released in 2024"}]}`},
		{"failed source", `{"sources":[{"final_url":"https://example.test/atlas","title":"Atlas","excerpt":"Atlas released in 2024","error":"read failed"}]}`},
		{"requested URL is not the final source", `{"sources":[{"requested_url":"https://example.test/atlas","final_url":"https://example.test/other","excerpt":"Atlas released in 2024"}]}`},
		{"source path case differs", `{"sources":[{"final_url":"https://example.test/Atlas","excerpt":"Atlas released in 2024"}]}`},
	} {
		op := researchTestOp(t, "research", tc.content, "controlled-doc")
		err := updateTask(file.Active, t.Name(), BrowserTaskParams{ID: "claim", Status: ReqVerified, Evidence: []string{op.ID}, Note: "Atlas released in 2024"})
		require.Error(t, err, tc.name)
	}
	op := researchTestOp(t, "research", `{"sources":[{"final_url":"https://example.test/atlas","title":"Atlas","excerpt":"Atlas released in 2024"}]}`, "controlled-doc")
	require.NoError(t, updateTask(file.Active, t.Name(), BrowserTaskParams{ID: "claim", Status: ReqVerified, Evidence: []string{op.ID}}))
	require.NoError(t, planTask(file, BrowserTaskParams{Mode: "extend", Requirements: []BrowserRequirementInput{{ID: "claim", Text: "Another entity", Verify: "observation", Subject: "Other", Source: "https://example.test/atlas", Expect: "released in 2024"}}}))
	require.Equal(t, ReqPending, file.Active.Requirements[0].Status)
	require.Empty(t, file.Active.Requirements[0].Evidence)
}

func TestBrowserDeliveryRequiresSavedFinalAndDoesNotLoop(t *testing.T) {
	t.Parallel()
	file := researchTestTask(t, BrowserRequirementInput{ID: "report", Text: "Final report delivered", Verify: "delivery", Expect: "Atlas research report"})
	op := researchTestOp(t, "observe", "observation_id=o1 mode=full\nurl=https://example.test/atlas\nAtlas research report", "doc")
	require.ErrorContains(t, updateTask(file.Active, t.Name(), BrowserTaskParams{ID: "report", Status: ReqVerified, Evidence: []string{op.ID}, Note: "Delivered"}), "saved final response")
	root := t.TempDir()
	require.NoError(t, saveBrowserTasks(root, t.Name(), file))
	d := BrowserTaskContinue(root, t.Name())
	require.False(t, d.Continue)
	require.Empty(t, d.StopReason)
	require.Equal(t, 1, d.Open)
	require.Equal(t, 1, d.DeliveryPending)
	var summary struct {
		Open            int `json:"open"`
		ActionableOpen  int `json:"actionable_open"`
		DeliveryPending int `json:"delivery_pending"`
	}
	require.NoError(t, json.Unmarshal([]byte(browserTaskSummary(file)), &summary))
	require.Equal(t, 1, summary.Open)
	require.Zero(t, summary.ActionableOpen)
	require.Equal(t, 1, summary.DeliveryPending)
	require.Error(t, RecordBrowserTaskDelivery(root, t.Name(), "", "Atlas research report"))
	require.NoError(t, RecordBrowserTaskDelivery(root, t.Name(), "saved-message", "An unrelated reply"))
	require.Equal(t, ReqPending, BrowserTaskRequirements(root, t.Name())[0].Status)
	require.NoError(t, RecordBrowserTaskDelivery(root, t.Name(), "saved-message", "Atlas research report: results and sources"))
	reqs := BrowserTaskRequirements(root, t.Name())
	require.Equal(t, ReqVerified, reqs[0].Status)
	require.Equal(t, []string{"saved-message"}, reqs[0].Evidence)
	require.Equal(t, "delivery", reqs[0].Provenance[0].Kind)
	require.NotEmpty(t, reqs[0].Provenance[0].ContentHash)
	require.Zero(t, BrowserTaskContinue(root, t.Name()).Open)
}

func TestBrowserObservationRequirementsRequireExplicitProvenance(t *testing.T) {
	t.Parallel()
	valid := BrowserRequirementInput{ID: "fact", Text: "Atlas fact", Verify: "observation", Subject: "Atlas", Source: "https://example.test/atlas", Expect: "released in 2024"}
	require.NoError(t, validRequirements([]BrowserRequirementInput{valid}))
	for _, field := range []string{"subject", "source", "expect"} {
		r := valid
		switch field {
		case "subject":
			r.Subject = ""
		case "source":
			r.Source = ""
		case "expect":
			r.Expect = ""
		}
		require.Error(t, validRequirements([]BrowserRequirementInput{r}), field)
	}
	require.Error(t, validRequirements([]BrowserRequirementInput{{ID: "delivery", Text: "Report", Verify: "delivery"}}))
}

func TestBrowserCollectedResearchUsesEachRowsReturnedSource(t *testing.T) {
	t.Parallel()
	file := researchTestTask(t, BrowserRequirementInput{ID: "claim", Text: "Atlas claim", Verify: "observation", Subject: "Atlas", Source: "https://example.test/atlas", Expect: "released in 2024"})
	op := researchTestOp(t, "collect", `{"source_url":"https://example.test/atlas","items":[{"source_url":"https://example.test/atlas","text":"Atlas released in 2023"},{"source_url":"https://example.test/other","text":"Other released in 2024"}]}`, "doc")
	require.Error(t, updateTask(file.Active, t.Name(), BrowserTaskParams{ID: "claim", Status: ReqVerified, Evidence: []string{op.ID}}))
	op = researchTestOp(t, "collect", `{"source_url":"https://example.test/atlas","items":[{"source_url":"https://example.test/atlas","text":"Atlas released in 2024"}]}`, "doc")
	require.NoError(t, updateTask(file.Active, t.Name(), BrowserTaskParams{ID: "claim", Status: ReqVerified, Evidence: []string{op.ID}}))
}
