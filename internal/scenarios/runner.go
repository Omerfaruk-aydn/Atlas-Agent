package scenarios

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/engineering"
	"github.com/google/uuid"
)

func fingerprint(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func Run(ctx context.Context, scenario Scenario, backend Backend) (ScenarioRun, error) {
	run := ScenarioRun{ID: uuid.NewString(), ScenarioID: scenario.ID, Target: scenario.Target, Status: "unavailable", Artifacts: []engineering.ArtifactRef{}, Gaps: []string{}}
	if err := Validate(ctx, scenario); err != nil {
		run.Status = "rejected"
		return run, err
	}
	proof, ok := backend.(EvidenceBackend)
	if !ok || proof == nil {
		run.Gaps = append(run.Gaps, "source-bound observation backend unavailable")
		return run, nil
	}
	timeout := 10 * time.Minute
	if scenario.TimeoutMS > 0 {
		timeout = time.Duration(scenario.TimeoutMS) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	source, err := proof.Source(ctx)
	if err != nil {
		run.Status = "failed"
		return run, fmt.Errorf("cannot establish complete scenario source: %w", err)
	}
	if !fingerprint(source) {
		run.Status = "failed"
		return run, fmt.Errorf("invalid scenario source fingerprint")
	}
	scenario.RunID = run.ID
	observed, err := proof.Run(ctx, scenario)
	observed.ID, observed.ScenarioID, observed.Target, observed.SourceFingerprint = run.ID, scenario.ID, scenario.Target, source
	if err != nil {
		observed.Passed = false
		observed.Status = "failed"
		return observed, err
	}
	if !observed.Observed {
		observed.Passed = false
		if observed.Status != "unavailable" {
			observed.Status = "failed"
		}
		return observed, nil
	}
	if len(observed.Artifacts) > 128 {
		observed.Passed = false
		observed.Status = "failed"
		return observed, fmt.Errorf("scenario artifacts exceed bounds")
	}
	actualSource, err := proof.Source(ctx)
	if err != nil || actualSource != source {
		observed.Passed = false
		observed.Status = "stale"
		observed.Gaps = append(observed.Gaps, "source changed during scenario observation")
		return observed, err
	}
	hasTranscript, hasScreenshot := false, false
	for _, ref := range observed.Artifacts {
		data, err := proof.ReadArtifact(ctx, ref)
		if err != nil || engineering.Hash(string(data)) != ref.Hash || int64(len(data)) != ref.Size {
			observed.Passed = false
			observed.Status = "failed"
			return observed, fmt.Errorf("scenario artifact integrity failed")
		}
		hasTranscript = hasTranscript || ref.Kind == "pty-transcript" && len(data) > 0 && !strings.ContainsRune(string(data), 0)
		hasScreenshot = hasScreenshot || ref.Kind == "scenario-screenshot" && len(data) > 0
	}
	if scenario.Target == "tui" && (!hasTranscript || !strings.HasPrefix(observed.Result.Backend, "pty-") || observed.Result.RunID != run.ID || !observed.Result.Done || observed.Result.ExitCode == nil) || scenario.Target == "web" && !hasScreenshot {
		observed.Passed, observed.Status = false, "failed"
		observed.Gaps = append(observed.Gaps, "required real observation evidence missing")
	}
	if observed.Passed {
		observed.Status = "passed"
	} else if observed.Status == "passed" {
		observed.Status = "failed"
	}
	return observed, nil
}

func Current(ctx context.Context, run ScenarioRun, backend EvidenceBackend) error {
	if !run.Passed || !run.Observed || !fingerprint(run.SourceFingerprint) {
		return fmt.Errorf("scenario has no passed observed proof")
	}
	actual, err := backend.Source(ctx)
	if err != nil {
		return err
	}
	if actual != run.SourceFingerprint {
		return fmt.Errorf("scenario source changed; recapture required")
	}
	for _, ref := range run.Artifacts {
		data, err := backend.ReadArtifact(ctx, ref)
		if err != nil {
			return err
		}
		if engineering.Hash(string(data)) != ref.Hash || int64(len(data)) != ref.Size {
			return fmt.Errorf("scenario artifact changed; recapture required")
		}
	}
	return nil
}
