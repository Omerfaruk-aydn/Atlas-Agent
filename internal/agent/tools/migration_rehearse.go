package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/permission"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/rehearsal"
)

//go:embed migration_rehearse.md
var migrationRehearseDescription string

type MigrationRehearseParams struct {
	SeedPath      string                `json:"seed_path"`
	MigrationPath string                `json:"migration_path"`
	RollbackPath  string                `json:"rollback_path,omitempty"`
	Before        []rehearsal.Assertion `json:"before,omitempty"`
	After         []rehearsal.Assertion `json:"after"`
}

func NewMigrationRehearseTool(root string, perms permission.Service) fantasy.AgentTool {
	return fantasy.NewAgentTool("migration_rehearse", migrationRehearseDescription, func(ctx context.Context, p MigrationRehearseParams, call fantasy.ToolCall) (fantasy.ToolResponse, error) {
		seed, err := engineering.ReadProjectEvidence(ctx, root, p.SeedPath)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		migration, err := engineering.ReadProjectEvidence(ctx, root, p.MigrationPath)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		var rollback []byte
		if p.RollbackPath != "" {
			rollback, err = engineering.ReadProjectEvidence(ctx, root, p.RollbackPath)
			if err != nil {
				return fantasy.NewTextErrorResponse(err.Error()), nil
			}
		}
		if perms == nil || GetSessionFromContext(ctx) == "" {
			return fantasy.NewTextErrorResponse("migration rehearsal requires permissions and a session"), nil
		}
		approved, err := perms.Request(ctx, permission.CreatePermissionRequest{SessionID: GetSessionFromContext(ctx), Path: root, ToolCallID: call.ID, ToolName: "migration_rehearse", Action: "execute", Description: "Rehearse SQLite migration in disposable memory", Params: p})
		if err != nil {
			return fantasy.ToolResponse{}, err
		}
		if !approved {
			return NewPermissionDeniedResponse(perms), nil
		}
		runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		result, err := rehearsal.SQLite(runCtx, string(seed), string(migration), string(rollback), p.Before, p.After)
		if err != nil {
			return fantasy.NewTextErrorResponse(err.Error()), nil
		}
		data, err := json.Marshal(map[string]any{"result": result, "seed_hash": engineering.Hash(string(seed)), "migration_hash": engineering.Hash(string(migration)), "rollback_hash": engineering.Hash(string(rollback))})
		return fantasy.WithResponseMetadata(fantasy.NewTextResponse(string(data)), MeasuredCheckMetadata{Observed: true, Passed: result.Passed}), err
	})
}
