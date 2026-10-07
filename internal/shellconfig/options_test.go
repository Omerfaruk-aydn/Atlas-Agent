package shellconfig

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOptionUILanguage(t *testing.T) {
	t.Parallel()
	for _, code := range []string{"en", "tr", "de", "fr", "it", "ar"} {
		data, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte("option ui language "+code))
		require.NoError(t, err)
		var result struct {
			Options struct {
				TUI struct {
					Language string `json:"language"`
				} `json:"tui"`
			} `json:"options"`
		}
		require.NoError(t, json.Unmarshal(data, &result))
		require.Equal(t, code, result.Options.TUI.Language)
	}
	_, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte("option ui language xx"))
	require.Error(t, err)
}

func TestOfflineVoiceOptions(t *testing.T) {
	t.Parallel()
	data, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte("option voice language de-DE\noption voice max-seconds 45"))
	require.NoError(t, err)
	var result struct {
		Options struct {
			Voice struct {
				Language   string `json:"language"`
				MaxSeconds int    `json:"max_seconds"`
			} `json:"voice"`
		} `json:"options"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.Equal(t, "de-DE", result.Options.Voice.Language)
	require.Equal(t, 45, result.Options.Voice.MaxSeconds)
	for _, script := range []string{"option voice max-seconds 0", "option voice max-seconds 181", "option voice language invalid/code", "option voice api-key secret", "option voice mode local"} {
		_, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte(script))
		require.Error(t, err, "unsupported voice settings must be rejected")
	}
}

func TestVoskVoiceOptions(t *testing.T) {
	t.Parallel()
	data, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte("option voice backend vosk\noption voice model-dir 'D:/Atlas/.atlas/speech-models'\noption voice runtime-dir 'D:/Atlas/.atlas/speech-runtime'\noption voice language tr-TR"))
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.Unmarshal(data, &result))
	voice := result["options"].(map[string]any)["voice"].(map[string]any)
	require.Equal(t, "vosk", voice["backend"])
	require.Equal(t, "D:/Atlas/.atlas/speech-models", voice["model_dir"])
	require.Equal(t, "D:/Atlas/.atlas/speech-runtime", voice["runtime_dir"])
	_, err = LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte("option voice backend cloud"))
	require.Error(t, err)
}

func TestNotificationSoundOptions(t *testing.T) {
	t.Parallel()
	data, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte("option notifications sound\noption notification-sound permission 'C:/Sesler/izin.wav'\noption notification-sound finished 'C:/Sesler/bitti.wav'"))
	require.NoError(t, err)
	var result struct {
		Options struct {
			Notifications      string            `json:"notifications"`
			NotificationSounds map[string]string `json:"notification_sounds"`
		} `json:"options"`
	}
	require.NoError(t, json.Unmarshal(data, &result))
	require.Equal(t, "sound", result.Options.Notifications)
	require.Equal(t, map[string]string{"permission": "C:/Sesler/izin.wav", "finished": "C:/Sesler/bitti.wav"}, result.Options.NotificationSounds)
	for _, script := range []string{"option notification-sound error x.wav", "option notification-sound finished", "option notification-sound"} {
		_, err := LoadShellConfig(t.Context(), filepath.Join(t.TempDir(), "atlasrc"), []byte(script))
		require.Error(t, err, script)
	}
}

func TestOption_Bool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option debug true
option progress false`
	path := filepath.Join(dir, "atlasrc")

	jsonBytes, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result))

	opts := result["options"].(map[string]any)
	require.Equal(t, true, opts["debug"])
	require.Equal(t, false, opts["progress"])
}

func TestOption_BoolCaseInsensitive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option debug TRUE
option progress False
option metrics YES`
	path := filepath.Join(dir, "atlasrc")

	jsonBytes, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result))

	opts := result["options"].(map[string]any)
	require.Equal(t, true, opts["debug"])
	require.Equal(t, false, opts["progress"])
	require.Equal(t, false, opts["disable_metrics"])
}

func TestOption_String(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option data-directory .atlas
option notifications osc`
	path := filepath.Join(dir, "atlasrc")

	jsonBytes, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result))

	opts := result["options"].(map[string]any)
	require.Equal(t, ".atlas", opts["data_directory"])
	require.Equal(t, "osc", opts["notifications"])
}

func TestOption_List(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option context-path .cursorrules
option context-path ATLAS-AGENT.md`
	path := filepath.Join(dir, "atlasrc")

	jsonBytes, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result))

	opts := result["options"].(map[string]any)
	paths := opts["context_paths"].([]any)
	require.Len(t, paths, 2)
	require.Equal(t, ".cursorrules", paths[0])
	require.Equal(t, "ATLAS-AGENT.md", paths[1])
}

func TestOption_Reset(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option skill-path ./a
option skill-path ./b
option reset skill-path`
	path := filepath.Join(dir, "atlasrc")

	jsonBytes, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result))

	opts := result["options"].(map[string]any)
	require.Empty(t, opts["skills_paths"].([]any))
}

func TestOption_ResetThenReadd(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option skill-path ./inherited-a
option skill-path ./inherited-b
option reset skill-path
option skill-path ./mine`
	path := filepath.Join(dir, "atlasrc")

	jsonBytes, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result))

	opts := result["options"].(map[string]any)
	paths := opts["skills_paths"].([]any)
	require.Len(t, paths, 1)
	require.Equal(t, "./mine", paths[0])
}

func TestOption_ResetUnknownKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option reset bogus-key`
	path := filepath.Join(dir, "atlasrc")

	_, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown key")
}

func TestOption_ResetNonListKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option reset debug`
	path := filepath.Join(dir, "atlasrc")

	_, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.Error(t, err)
	require.Contains(t, err.Error(), "not one")
}

func TestOption_UIUnknownKey(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "atlasrc")
	_, err := LoadShellConfig(t.Context(), path, []byte(`option ui bogus true`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown key")
}

func TestOption_UIExitBanner(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "atlasrc")
	jsonBytes, err := LoadShellConfig(t.Context(), path, []byte(`option ui exit-banner compact`))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result))

	ui := result["options"].(map[string]any)["tui"].(map[string]any)
	require.Equal(t, "compact", ui["exit_banner"])

	_, err = LoadShellConfig(t.Context(), path, []byte(`option ui exit-banner bogus`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "expects default, compact, or none")
}

func TestOption_BoolShorthand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option debug
option metrics`
	path := filepath.Join(dir, "atlasrc")

	jsonBytes, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result))

	opts := result["options"].(map[string]any)
	require.Equal(t, true, opts["debug"])
	require.Equal(t, false, opts["disable_metrics"])
}

func TestOption_InvertedBool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option metrics false`
	path := filepath.Join(dir, "atlasrc")

	jsonBytes, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal(jsonBytes, &result))

	opts := result["options"].(map[string]any)
	require.Equal(t, true, opts["disable_metrics"])
}

func TestOption_UnknownKey(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := `option bogus-key value`
	path := filepath.Join(dir, "atlasrc")

	_, err := LoadShellConfig(t.Context(), path, []byte(script))
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown key")
}
