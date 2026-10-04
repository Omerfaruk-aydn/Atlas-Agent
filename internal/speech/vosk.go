package speech

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var voskModels = []struct{ language, folder string }{
	{"en-US", "vosk-model-small-en-us-0.15"},
	{"tr-TR", "vosk-model-small-tr-0.3"},
	{"it-IT", "vosk-model-small-it-0.22"},
	{"fr-FR", "vosk-model-small-fr-0.22"},
}

func voskDirectories(o DictationOptions) (models, runtime string, err error) {
	home := os.Getenv("ATLAS_AGENT_SPEECH_HOME")
	if home == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", "", err
		}
		home = filepath.Dir(exe)
	}
	models = o.ModelDir
	if models == "" {
		models = filepath.Join(home, "speech-models")
	}
	runtime = o.RuntimeDir
	if runtime == "" {
		runtime = filepath.Join(home, "speech-runtime")
	}
	models, err = filepath.Abs(models)
	if err != nil {
		return "", "", err
	}
	runtime, err = filepath.Abs(runtime)
	return models, runtime, err
}

func regularModelFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

func validVoskModel(dir string) bool {
	// Older Turkish releases use a flat layout supported by Vosk itself.
	if regularModelFile(filepath.Join(dir, "final.mdl")) {
		return regularModelFile(filepath.Join(dir, "mfcc.conf")) && regularModelFile(filepath.Join(dir, "HCLr.fst")) && regularModelFile(filepath.Join(dir, "Gr.fst"))
	}
	return regularModelFile(filepath.Join(dir, "am", "final.mdl")) && regularModelFile(filepath.Join(dir, "conf", "mfcc.conf")) && regularModelFile(filepath.Join(dir, "graph", "HCLr.fst")) && regularModelFile(filepath.Join(dir, "graph", "Gr.fst"))
}

func findVosk(o DictationOptions) (Status, string, string, error) {
	status := Status{Backend: "vosk", Installed: []string{}}
	models, runtimeDir, err := voskDirectories(o)
	if err != nil {
		return status, "", "", err
	}
	requested := o.Language
	if requested == "" {
		requested = "en-US"
	}
	var selected string
	for _, model := range voskModels {
		path := filepath.Join(models, model.folder)
		if !validVoskModel(path) {
			continue
		}
		status.Installed = append(status.Installed, model.language)
		if requested == model.language || requested == strings.Split(model.language, "-")[0] {
			status.Language, selected = model.language, path
		}
	}
	if selected == "" {
		return status, "", runtimeDir, fmt.Errorf("%w: Vosk language %s; models directory %s", ErrNoRecognizer, requested, models)
	}
	for _, library := range []string{"libvosk.dll", "libgcc_s_seh-1.dll", "libstdc++-6.dll", "libwinpthread-1.dll"} {
		if !regularModelFile(filepath.Join(runtimeDir, library)) {
			return status, selected, runtimeDir, fmt.Errorf("%w: missing Vosk runtime %s in %s", ErrNoRecognizer, library, runtimeDir)
		}
	}
	return status, selected, runtimeDir, nil
}
