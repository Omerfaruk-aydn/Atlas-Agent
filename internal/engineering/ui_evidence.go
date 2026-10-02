package engineering

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ValidateUIArtifact(ctx context.Context, evidence UIEvidence) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.Open(evidence.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 16*1024*1024 {
		return fmt.Errorf("invalid UI artifact size")
	}
	data, err := io.ReadAll(io.LimitReader(f, 16*1024*1024+1))
	if err != nil {
		return err
	}
	if len(data) > 16*1024*1024 || Hash(string(data)) != evidence.Hash {
		return fmt.Errorf("UI artifact changed after capture")
	}
	if evidence.ScenarioProof != nil {
		if !filepath.IsAbs(evidence.ScenarioStore) {
			return fmt.Errorf("scenario proof store must be absolute")
		}
		store := NewStore(filepath.Dir(evidence.ScenarioStore))
		payload, err := store.ReadArtifact(ctx, *evidence.ScenarioProof)
		if err != nil {
			return err
		}
		var saved struct {
			Run struct {
				ID        string        `json:"id"`
				Target    string        `json:"target"`
				Source    string        `json:"source_fingerprint"`
				Passed    bool          `json:"passed"`
				Observed  bool          `json:"observed"`
				Artifacts []ArtifactRef `json:"artifacts"`
				Result    struct {
					RunID    string `json:"run_id"`
					Backend  string `json:"backend"`
					Done     bool   `json:"done"`
					ExitCode *int   `json:"exit_code"`
				} `json:"result"`
			} `json:"run"`
		}
		if err := json.Unmarshal(payload, &saved); err != nil {
			return err
		}
		run := saved.Run
		if !run.Passed || !run.Observed || run.ID != evidence.ID || run.Target != evidence.Target || run.Source != evidence.SourceFingerprint {
			return fmt.Errorf("UI scenario proof identity or outcome mismatch")
		}
		found := false
		for _, ref := range run.Artifacts {
			if ref.Hash == evidence.Hash && (evidence.Target == "web" && ref.Kind == "scenario-screenshot" || evidence.Target == "tui" && ref.Kind == "pty-transcript") {
				actual, err := store.ReadArtifact(ctx, ref)
				if err != nil {
					return err
				}
				found = Hash(string(actual)) == evidence.Hash
			}
		}
		if !found {
			return fmt.Errorf("UI artifact missing from observed scenario")
		}
		if evidence.Target == "tui" && (!strings.HasPrefix(run.Result.Backend, "pty-") || run.Result.RunID != run.ID || !run.Result.Done || run.Result.ExitCode == nil) {
			return fmt.Errorf("UI scenario lacks a real terminal result")
		}
	}
	if evidence.Target == "web" {
		config, _, err := image.DecodeConfig(strings.NewReader(string(data)))
		if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 16384 || config.Height > 32768 {
			return fmt.Errorf("UI artifact is not a bounded rendered image")
		}
		if int64(config.Width)*int64(config.Height) > 32*1024*1024 {
			return fmt.Errorf("UI image exceeds decoded pixel limit")
		}
		if _, _, err := image.Decode(strings.NewReader(string(data))); err != nil {
			return fmt.Errorf("UI artifact image is incomplete: %w", err)
		}
	} else if evidence.Target != "tui" || strings.ContainsRune(string(data), 0) {
		return fmt.Errorf("TUI evidence requires a text transcript")
	}
	return ctx.Err()
}
