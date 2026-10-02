package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/agent/tools"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/config"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/subagents"
	"github.com/stretchr/testify/require"
)

func TestRepairCancellationDeniedBudgetAndObservedResolution(t *testing.T) {
	for _, mode := range []string{"passed", "cancelled", "denied", "budget", "unavailable", "diagnostic-change", "forged", "source-change", "repeat", "bounded"} {
		t.Run(mode, func(t *testing.T) {
			env := testEnv(t)
			root := t.TempDir()
			_, err := engineering.Git(t.Context(), root, "init")
			require.NoError(t, err)
			source := filepath.Join(root, "source.go")
			require.NoError(t, os.WriteFile(source, []byte("broken"), 0o644))
			sess, err := env.sessions.Create(t.Context(), "repair")
			require.NoError(t, err)
			task := session.Todo{ID: "task", Content: "Fix the observed failure", Status: session.TodoStatusInProgress, Agent: "debug", OwnedPaths: []string{"."}, AcceptanceCriteria: []string{"Observed check passes"}}
			sess.Todos = []session.Todo{task}
			sess, err = env.sessions.Save(t.Context(), sess)
			require.NoError(t, err)
			store := engineering.NewStore(t.TempDir())
			require.NoError(t, store.Update(t.Context(), sess.ID, func(st *engineering.State) error {
				st.RoleExecutions[task.ID] = engineering.RoleExecution{TaskID: task.ID, TaskFingerprint: session.TaskFingerprint(task)}
				return nil
			}))
			ctx := context.WithValue(t.Context(), tools.SessionIDContextKey, sess.ID)
			cancelCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			failure := fantasy.WithResponseMetadata(fantasy.NewTextResponse("observed failure"), tools.BashResponseMetadata{ExitCode: new(1), Output: "observed failure"})
			op, err := store.Begin(ctx, sess.ID, "original", "bash", `{"command":"check"}`, task.ID)
			require.NoError(t, err)
			require.NoError(t, store.FinishObserved(ctx, sess.ID, op, false, false, tools.ToolEvidenceHash("bash", failure, nil), true))
			env.permissions.SetMode(permission.ModeBypass)
			if mode == "denied" {
				env.permissions.SetMode(permission.ModePlan)
			}
			if mode == "budget" {
				require.NoError(t, store.SetBudget(ctx, sess.ID, task.ID, engineering.Limits{MaxToolCalls: 1}))
			}
			c := &coordinator{cfg: config.NewTestStore(&config.Config{}).Scoped(root), sessions: env.sessions, permissions: env.permissions, engineering: store}
			counts := map[string]int{}
			bash := fantasy.NewAgentTool("bash", "fixture", func(ctx context.Context, p tools.BashParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				counts["bash"]++
				if mode == "cancelled" {
					cancel()
					return fantasy.ToolResponse{}, context.Canceled
				}
				if mode == "unavailable" {
					return fantasy.NewTextErrorResponse("runtime unavailable"), nil
				}
				if mode == "diagnostic-change" {
					require.NoError(t, os.WriteFile(source, []byte("diagnostic wrote source"), 0o644))
					return failure, nil
				}
				data, err := os.ReadFile(source)
				require.NoError(t, err)
				if string(data) == "fixed" {
					return fantasy.WithResponseMetadata(fantasy.NewTextResponse("passed"), tools.BashResponseMetadata{ExitCode: new(0), Output: "passed"}), nil
				}
				return failure, nil
			})
			specialist := fantasy.NewAgentTool("agent", "fixture", func(ctx context.Context, p AgentParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				counts[p.AgentName]++
				handoff := subagents.Handoff{TaskID: task.ID, Summary: "Observed fixture", ChangedFiles: []string{}, Checks: []subagents.HandoffCheck{}, Risks: []string{}, Dependencies: []string{}, Decision: "passed"}
				if p.AgentName == "debug" {
					handoff.Decision = "ready"
					switch mode {
					case "repeat":
					case "bounded":
						require.NoError(t, os.WriteFile(source, []byte(fmt.Sprintf("still broken %d", counts["debug"])), 0o644))
					default:
						require.NoError(t, os.WriteFile(source, []byte("fixed"), 0o644))
					}
					if mode != "repeat" {
						require.NoError(t, store.Update(ctx, sess.ID, func(st *engineering.State) error { st.Generation++; return nil }))
					}
				} else if mode == "source-change" {
					require.NoError(t, os.WriteFile(source, []byte("changed during review"), 0o644))
				}
				return fantasy.NewTextResponse(workflowJSON(handoff)), nil
			})
			var invoke tools.ToolInvoker
			verify := tools.NewVerifyTool(root, store, func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				return invoke(ctx, call)
			})
			invoke = func(ctx context.Context, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
				if mode == "forged" {
					return fantasy.NewTextResponse(`{"passed":true}`), nil
				}
				var target fantasy.AgentTool
				switch call.Name {
				case "verify":
					counts["verify"]++
					target = verify
				case "bash":
					target = bash
				case "agent":
					target = specialist
				default:
					t.Fatalf("Unexpected repair tool %s", call.Name)
				}
				return (&guardedTool{AgentTool: target, store: store}).Run(ctx, call)
			}
			p := WorkflowParams{Action: "repair", TaskID: task.ID, OperationID: op, RepairHypothesis: "Inspect the failing check", Checks: []tools.VerificationStep{{Name: "check", Tool: "bash", Input: json.RawMessage(`{"command":"check"}`)}}}
			response, err := c.repairWorkflow(cancelCtx, p, fantasy.ToolCall{ID: "repair"}, invoke)
			if mode == "cancelled" {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, err)
			}
			if mode == "denied" || mode == "budget" {
				require.Empty(t, counts)
			}
			if mode == "repeat" || mode == "bounded" {
				before := counts["debug"]
				if mode == "bounded" {
					for i := range 2 {
						response, err = c.repairWorkflow(ctx, p, fantasy.ToolCall{ID: fmt.Sprintf("again-%d", i)}, invoke)
						require.NoError(t, err)
						require.True(t, response.IsError)
					}
					before = counts["debug"]
					require.Equal(t, 3, before)
				}
				response, err = c.repairWorkflow(ctx, p, fantasy.ToolCall{ID: "new-call-id"}, invoke)
				require.NoError(t, err)
				require.True(t, response.IsError)
				require.Equal(t, before, counts["debug"])
			}
			status, err := c.repairWorkflow(ctx, WorkflowParams{TaskID: task.ID, RepairAction: "status"}, fantasy.ToolCall{ID: "status"}, nil)
			require.NoError(t, err)
			var repair engineering.RepairCase
			require.NoError(t, json.Unmarshal([]byte(status.Content), &repair))
			if mode == "passed" {
				require.False(t, response.IsError)
				require.Equal(t, "resolved", repair.Status)
				state, err := store.Read(ctx, sess.ID)
				require.NoError(t, err)
				require.NoError(t, engineering.ValidateRepair(ctx, state, repair))
				require.Equal(t, 1, counts["debug"])
				require.Equal(t, 1, counts["review"])
			} else {
				require.NotEqual(t, "resolved", repair.Status)
			}
			if mode == "cancelled" {
				require.Len(t, repair.Attempts, 1)
				require.NotEmpty(t, repair.Attempts[0].PendingCallID)
			}
			if mode == "unavailable" {
				require.Zero(t, counts["debug"])
			}
			if mode == "denied" || mode == "budget" {
				require.Empty(t, repair.Attempts)
				require.Equal(t, "blocked", repair.Status)
			}
			if mode == "diagnostic-change" || mode == "forged" {
				require.Zero(t, counts["debug"])
				require.Equal(t, "blocked", repair.Status)
			}
			for _, attempt := range repair.Attempts {
				require.LessOrEqual(t, len(attempt.DiagnosticRunIDs), 2)
			}
		})
	}
}
