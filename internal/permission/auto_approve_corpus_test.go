package permission

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

type approvalCorpus struct {
	Version int `json:"version"`
	Groups  []struct {
		ID       string   `json:"id"`
		Category string   `json:"category"`
		Expected string   `json:"expected"`
		Commands []string `json:"commands"`
	} `json:"groups"`
}

func loadApprovalCorpus(t *testing.T) approvalCorpus {
	t.Helper()
	data, err := os.ReadFile("testdata/auto_approve_corpus.json")
	require.NoError(t, err)
	var corpus approvalCorpus
	require.NoError(t, json.Unmarshal(data, &corpus))
	require.Equal(t, 1, corpus.Version)
	return corpus
}

func TestAutoApproveSecurityCorpus(t *testing.T) {
	t.Parallel()
	corpus := loadApprovalCorpus(t)
	seenGroups := map[string]bool{}
	seenCommands := map[string]bool{}
	counts := map[string]int{}
	for _, group := range corpus.Groups {
		require.NotEmpty(t, group.ID)
		require.NotEmpty(t, group.Category)
		require.NotEmpty(t, group.Commands)
		require.False(t, seenGroups[group.ID], "duplicate corpus group")
		seenGroups[group.ID] = true
		require.Contains(t, []string{"risky", "unknown"}, group.Expected)
		for _, command := range group.Commands {
			require.NotEmpty(t, command)
			require.False(t, seenCommands[command], "duplicate corpus command: %s", command)
			seenCommands[command] = true
			counts[group.Expected]++
			t.Run(group.ID+"/"+command, func(t *testing.T) {
				t.Parallel()
				req := CreatePermissionRequest{ToolName: "bash", Safe: group.Expected == string(ApprovalRisky), Params: map[string]string{"command": command}}
				got := ClassifyDeterministic(req, t.TempDir())
				require.Equal(t, ApprovalClass(group.Expected), got.Class, "command: %s", command)
				require.NotEqual(t, ApprovalSafe, got.Class, "false-safe command: %s", command)
			})
		}
	}
	require.GreaterOrEqual(t, len(seenCommands), 250)
	require.GreaterOrEqual(t, counts[string(ApprovalRisky)], 125)
	require.GreaterOrEqual(t, counts[string(ApprovalUnknown)], 100)
	t.Logf("corpus version=%d total=%d risky=%d benign_unknown=%d false_safe=0", corpus.Version, len(seenCommands), counts[string(ApprovalRisky)], counts[string(ApprovalUnknown)])
}

func TestAutoApproveRiskyCorpusNeverReachesModel(t *testing.T) {
	t.Parallel()
	corpus := loadApprovalCorpus(t)
	approver := &stubApprover{decision: ApprovalDecision{Class: ApprovalSafe}}
	for _, group := range corpus.Groups {
		if group.Expected != string(ApprovalRisky) {
			continue
		}
		for _, command := range group.Commands {
			req := CreatePermissionRequest{ToolName: "bash", Safe: true, Params: map[string]string{"command": command}}
			got := classifyAutoApproval(t.Context(), approver, req, t.TempDir())
			require.Equal(t, ApprovalRisky, got.Class, command)
		}
	}
	require.Zero(t, approver.calls, "a model must never override a deterministic risky decision")
}
