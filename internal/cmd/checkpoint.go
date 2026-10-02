package cmd

import (
	"encoding/json"
	"path/filepath"
	"slices"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/workflows"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// Checkpoint commands never evaluate executable project configuration.
func checkpointStore(cmd *cobra.Command, root string) *engineering.Store {
	dir, _ := cmd.Flags().GetString("data-dir")
	if dir == "" {
		dir = ".atlas"
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return engineering.NewStore(dir)
}

func newCheckpointCommands() []*cobra.Command {
	commands := []*cobra.Command{}
	for _, action := range []string{"checkpoint", "resume-plan"} {
		args, use := cobra.ExactArgs(1), "checkpoint <session-id>"
		if action == "resume-plan" {
			args, use = cobra.ExactArgs(2), "resume-plan <session-id> <checkpoint-id>"
		}
		command := &cobra.Command{Use: use, Args: args, Short: "Inspect recovery state without an LLM, replay or file restore", RunE: func(cmd *cobra.Command, args []string) error {
			root, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			store := checkpointStore(cmd, root)
			conn, err := db.ConnectReadOnly(cmd.Context(), filepath.Join(filepath.Dir(store.Dir()), "atlas.db"))
			if err != nil {
				return err
			}
			defer conn.Close()
			sess, err := session.NewService(db.New(conn), conn).Get(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			state, err := store.Read(cmd.Context(), sess.ID)
			if err != nil {
				return err
			}
			paths, roles := recipeCLISources(cmd, root)
			if err := workflows.Current(cmd.Context(), store, sess.ID, paths, roles); err != nil {
				return err
			}
			source, err := engineering.SourceFingerprint(cmd.Context(), root, store.Dir())
			if err != nil {
				return err
			}
			specs, states := map[string]string{}, map[string]string{}
			for _, todo := range sess.Todos {
				specs[todo.ID], states[todo.ID] = session.TaskFingerprint(todo), string(todo.Status)
			}
			if action == "checkpoint" {
				data, err := json.Marshal(specs)
				if err != nil {
					return err
				}
				cp := engineering.Checkpoint{ID: uuid.NewString(), Root: root, SessionID: sess.ID, SourceFingerprint: source, PlanFingerprint: engineering.Hash(string(data)), TaskFingerprints: specs, Workspaces: []string{}, Operations: []string{}}
				if state.Recipe != nil {
					cp.RecipeHash, cp.ParametersHash = state.Recipe.RecipeHash, state.Recipe.ParametersHash
				}
				if state.Delivery != nil {
					cp.PlanFingerprint, cp.Stage = state.Delivery.Fingerprint(), state.Delivery.CurrentStage
				}
				for _, work := range state.Workspaces {
					cp.Workspaces = append(cp.Workspaces, work.ID)
				}
				for _, op := range state.Operations {
					cp.Operations = append(cp.Operations, op.ID)
				}
				if _, err := store.SaveCheckpoint(cmd.Context(), cp); err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(cp)
			}
			cp, err := store.ReadCheckpoint(cmd.Context(), root, sess.ID, args[1])
			if err != nil {
				return err
			}
			plan, err := engineering.PlanResume(cmd.Context(), engineering.ResumeInput{Checkpoint: cp, State: state, Source: source, TaskSpecs: specs, TaskStates: states})
			if err != nil {
				return err
			}
			store.ReconcileResumeEvidence(cmd.Context(), sess.ID, root, state, specs, states, &plan)
			if len(plan.AmbiguousOperations) == 0 && len(plan.ReverifyTasks) == 0 {
				ordered := slices.Clone(sess.Todos)
				if state.Delivery != nil {
					slices.SortStableFunc(ordered, func(a, b session.Todo) int {
						x, y := state.Delivery.AllowsTask(a.ID), state.Delivery.AllowsTask(b.ID)
						if x && !y {
							return -1
						}
						if !x && y {
							return 1
						}
						return 0
					})
				}
				wave, err := session.ReadyTaskWave(ordered, 16)
				if err != nil {
					return err
				}
				ready := []string{}
				for _, todo := range wave {
					if slices.Contains(plan.ReadyTasks, todo.ID) && (state.Delivery == nil || state.Delivery.AllowsTask(todo.ID)) {
						ready = append(ready, todo.ID)
					}
				}
				plan.ReadyTasks = ready
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(plan)
		}}
		command.Flags().StringArray("workflow-path", nil, "Additional recipe directory or JSON file")
		command.Flags().StringArray("role-path", nil, "Additional named role directory")
		commands = append(commands, command)
	}
	return commands
}
