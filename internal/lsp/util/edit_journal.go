package util

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/lock"
	"github.com/google/uuid"
)

const maxEditJournal = 32 * 1024 * 1024

type editJournalEntry struct {
	Before []byte `json:"before,omitempty"`
	Mode   uint32 `json:"mode"`
	Status string `json:"status"`
}

type editJournal struct {
	Plan     EditPlan           `json:"plan"`
	PlanHash string             `json:"plan_hash"`
	Status   string             `json:"status"`
	Entries  []editJournalEntry `json:"entries"`
}

func journalName(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", fmt.Errorf("edit journal requires a UUID")
	}
	return contentHash([]byte(id)) + ".json", nil
}

func journalPlanHash(plan EditPlan) string {
	data, _ := json.Marshal(plan)
	return contentHash(data)
}

func openEditJournal(ctx context.Context, journalDir, root string) (*os.Root, func(), error) {
	if !filepath.IsAbs(journalDir) {
		return nil, nil, fmt.Errorf("edit journal directory must be absolute")
	}
	if err := os.MkdirAll(journalDir, 0o700); err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	release, err := lock.File(ctx, filepath.Join(journalDir, contentHash([]byte(pathKey(root)))+".lock"))
	if err != nil {
		return nil, nil, err
	}
	opened, err := os.OpenRoot(journalDir)
	if err != nil {
		release()
		return nil, nil, err
	}
	return opened, func() { _ = opened.Close(); release() }, nil
}

func atomicEditWrite(root *os.Root, relative string, content []byte, mode os.FileMode) error {
	directory := filepath.Dir(relative)
	if err := root.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary := filepath.Join(directory, ".atlas-edit-"+uuid.NewString())
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode.Perm())
	if err != nil {
		return err
	}
	defer root.Remove(temporary)
	_, err = file.Write(content)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := root.Rename(temporary, relative); err != nil {
		return err
	}
	// Unix directory fsync persists the rename. Windows does not expose the
	// equivalent through os.File.Sync; file contents are flushed there, and
	// the journal's process-crash recovery remains supported.
	if runtime.GOOS != "windows" {
		for {
			directoryFile, err := root.Open(directory)
			if err != nil {
				return err
			}
			err = directoryFile.Sync()
			closeErr := directoryFile.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if directory == "." {
				break
			}
			directory = filepath.Dir(directory)
		}
	}
	return nil
}

func saveEditJournal(root *os.Root, journal editJournal) error {
	name, err := journalName(journal.Plan.ID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	if len(data) > maxEditJournal {
		return fmt.Errorf("edit journal exceeds 32 MiB")
	}
	return atomicEditWrite(root, name, data, 0o600)
}

func readEditJournal(root *os.Root, project, id string) (editJournal, error) {
	name, err := journalName(id)
	if err != nil {
		return editJournal{}, err
	}
	info, err := root.Lstat(name)
	if err != nil {
		return editJournal{}, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxEditJournal {
		return editJournal{}, fmt.Errorf("invalid edit journal file")
	}
	file, err := root.Open(name)
	if err != nil {
		return editJournal{}, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxEditJournal+1))
	if err != nil || len(data) > maxEditJournal {
		return editJournal{}, fmt.Errorf("cannot read bounded edit journal")
	}
	var journal editJournal
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&journal); err != nil {
		return journal, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return journal, fmt.Errorf("multiple edit journal values")
	}
	if journal.Plan.ID != id || journal.PlanHash != journalPlanHash(journal.Plan) || len(journal.Entries) != len(journal.Plan.Changes) {
		return journal, fmt.Errorf("edit journal integrity mismatch")
	}
	if err := validatePlanShape(project, journal.Plan); err != nil {
		return journal, err
	}
	switch journal.Status {
	case "applying", "applied", "rolling_back", "rolled_back", "conflicted":
	default:
		return journal, fmt.Errorf("invalid edit journal status")
	}
	beforeBytes := 0
	for i, entry := range journal.Entries {
		beforeBytes += len(entry.Before)
		if len(entry.Before) > maxEditFile || beforeBytes > maxEditBytes || entry.Mode & ^uint32(0o777) != 0 {
			return journal, fmt.Errorf("edit recovery contents exceed bounds")
		}
		change := journal.Plan.Changes[i]
		if change.BeforeHash == missingFileHash {
			if len(entry.Before) != 0 {
				return journal, fmt.Errorf("unexpected create backup")
			}
		} else if contentHash(entry.Before) != change.BeforeHash {
			return journal, fmt.Errorf("edit backup integrity mismatch")
		}
		switch entry.Status {
		case "prepared", "writing", "applied", "restored", "conflicted":
		default:
			return journal, fmt.Errorf("invalid edit entry status")
		}
	}
	return journal, nil
}

func editJournalResult(journal editJournal) EditResult {
	result := EditResult{PlanID: journal.Plan.ID, Status: journal.Status, Applied: []string{}, Restored: []string{}, Conflicted: []string{}}
	for i, entry := range journal.Entries {
		path := journal.Plan.Changes[i].Path
		switch entry.Status {
		case "applied":
			result.Applied = append(result.Applied, path)
		case "restored":
			result.Restored = append(result.Restored, path)
		case "conflicted":
			result.Conflicted = append(result.Conflicted, path)
		}
	}
	return result
}

func writePlanned(root *os.Root, change PlannedChange) error {
	rel, err := editRelative(root.Name(), change.Path)
	if err != nil {
		return err
	}
	if change.Operation == "delete" {
		return root.Remove(rel)
	}
	return atomicEditWrite(root, rel, change.Content, os.FileMode(change.Mode))
}

func recoverJournal(ctx context.Context, project, journalRoot *os.Root, journal *editJournal) (EditResult, error) {
	if journal.Status == "applied" || journal.Status == "rolled_back" {
		return editJournalResult(*journal), nil
	}
	journal.Status = "rolling_back"
	if err := saveEditJournal(journalRoot, *journal); err != nil {
		return editJournalResult(*journal), err
	}
	for i := len(journal.Entries) - 1; i >= 0; i-- {
		if err := ctx.Err(); err != nil {
			return editJournalResult(*journal), err
		}
		entry := &journal.Entries[i]
		if entry.Status == "prepared" || entry.Status == "restored" {
			continue
		}
		change := journal.Plan.Changes[i]
		rel, _ := editRelative(project.Name(), change.Path)
		current, err := readEditFile(ctx, project, rel)
		if err != nil || current.hash != change.BeforeHash && current.hash != change.AfterHash {
			entry.Status = "conflicted"
		} else if current.hash == change.BeforeHash {
			entry.Status = "restored"
		} else {
			restore := PlannedChange{Path: change.Path, Content: entry.Before, Mode: entry.Mode, Operation: "write"}
			if change.BeforeHash == missingFileHash {
				restore.Operation = "delete"
			}
			if err := writePlanned(project, restore); err != nil {
				entry.Status = "conflicted"
			} else {
				entry.Status = "restored"
			}
		}
		if err := saveEditJournal(journalRoot, *journal); err != nil {
			return editJournalResult(*journal), err
		}
	}
	journal.Status = "rolled_back"
	for _, entry := range journal.Entries {
		if entry.Status == "conflicted" {
			journal.Status = "conflicted"
		}
	}
	if err := saveEditJournal(journalRoot, *journal); err != nil {
		journal.Status = "rolling_back"
		return editJournalResult(*journal), err
	}
	return editJournalResult(*journal), nil
}

func ApplyEditPlan(ctx context.Context, root, journalDir string, plan EditPlan) (EditResult, error) {
	return applyEditPlan(ctx, root, journalDir, plan, writePlanned)
}

func applyEditPlan(ctx context.Context, root, journalDir string, plan EditPlan, mutate func(*os.Root, PlannedChange) error) (EditResult, error) {
	canonical, project, err := editRoot(root)
	if err != nil {
		return EditResult{}, err
	}
	defer project.Close()
	if err := validatePlanShape(canonical, plan); err != nil {
		return EditResult{}, err
	}
	journalRoot, release, err := openEditJournal(ctx, journalDir, canonical)
	if err != nil {
		return EditResult{}, err
	}
	defer release()
	existing, err := readEditJournal(journalRoot, canonical, plan.ID)
	if err == nil {
		if existing.PlanHash != journalPlanHash(plan) {
			return editJournalResult(existing), fmt.Errorf("edit identity already has a different journal")
		}
		if existing.Status != "applied" {
			return editJournalResult(existing), fmt.Errorf("existing edit journal requires recovery; no command is replayed")
		}
		for _, change := range plan.Changes {
			rel, _ := editRelative(canonical, change.Path)
			current, err := readEditFile(ctx, project, rel)
			if err != nil || current.hash != change.AfterHash {
				return editJournalResult(existing), fmt.Errorf("previously applied edit source changed")
			}
		}
		return editJournalResult(existing), nil
	}
	if !os.IsNotExist(err) {
		return EditResult{}, err
	}
	journal := editJournal{Plan: plan, PlanHash: journalPlanHash(plan), Status: "applying"}
	total := 0
	for _, change := range plan.Changes {
		rel, _ := editRelative(canonical, change.Path)
		before, err := readEditFile(ctx, project, rel)
		if err != nil || before.hash != change.BeforeHash {
			return EditResult{PlanID: plan.ID, Status: "rejected"}, fmt.Errorf("edit source changed before apply: %s", change.Path)
		}
		total += len(before.content)
		if total > maxEditBytes {
			return EditResult{}, fmt.Errorf("edit recovery source exceeds bounds")
		}
		journal.Entries = append(journal.Entries, editJournalEntry{Before: before.content, Mode: uint32(before.mode), Status: "prepared"})
	}
	if err := saveEditJournal(journalRoot, journal); err != nil {
		return EditResult{}, err
	}
	fail := func(cause error) (EditResult, error) {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		result, err := recoverJournal(cleanup, project, journalRoot, &journal)
		return result, errors.Join(cause, err)
	}
	for i, change := range plan.Changes {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		rel, _ := editRelative(canonical, change.Path)
		current, err := readEditFile(ctx, project, rel)
		if err != nil || current.hash != change.BeforeHash {
			return fail(fmt.Errorf("edit source changed immediately before write: %s", change.Path))
		}
		journal.Entries[i].Status = "writing"
		if err := saveEditJournal(journalRoot, journal); err != nil {
			return fail(err)
		}
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		if err := mutate(project, change); err != nil {
			return fail(err)
		}
		journal.Entries[i].Status = "applied"
		if err := saveEditJournal(journalRoot, journal); err != nil {
			return fail(err)
		}
	}
	journal.Status = "applied"
	if err := saveEditJournal(journalRoot, journal); err != nil {
		journal.Status = "applying"
		return fail(err)
	}
	return editJournalResult(journal), nil
}

// RecoverEditPlan rolls back incomplete writes only when their current hashes
// still match the journal. Other content is retained and reported as conflicted.
func RecoverEditPlan(ctx context.Context, root, journalDir, id string, expected ...EditPlan) (EditResult, error) {
	canonical, project, err := editRoot(root)
	if err != nil {
		return EditResult{}, err
	}
	defer project.Close()
	journalRoot, release, err := openEditJournal(ctx, journalDir, canonical)
	if err != nil {
		return EditResult{}, err
	}
	defer release()
	journal, err := readEditJournal(journalRoot, canonical, id)
	if err != nil {
		return EditResult{}, err
	}
	if len(expected) > 1 || len(expected) == 1 && journal.PlanHash != journalPlanHash(expected[0]) {
		return EditResult{}, fmt.Errorf("recovery journal does not match approved plan")
	}
	return recoverJournal(ctx, project, journalRoot, &journal)
}

func InspectEditJournal(ctx context.Context, root, journalDir, id string) (EditResult, error) {
	canonical, project, err := editRoot(root)
	if err != nil {
		return EditResult{}, err
	}
	defer project.Close()
	journalRoot, release, err := openEditJournal(ctx, journalDir, canonical)
	if err != nil {
		return EditResult{}, err
	}
	defer release()
	journal, err := readEditJournal(journalRoot, canonical, id)
	if err != nil {
		return EditResult{}, err
	}
	return editJournalResult(journal), nil
}

// ValidateEditJournal binds recovery to the caller's immutable approved plan,
// rather than trusting a journal's independently supplied target list.
func ValidateEditJournal(ctx context.Context, root, journalDir string, plan EditPlan) error {
	canonical, project, err := editRoot(root)
	if err != nil {
		return err
	}
	defer project.Close()
	journalRoot, release, err := openEditJournal(ctx, journalDir, canonical)
	if err != nil {
		return err
	}
	defer release()
	journal, err := readEditJournal(journalRoot, canonical, plan.ID)
	if err != nil {
		return err
	}
	if journal.PlanHash != journalPlanHash(plan) {
		return fmt.Errorf("recovery journal does not match approved plan")
	}
	return nil
}

// PreviewEditRecovery returns only currently hash-matched restoration writes.
// Unmatched user content produces no proposed write and is preserved by recover.
func PreviewEditRecovery(ctx context.Context, root, journalDir, id string) ([]PlannedChange, error) {
	canonical, project, err := editRoot(root)
	if err != nil {
		return nil, err
	}
	defer project.Close()
	journalRoot, release, err := openEditJournal(ctx, journalDir, canonical)
	if err != nil {
		return nil, err
	}
	defer release()
	journal, err := readEditJournal(journalRoot, canonical, id)
	if err != nil {
		return nil, err
	}
	if journal.Status == "applied" || journal.Status == "rolled_back" {
		return nil, nil
	}
	var proposed []PlannedChange
	for i, entry := range journal.Entries {
		if entry.Status == "prepared" || entry.Status == "restored" {
			continue
		}
		change := journal.Plan.Changes[i]
		rel, _ := editRelative(canonical, change.Path)
		current, err := readEditFile(ctx, project, rel)
		if err != nil {
			return nil, err
		}
		if current.hash != change.AfterHash || current.hash == change.BeforeHash {
			continue
		}
		restore := PlannedChange{Path: change.Path, Operation: "write", BeforeHash: current.hash, AfterHash: change.BeforeHash, Content: entry.Before, Mode: entry.Mode}
		if change.BeforeHash == missingFileHash {
			restore.Operation = "delete"
		}
		proposed = append(proposed, restore)
	}
	return proposed, nil
}
