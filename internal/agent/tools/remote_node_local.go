package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/taskstate"
)

type localStepRecord struct {
	RequestHash string           `json:"request_hash"`
	Result      nodes.StepResult `json:"result"`
}

// runLocalOnce journals local effects under a cross-process lock. A retry
// waits for an active owner, replays a terminal result, or reports an unknown
// outcome after a crash. It never blindly repeats an interrupted command.
func (t *remoteNodeTool) runLocalOnce(ctx context.Context, session string, step nodes.PlanStep, res nodes.StepResult) nodes.StepResult {
	if err := ctx.Err(); err != nil {
		res = failStep(res, err)
		res.State = taskstate.Canceled
		return res
	}
	if t.runsDir == "" {
		return failStep(res, errors.New("local operation journal unavailable"))
	}
	dir := filepath.Join(t.runsDir, "local-operations")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return failStep(res, err)
	}
	path := filepath.Join(dir, nodes.RequestHash(res.OperationID)+".json")
	release, err := lock.File(ctx, path+".lock")
	if err != nil {
		res = failStep(res, err)
		if ctx.Err() != nil {
			res.State = taskstate.Canceled
		}
		return res
	}
	defer release()
	hash := nodes.RequestHash(struct {
		Root    string
		Session string
		Step    nodes.PlanStep
	}{filepath.Clean(t.workingDir), session, step})
	data, err := os.ReadFile(path)
	if err == nil {
		var prior localStepRecord
		if err := json.Unmarshal(data, &prior); err != nil {
			return failStep(res, fmt.Errorf("read local operation journal: %w", err))
		}
		if prior.RequestHash != hash || prior.Result.OperationID != res.OperationID {
			return failStep(res, deviceErr("operation_conflict", "operation_id", nodes.ErrOperationConflict.Error(), "Use a new plan id for different work."))
		}
		prior.Result.Replayed = true
		if !prior.Result.State.Terminal() {
			prior.Result.State, prior.Result.Effect = taskstate.Failed, "unknown"
			prior.Result.ErrorCode = "interrupted_operation"
			prior.Result.Error = "previous local operation was interrupted; outcome unknown, not re-run"
			prior.Result.EndedAt = time.Now().UnixMilli()
			if err := taskstate.WriteJSONAtomic(path, prior); err != nil {
				return failStep(res, fmt.Errorf("persist interrupted local operation: %w", err))
			}
		}
		return prior.Result
	}
	if !errors.Is(err, os.ErrNotExist) {
		return failStep(res, err)
	}
	record := localStepRecord{RequestHash: hash, Result: res}
	record.Result.State, record.Result.Effect = taskstate.Running, "unknown"
	if err := taskstate.WriteJSONAtomic(path, record); err != nil {
		return failStep(res, fmt.Errorf("journal local operation before executing: %w", err))
	}
	res = t.runLocal(ctx, step, res)
	record.Result = res
	if err := taskstate.WriteJSONAtomic(path, record); err != nil {
		res.State, res.Effect, res.ErrorCode = taskstate.Failed, "unknown", "persist_failed"
		res.Error = "local operation finished but its result could not be persisted: " + err.Error()
	}
	return res
}
