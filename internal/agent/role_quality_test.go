package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

func TestIndependentQualityRequiresBothReviewersMachineChecksAndStableSources(t *testing.T) {
	for _, mode := range []string{"pass", "review-fails", "verify-fails", "source-mutates", "unobserved-pass"} {
		t.Run(mode, func(t *testing.T) {
			env := testEnv(t)
			root := t.TempDir()
			_, err := engineering.Git(t.Context(), root, "init")
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(root, "feature.go"), []byte("package feature\n"), 0o644))
			store := engineering.NewStore(t.TempDir())
			cfg, err := config.Init(root, t.TempDir(), false)
			require.NoError(t, err)
			c := &coordinator{cfg: cfg, sessions: env.sessions, engineering: store}
			sess, err := env.sessions.Create(t.Context(), "quality")
			require.NoError(t, err)
			task := session.Todo{ID: "api", Content: "Implement API", Agent: "backend", OwnedPaths: []string{"feature.go"}, Status: session.TodoStatusInProgress, AcceptanceCriteria: []string{"API tested"}}
			sess.Todos = []session.Todo{task}
			_, err = env.sessions.Save(t.Context(), sess)
			require.NoError(t, err)
			initial := subagents.Handoff{TaskID: "api", Summary: "Implemented API", ChangedFiles: []string{"feature.go"}, Checks: []subagents.HandoffCheck{}, Risks: []string{}, Dependencies: []string{}, Decision: "ready"}
			require.NoError(t, store.Update(t.Context(), sess.ID, func(st *engineering.State) error {
				st.RoleExecutions[task.ID] = engineering.RoleExecution{TaskID: task.ID, Agent: "backend", TaskFingerprint: session.TaskFingerprint(task), Root: root, Handoff: &initial, RequireReview: true}
				return nil
			}))
			var calls []string
			invoke := func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				if call.Name == "verify" {
					calls = append(calls, "verify")
					if mode == "verify-fails" {
						return fantasy.NewTextErrorResponse(`{"passed":false}`), nil
					}
					if mode == "source-mutates" {
						require.NoError(t, os.WriteFile(filepath.Join(root, "feature.go"), []byte("package changed\n"), 0o644))
					}
					if mode != "unobserved-pass" {
						require.NoError(t, store.Update(ctx, sess.ID, func(st *engineering.State) error {
							st.Checks = append(st.Checks, engineering.Check{TaskID: "api", RunID: call.ID, Passed: true, Evidence: "observed fixture exit 0"})
							return nil
						}))
					}
					return fantasy.NewTextResponse(`{"passed":true}`), nil
				}
				var p AgentParams
				require.NoError(t, json.Unmarshal([]byte(call.Input), &p))
				require.True(t, p.QualityOnly)
				require.Empty(t, p.WorkspaceID)
				require.Equal(t, "api", engineering.GetScope(ctx, "").TaskID)
				calls = append(calls, p.AgentName)
				h := initial
				h.ChangedFiles = []string{}
				h.Decision = "passed"
				zero := 0
				h.Checks = []subagents.HandoffCheck{{Command: "scoped test", ExitCode: &zero, Evidence: "exit 0"}}
				if mode == "review-fails" && p.AgentName == "review" {
					h.Decision = "changes_required"
					h.Risks = []string{"API regression"}
				}
				return fantasy.NewTextResponse(workflowJSON(h)), nil
			}
			ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
			resp, err := c.reviewTask(ctx, WorkflowParams{TaskID: "api"}, fantasy.ToolCall{ID: "quality"}, sess, invoke)
			require.NoError(t, err)
			st, err := store.Read(ctx, sess.ID)
			require.NoError(t, err)
			require.Equal(t, mode == "pass", st.RoleExecutions["api"].Passed)
			require.Equal(t, mode != "pass", resp.IsError)
			require.Equal(t, []string{"test", "review"}, calls[:2])
			if mode == "pass" {
				require.Equal(t, []string{"test", "review", "verify"}, calls)
				require.NotEmpty(t, st.RoleExecutions["api"].SourceFingerprint)
			}
		})
	}
}
