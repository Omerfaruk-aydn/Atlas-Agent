package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	sessionexport "github.com/Omerfaruk-aydn/Atlas-Agent/internal/session/export"
	"github.com/spf13/cobra"
)

type remoteHandoffBundle struct {
	Version   int    `json:"version"`
	SessionID string `json:"session_id"`
	Session   string `json:"session"`
	Diff      string `json:"diff"`
	CreatedAt int64  `json:"created_at"`
}

func createRemoteHandoffBundle(cmd *cobra.Command, sessionID string) (string, error) {
	ctx, svc, cleanup, err := sessionSetup(cmd)
	if err != nil {
		return "", err
	}
	defer cleanup()
	sess, err := resolveSessionID(ctx, svc.sessions, sessionID)
	if err != nil {
		return "", err
	}
	messages, err := svc.messages.List(ctx, sess.ID)
	if err != nil {
		return "", err
	}
	rawExport, err := sessionexport.JSON(sess, messages)
	if err != nil {
		return "", err
	}
	var exportDocument any
	if err := json.Unmarshal(rawExport, &exportDocument); err != nil {
		return "", err
	}
	redactedExport, err := json.Marshal(redactSecrets(exportDocument))
	if err != nil {
		return "", err
	}
	exported := string(redactedExport)
	root, err := workflowRoot(cmd)
	if err != nil {
		return "", err
	}
	diff, err := exec.CommandContext(ctx, "git", "-C", root, "diff", "--binary", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("capture handoff diff: %w", err)
	}
	bundle := remoteHandoffBundle{Version: 1, SessionID: sess.ID, Session: exported, Diff: string(diff), CreatedAt: time.Now().UnixMilli()}
	data, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, ".atlas", "handoff", sess.ID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func newNodeHandoffCommand() *cobra.Command {
	command := &cobra.Command{Use: "handoff <node> <session-id>", Short: "Continue a session on a paired TLS node and return a branch; never merges locally", Args: cobra.ExactArgs(2), RunE: func(cmd *cobra.Command, args []string) error {
		path, err := createRemoteHandoffBundle(cmd, args[1])
		if err != nil {
			return err
		}
		root, err := workflowRoot(cmd)
		if err != nil {
			return err
		}
		local, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(local, "..") {
			return fmt.Errorf("handoff bundle must remain inside the workspace")
		}
		remote := filepath.ToSlash(local)
		prompt := "Continue the Atlas session from this handoff file. Apply its included diff only if it applies cleanly. Preserve all requirements, run the verification contract, and create a new branch containing the result. Do not push or merge without explicit permission. Print the branch name and verification evidence. Handoff: " + remote
		return runNodeSteps(cmd, "handoff-"+args[1], []nodes.PlanStep{
			{ID: "bundle", Node: args[0], Action: "push", LocalPath: local, RemotePath: remote, Overwrite: false},
			{ID: "continue", Node: args[0], Action: "exec", Argv: []string{"atlas", "run", "--output-format", "stream-json", "--", prompt}, DependsOn: []string{"bundle"}},
		})
	}}
	return command
}
