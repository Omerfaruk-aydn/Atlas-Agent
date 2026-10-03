// Package maintenance provides opt-in durable maintenance jobs.
package maintenance

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
)

const namespace = "maintenance-jobs"

type Job struct {
	ID             string   `json:"id"`
	Root           string   `json:"root"`
	Argv           []string `json:"argv"`
	PeriodMS       int64    `json:"period_ms"`
	TimeoutMS      int64    `json:"timeout_ms"`
	NextAt         int64    `json:"next_at"`
	LeaseUntil     int64    `json:"lease_until,omitempty"`
	Paused         bool     `json:"paused"`
	LastResultHash string   `json:"last_result_hash,omitempty"`
	LastExit       *int     `json:"last_exit,omitempty"`
	LastAt         int64    `json:"last_at,omitempty"`
}

type Runner func(context.Context, Job) (int, string, error)

type Event struct {
	JobID      string `json:"job_id"`
	ExitCode   int    `json:"exit_code"`
	Changed    bool   `json:"changed"`
	OutputHash string `json:"output_hash"`
}

func (j Job) Validate() error {
	if len(j.ID) < 1 || len(j.ID) > 128 || strings.ContainsAny(j.ID, "\x00\r\n") || !filepath.IsAbs(j.Root) || len(j.Argv) < 1 || len(j.Argv) > 64 || j.Argv[0] == "" || j.PeriodMS < 10000 || j.TimeoutMS < 1000 || j.TimeoutMS > 600000 {
		return fmt.Errorf("invalid job identity, command, period or timeout")
	}
	for _, arg := range j.Argv {
		if len(arg) > 32768 || strings.ContainsRune(arg, 0) {
			return fmt.Errorf("invalid job argument")
		}
	}
	return nil
}

func Save(ctx context.Context, store *engineering.Store, job Job, revision uint64) error {
	if err := job.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = store.PutRecordStrict(ctx, namespace, job.ID, revision, data)
	return err
}

func List(ctx context.Context, store *engineering.Store) ([]Job, error) {
	records, err := store.ListRecords(ctx, namespace)
	if err != nil {
		return nil, err
	}
	if len(records) > 64 {
		return nil, fmt.Errorf("maintenance job count exceeds 64")
	}
	jobs := make([]Job, 0, len(records))
	for id := range records {
		_, data, err := store.ReadRecord(ctx, namespace, id)
		if err != nil {
			return nil, err
		}
		var job Job
		if err := json.Unmarshal(data, &job); err != nil {
			return nil, err
		}
		if job.ID != id {
			return nil, fmt.Errorf("job identity mismatch")
		}
		if err := job.Validate(); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	slices.SortFunc(jobs, func(a, b Job) int { return strings.Compare(a.ID, b.ID) })
	return jobs, nil
}

// Tick claims each due job using CAS before invoking the authorized runner.
func Tick(ctx context.Context, store *engineering.Store, now time.Time, run Runner) ([]Event, error) {
	if run == nil {
		return nil, fmt.Errorf("maintenance runner unavailable")
	}
	jobs, err := List(ctx, store)
	if err != nil {
		return nil, err
	}
	events := []Event{}
	for _, job := range jobs {
		if err := ctx.Err(); err != nil {
			return events, err
		}
		if job.Paused || job.NextAt > now.UnixMilli() || job.LeaseUntil > now.UnixMilli() {
			continue
		}
		record, data, err := store.ReadRecord(ctx, namespace, job.ID)
		if err != nil {
			return events, err
		}
		var current Job
		if err := json.Unmarshal(data, &current); err != nil {
			return events, err
		}
		if current.Paused || current.NextAt > now.UnixMilli() || current.LeaseUntil > now.UnixMilli() {
			continue
		}
		if current.LeaseUntil != 0 {
			// An interrupted run requires explicit recovery before another attempt.
			current.Paused = true
			if err := Save(ctx, store, current, record.Revision); err != nil {
				return events, err
			}
			continue
		}
		// One minute of padding keeps another worker out during cleanup.
		current.LeaseUntil = now.UnixMilli() + current.TimeoutMS + 60000
		if err := Save(ctx, store, current, record.Revision); err != nil {
			return events, err
		}
		claimedRevision := record.Revision + 1
		runCtx, cancel := context.WithTimeout(ctx, time.Duration(current.TimeoutMS)*time.Millisecond)
		exit, output, runErr := run(runCtx, current)
		cancel()
		if ctx.Err() != nil {
			return events, ctx.Err()
		}
		if runErr != nil {
			output = runErr.Error()
			exit = -1
		}
		resultHash := engineering.Hash(fmt.Sprintf("%d\x00%s", exit, output))
		changed := current.LastResultHash != resultHash
		current.LastResultHash = resultHash
		current.LastExit = &exit
		current.LastAt = now.UnixMilli()
		current.NextAt = time.Now().UnixMilli() + current.PeriodMS
		current.LeaseUntil = 0
		if err := Save(ctx, store, current, claimedRevision); err != nil {
			// A pause/resume command may advance the revision during execution.
			latestRecord, data, readErr := store.ReadRecord(ctx, namespace, current.ID)
			if readErr != nil {
				return events, readErr
			}
			var latest Job
			if err := json.Unmarshal(data, &latest); err != nil {
				return events, err
			}
			if latest.LeaseUntil != now.UnixMilli()+current.TimeoutMS+60000 {
				return events, err
			}
			current.Paused = latest.Paused
			if err := Save(ctx, store, current, latestRecord.Revision); err != nil {
				return events, err
			}
		}
		if changed {
			events = append(events, Event{JobID: job.ID, ExitCode: exit, Changed: changed, OutputHash: resultHash})
		}
	}
	return events, nil
}
