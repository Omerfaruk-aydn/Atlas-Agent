package localapps

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// BeginUpdate starts an update task on an existing app: it backs up the
// app's files and data, then marks the app as updating. The running copy
// keeps serving the old code until the update is verified.
func (m *Manager) BeginUpdate(ctx context.Context, id, task string) (BackupInfo, error) {
	release, err := m.acquire(id, "update")
	if err != nil {
		return BackupInfo{}, err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return BackupInfo{}, err
	}
	switch rec.State {
	case StateDesigning, StateVerifying:
		return BackupInfo{}, opErr(ErrInvalidState, "Finish creating the app first.", "the app is %s", rec.State)
	case StateUpdating:
		return BackupInfo{}, opErr(ErrInvalidState, "Finish or roll back the current update first.", "an update is already in progress: %s", rec.Update.Task)
	}
	task = strings.TrimSpace(task)
	if task == "" {
		return BackupInfo{}, opErr(ErrInvalidState, "Describe the change.", "the update task is empty")
	}
	m.startJournal(id, "update")
	backup, err := m.backupLocked(ctx, rec, "before-update", true)
	if err != nil {
		m.finishJournal(id, err)
		return BackupInfo{}, fmt.Errorf("the backup before the update failed; nothing was changed: %w", err)
	}
	prev := rec.State
	if rec.Update != nil {
		// A failed update is being retried; keep the original backup and
		// state so a rollback still returns to the last good version.
		prev, backup.ID = rec.Update.PrevState, rec.Update.BackupID
	}
	_, err = m.mutate(ctx, id, func(r *Record) error {
		r.Update = &UpdateRecord{Task: task, StartedAt: time.Now(), BackupID: backup.ID, PrevState: prev}
		r.State = StateUpdating
		return nil
	})
	m.finishJournal(id, err)
	return backup, err
}

// FinishUpdate verifies the updated app. On success the update is accepted
// and a running copy is restarted on the new code; on failure the app is
// marked update_failed and the last good version stays in the backup.
func (m *Manager) FinishUpdate(ctx context.Context, id string) (Verification, string, error) {
	release, err := m.acquire(id, "update")
	if err != nil {
		return Verification{}, "", err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return Verification{}, "", err
	}
	if rec.Update == nil {
		return Verification{}, "", opErr(ErrInvalidState, "Begin an update first.", "no update is in progress")
	}
	m.startJournal(id, "update")
	v, err := m.verifyLocked(ctx, rec)
	if err != nil {
		m.finishJournal(id, err)
		return v, "", err
	}
	if !v.Passed {
		m.finishJournal(id, fmt.Errorf("%s", v.Error))
		return v, fmt.Sprintf("The update did not pass verification, so it was not accepted. The previous version is kept in backup %s; roll back to restore it or fix the problem and finish again.", rec.Update.BackupID), nil
	}
	msg := "The update passed verification."
	wasRunning := m.view(rec, false).Running
	if _, err := m.mutate(ctx, id, func(r *Record) error {
		r.Update = nil
		return nil
	}); err != nil {
		m.finishJournal(id, err)
		return v, "", err
	}
	if wasRunning {
		fresh, _ := m.record(id)
		if _, err := m.stopLocked(ctx, fresh); err == nil {
			fresh, _ = m.record(id)
			if res, err := m.startLocked(ctx, fresh, StartOptions{}); err == nil {
				msg += " The running app was restarted on the new version at " + res.URL + "."
			} else {
				msg += " The app could not be restarted: " + err.Error()
			}
		}
	}
	m.finishJournal(id, nil)
	return v, msg, nil
}

// Rollback restores the files and data saved when the update began and
// verifies the result.
func (m *Manager) Rollback(ctx context.Context, id string) (string, error) {
	release, err := m.acquire(id, "rollback")
	if err != nil {
		return "", err
	}
	defer release()
	rec, err := m.record(id)
	if err != nil {
		return "", err
	}
	if rec.Update == nil {
		return "", opErr(ErrInvalidState, "", "there is no update to roll back")
	}
	m.startJournal(id, "rollback")
	msg, err := m.restoreLocked(ctx, rec, rec.Update.BackupID, RestoreOptions{Source: true})
	if err != nil {
		m.finishJournal(id, err)
		return "", err
	}
	if _, err := m.mutate(ctx, id, func(r *Record) error {
		r.Update = nil
		r.State = StateScaffolded
		return nil
	}); err != nil {
		m.finishJournal(id, err)
		return "", err
	}
	fresh, err := m.record(id)
	if err != nil {
		return "", err
	}
	v, err := m.verifyLocked(ctx, fresh)
	m.finishJournal(id, err)
	if err != nil {
		return msg, err
	}
	if v.Passed {
		return msg + " The restored version passed verification.", nil
	}
	return msg + " The restored version did not pass verification: " + v.Error, nil
}
