package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/maintenance"
	"github.com/spf13/cobra"
)

func newWatchCommentsCommand() *cobra.Command {
	command := &cobra.Command{Use: "watch-comments", Short: "Watch explicitly selected files for ATLAS comment tasks", RunE: func(cmd *cobra.Command, args []string) error {
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		store, err := workflowStore(cmd)
		if err != nil {
			return err
		}
		paths, _ := cmd.Flags().GetStringArray("file")
		once, _ := cmd.Flags().GetBool("once")
		dispatch, _ := cmd.Flags().GetBool("dispatch")
		if len(paths) == 0 || len(paths) > 16 {
			return fmt.Errorf("select 1-16 literal source files with --file")
		}
		namespace := "editor-tasks-" + engineering.Hash(root)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			for _, path := range paths {
				data, err := engineering.ReadProjectEvidence(cmd.Context(), root, path)
				if err != nil {
					return err
				}
				tasks, err := maintenance.CommentTasks(cmd.Context(), filepath.ToSlash(filepath.Clean(path)), data)
				if err != nil {
					return err
				}
				for _, task := range tasks {
					existing, previous, readErr := store.ReadRecord(cmd.Context(), namespace, task.ID)
					if readErr == nil {
						var saved struct {
							Status string `json:"status"`
						}
						if err := json.Unmarshal(previous, &saved); err != nil {
							return err
						}
						if !dispatch || saved.Status != "queued" {
							continue
						}
					} else if !errors.Is(readErr, os.ErrNotExist) {
						return readErr
					}
					payload, err := json.Marshal(map[string]any{"task": task, "status": "queued", "source_hash": engineering.Hash(string(data))})
					if err != nil {
						return err
					}
					record := existing
					if readErr != nil {
						record, err = store.PutRecordStrict(cmd.Context(), namespace, task.ID, 0, payload)
						if err != nil {
							return err
						}
					}
					if err := json.NewEncoder(cmd.OutOrStdout()).Encode(task); err != nil {
						return err
					}
					if !dispatch {
						continue
					}
					payload, err = json.Marshal(map[string]any{"task": task, "status": "running", "source_hash": engineering.Hash(string(data))})
					if err != nil {
						return err
					}
					record, err = store.PutRecordStrict(cmd.Context(), namespace, task.ID, record.Revision, payload)
					if err != nil {
						return err
					}
					executable, err := os.Executable()
					if err != nil {
						return err
					}
					prompt := fmt.Sprintf("User enabled editor comment dispatch for %s:%d. Task: %s. Inspect project instructions and current source before editing. Follow ordinary permissions and verify the outcome.", task.Path, task.Line, task.Prompt)
					child := exec.CommandContext(cmd.Context(), executable, "run", "--cwd", root, "--data-dir", filepath.Dir(store.Dir()), "--", prompt)
					child.Stdout = cmd.OutOrStdout()
					child.Stderr = cmd.ErrOrStderr()
					runErr := child.Run()
					status := "delivered"
					if runErr != nil {
						status = "failed"
					}
					payload, err = json.Marshal(map[string]any{"task": task, "status": status, "source_hash": engineering.Hash(string(data))})
					if err != nil {
						return err
					}
					if _, err := store.PutRecordStrict(cmd.Context(), namespace, task.ID, record.Revision, payload); err != nil {
						return err
					}
					if runErr != nil {
						return runErr
					}
				}
			}
			if once {
				return nil
			}
			select {
			case <-cmd.Context().Done():
				return cmd.Context().Err()
			case <-ticker.C:
			}
		}
	}}
	command.Flags().StringArray("file", nil, "Explicit workspace-relative source file to watch; repeatable")
	command.Flags().Bool("once", false, "Scan once and exit")
	command.Flags().Bool("dispatch", false, "Explicitly authorize sending newly discovered comments to atlas run; does not bypass tool permissions")
	command.AddCommand(newEditorQueueCommands()...)
	return command
}

func newEditorQueueCommands() []*cobra.Command {
	var commands []*cobra.Command
	for _, action := range []string{"list", "cancel", "retry"} {
		use, args := "list", cobra.NoArgs
		if action != "list" {
			use, args = action+" <id>", cobra.ExactArgs(1)
		}
		command := &cobra.Command{Use: use, Args: args, Short: "Inspect or update persisted editor tasks without running them", RunE: func(cmd *cobra.Command, args []string) error {
			root, err := workflowRoot(cmd)
			if err != nil {
				return err
			}
			store, err := workflowStore(cmd)
			if err != nil {
				return err
			}
			namespace := "editor-tasks-" + engineering.Hash(root)
			if action == "list" {
				records, err := store.ListRecords(cmd.Context(), namespace)
				if err != nil {
					return err
				}
				if len(records) > 1024 {
					return fmt.Errorf("editor queue exceeds 1024 records")
				}
				items := make(map[string]json.RawMessage, len(records))
				for id := range records {
					_, data, err := store.ReadRecord(cmd.Context(), namespace, id)
					if err != nil {
						return err
					}
					items[id] = data
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(items)
			}
			record, data, err := store.ReadRecord(cmd.Context(), namespace, args[0])
			if err != nil {
				return err
			}
			var item map[string]json.RawMessage
			if err := json.Unmarshal(data, &item); err != nil {
				return err
			}
			var status string
			if err := json.Unmarshal(item["status"], &status); err != nil {
				return err
			}
			acknowledged, _ := cmd.Flags().GetBool("acknowledge-previous-run")
			if status == "running" && !acknowledged {
				return fmt.Errorf("stop the original watcher and inspect the previous run before using --acknowledge-previous-run")
			}
			next := "cancelled"
			if action == "retry" {
				next = "queued"
			}
			item["status"], _ = json.Marshal(next)
			data, err = json.Marshal(item)
			if err != nil {
				return err
			}
			_, err = store.PutRecordStrict(cmd.Context(), namespace, args[0], record.Revision, data)
			return err
		}}
		command.Flags().Bool("acknowledge-previous-run", false, "Acknowledge that the original watcher stopped and its effects were inspected")
		commands = append(commands, command)
	}
	return commands
}

func init() { rootCmd.AddCommand(newWatchCommentsCommand()) }
