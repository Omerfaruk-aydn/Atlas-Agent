package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/db"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/projects"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/server"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/session"
	"github.com/spf13/cobra"
)

type sessionOwner struct{ Root, DataDir, SessionID string }

// FindSessionOwner reads existing databases without initializing a workspace.
// Explicit workspace/data flags keep resolution scoped to their database.
func findSessionOwner(ctx context.Context, id, root, dataDir string, registered []projects.Project, pinned bool) (sessionOwner, error) {
	if id == "" {
		return sessionOwner{}, fmt.Errorf("session ID is empty")
	}
	if dataDir == "" {
		dataDir = filepath.Join(root, ".atlas")
	} else if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(root, dataDir)
	}
	candidates := []projects.Project{{Path: root, DataDir: dataDir}}
	if !pinned {
		candidates = append(candidates, registered...)
	}
	seen := map[string]bool{}
	matches := []sessionOwner{}
	failures := []string{}
	for _, project := range candidates {
		if err := ctx.Err(); err != nil {
			return sessionOwner{}, err
		}
		if !filepath.IsAbs(project.Path) || !filepath.IsAbs(project.DataDir) {
			continue
		}
		path := filepath.Join(project.DataDir, "atlas.db")
		canonical, err := filepath.EvalSymlinks(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			failures = append(failures, project.Path)
			continue
		}
		canonical = filepath.Clean(canonical)
		if seen[canonical] {
			continue
		}
		seen[canonical] = true
		conn, err := db.ConnectReadOnly(ctx, canonical)
		if err != nil {
			failures = append(failures, project.Path)
			continue
		}
		sessions, err := session.NewService(db.New(conn), conn).List(ctx)
		_ = conn.Close()
		if err != nil {
			failures = append(failures, project.Path)
			continue
		}
		for _, sess := range sessions {
			if sess.ParentSessionID == "" && (sess.ID == id || strings.HasPrefix(session.HashID(sess.ID), id)) {
				matches = append(matches, sessionOwner{Root: project.Path, DataDir: project.DataDir, SessionID: sess.ID})
			}
		}
	}
	if len(failures) > 0 {
		return sessionOwner{}, fmt.Errorf("cannot safely resolve session: %d project databases could not be inspected (%s)", len(failures), strings.Join(failures, ", "))
	}
	switch len(matches) {
	case 0:
		return sessionOwner{}, fmt.Errorf("session not found: %s", id)
	case 1:
		return matches[0], nil
	default:
		return sessionOwner{}, fmt.Errorf("session ID %q is ambiguous across registered projects (%d matches); specify --cwd or --data-dir", id, len(matches))
	}
}

func selectSessionWorkspace(cmd *cobra.Command) error {
	if useClientServer() {
		host, err := server.ParseHostURL(clientHost)
		if err != nil {
			return err
		}
		name := host.Hostname()
		if host.Scheme == "tcp" && name != "localhost" && name != "127.0.0.1" && name != "::1" {
			return nil
		}
	}
	id, _ := cmd.Flags().GetString("session")
	if id == "" {
		return nil
	}
	root, err := workflowRoot(cmd)
	if err != nil {
		return err
	}
	dataDir, _ := cmd.Flags().GetString("data-dir")
	pinned := cmd.Flags().Changed("cwd") || cmd.Flags().Changed("data-dir")
	var registered []projects.Project
	if !pinned {
		registered, err = projects.List()
		if err != nil {
			return fmt.Errorf("read registered projects: %w", err)
		}
	}
	owner, err := findSessionOwner(cmd.Context(), id, root, dataDir, registered, pinned)
	if err != nil {
		return err
	}
	if err := cmd.Flags().Set("cwd", owner.Root); err != nil {
		return err
	}
	if err := cmd.Flags().Set("data-dir", owner.DataDir); err != nil {
		return err
	}
	return cmd.Flags().Set("session", owner.SessionID)
}
