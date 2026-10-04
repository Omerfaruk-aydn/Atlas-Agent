package speech

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type speechAsset struct {
	URL, SHA256, Folder string
	Required            []string
	Runtime             bool
}

var speechAssets = []speechAsset{
	{URL: "https://alphacephei.com/vosk/models/vosk-model-small-tr-0.3.zip", SHA256: "8c8d07cec1bce31add14967c16891c84152cdf76d391e33c08278119b9ea96e5", Folder: "vosk-model-small-tr-0.3", Required: []string{"final.mdl", "mfcc.conf", "HCLr.fst", "Gr.fst"}},
	{URL: "https://alphacephei.com/vosk/models/vosk-model-small-en-us-0.15.zip", SHA256: "30f26242c4eb449f948e42cb302dd7a686cb29a3423a8367f99ff41780942498", Folder: "vosk-model-small-en-us-0.15", Required: []string{"am/final.mdl", "conf/mfcc.conf", "graph/HCLr.fst", "graph/Gr.fst"}},
	{URL: "https://alphacephei.com/vosk/models/vosk-model-small-it-0.22.zip", SHA256: "9ec65e75861d1c6c2e457cccd932705340dcdf233f5b239f00733b4de0bf3267", Folder: "vosk-model-small-it-0.22", Required: []string{"am/final.mdl", "conf/mfcc.conf", "graph/HCLr.fst", "graph/Gr.fst"}},
	{URL: "https://alphacephei.com/vosk/models/vosk-model-small-fr-0.22.zip", SHA256: "cabf6180e177eb9b3a9a9d43a437bd5e549f3a7d09525e5d69a3fed787be12ad", Folder: "vosk-model-small-fr-0.22", Required: []string{"am/final.mdl", "conf/mfcc.conf", "graph/HCLr.fst", "graph/Gr.fst"}},
	{URL: "https://github.com/alphacep/vosk-api/releases/download/v0.3.45/vosk-win64-0.3.45.zip", SHA256: "f1dcc9cca460630f81ea8f71794f69c80bed6556d2a4e6237b5785e1d2dff34b", Folder: "vosk-win64-0.3.45", Required: []string{"libvosk.dll", "libgcc_s_seh-1.dll", "libstdc++-6.dll", "libwinpthread-1.dll"}, Runtime: true},
}

// InstallVosk downloads pinned official archives without administrator access.
// Complete existing assets are reused; incomplete destinations are preserved.
func InstallVosk(ctx context.Context, o DictationOptions, progress func(string)) (Status, error) {
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return Status{}, ErrUnsupported
	}
	o.Backend = "vosk"
	if err := o.Validate(); err != nil {
		return Status{}, err
	}
	models, runtimeDir, err := voskDirectories(o)
	if err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	client := &http.Client{Timeout: 5 * time.Minute}
	for _, asset := range speechAssets {
		dest := filepath.Join(models, asset.Folder)
		if asset.Runtime {
			dest = runtimeDir
		}
		if progress != nil {
			progress("Checking/downloading " + asset.Folder)
		}
		if err := installSpeechAsset(ctx, client, asset, dest); err != nil {
			return Status{}, fmt.Errorf("install %s: %w", asset.Folder, err)
		}
	}
	return Check(ctx, o)
}

func completeSpeechAsset(asset speechAsset, dir string) bool {
	if len(asset.Required) == 0 {
		return false
	}
	for _, name := range asset.Required {
		if !regularModelFile(filepath.Join(dir, filepath.FromSlash(name))) {
			return false
		}
	}
	return true
}

func installSpeechAsset(ctx context.Context, client *http.Client, asset speechAsset, dest string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if completeSpeechAsset(asset, dest) {
		return nil
	}
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("incomplete destination %s already exists; move it aside before retrying", dest)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(dest), ".atlas-speech-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned HTTP %d", response.StatusCode)
	}
	archivePath := filepath.Join(stage, "download.zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	hash := sha256.New()
	const archiveLimit = 128 * 1024 * 1024
	n, copyErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(response.Body, archiveLimit+1))
	closeErr := archive.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if n > archiveLimit || !strings.EqualFold(fmt.Sprintf("%x", hash.Sum(nil)), asset.SHA256) {
		return fmt.Errorf("archive size or SHA256 verification failed")
	}
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()
	if len(reader.File) > 256 {
		return fmt.Errorf("archive has too many entries")
	}
	extracted := filepath.Join(stage, "extracted")
	var total uint64
	for _, entry := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSuffix(entry.Name, "/")
		if name == "" || path.Clean(name) != name || strings.ContainsAny(name, "\\:") || strings.HasPrefix(name, "/") || entry.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsafe archive entry")
		}
		if name == asset.Folder && entry.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(name, asset.Folder+"/") {
			return fmt.Errorf("unexpected archive root")
		}
		rel := strings.TrimPrefix(name, asset.Folder+"/")
		total += entry.UncompressedSize64
		if total > 512*1024*1024 {
			return fmt.Errorf("extracted archive exceeds size limit")
		}
		if asset.Runtime {
			allowed := false
			for _, required := range asset.Required {
				allowed = allowed || rel == required
			}
			if !allowed {
				continue
			}
		}
		target := filepath.Join(extracted, filepath.FromSlash(rel))
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := extractSpeechFile(entry, target); err != nil {
			return err
		}
	}
	if !completeSpeechAsset(asset, extracted) {
		return fmt.Errorf("archive is missing required model/runtime files")
	}
	return os.Rename(extracted, dest)
}

func extractSpeechFile(entry *zip.File, target string) error {
	source, err := entry.Open()
	if err != nil {
		return err
	}
	defer source.Close()
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, io.LimitReader(source, 512*1024*1024+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}
