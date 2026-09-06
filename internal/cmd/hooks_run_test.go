package cmd

import (
	"bytes"
	"testing"

	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/hooks"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCanonicalHookEvent(t *testing.T) {
	got, err := canonicalHookEvent("PreToolUse")
	require.NoError(t, err)
	require.Equal(t, "PreToolUse", got)

	got, err = canonicalHookEvent("post_tool_use")
	require.NoError(t, err)
	require.Equal(t, "PostToolUse", got)

	got, err = canonicalHookEvent("UserPromptSubmit")
	require.NoError(t, err)
	require.Equal(t, "UserPromptSubmit", got)

	_, err = canonicalHookEvent("nope")
	require.Error(t, err)
}

func newHooksRunTestCmd(t *testing.T, workingDir string) *cobra.Command {
	t.Helper()
	c := newSkillTestCmd(t, runHooksRun, workingDir, t.TempDir())
	c.Flags().StringVar(&hooksRunTool, "tool", "", "")
	c.Flags().StringVar(&hooksRunInput, "input", "{}", "")
	c.Flags().StringVar(&hooksRunResponse, "response", "", "")
	c.Flags().StringVar(&hooksRunPrompt, "prompt", "", "")
	t.Cleanup(func() {
		hooksRunTool = ""
		hooksRunInput = "{}"
		hooksRunResponse = ""
		hooksRunPrompt = ""
	})
	return c
}

func TestHooksRunWithNoneConfigured(t *testing.T) {
	c := newHooksRunTestCmd(t, t.TempDir())
	var out bytes.Buffer
	c.SetOut(&out)

	require.NoError(t, c.RunE(c, []string{"PreToolUse"}))
	require.Contains(t, out.String(), "No PreToolUse hooks configured.")
}

func TestHooksRunRejectsUnknownEvent(t *testing.T) {
	c := newHooksRunTestCmd(t, t.TempDir())
	require.Error(t, c.RunE(c, []string{"NotAnEvent"}))
}

func TestHooksRunExecutesPreToolUse(t *testing.T) {
	workingDir := t.TempDir()
	writeAtlasConfig(t, workingDir, `{"hooks":{"PreToolUse":[
		{"name":"deny-it","command":"echo '{\"decision\":\"deny\",\"reason\":\"nope\"}'"}
	]}}`)

	c := newHooksRunTestCmd(t, workingDir)
	require.NoError(t, c.Flags().Set("tool", "bash"))
	var out bytes.Buffer
	c.SetOut(&out)

	require.NoError(t, c.RunE(c, []string{"PreToolUse"}))
	got := out.String()
	require.Contains(t, got, "deny-it")
	require.Contains(t, got, "deny")
	require.Contains(t, got, "result: deny")
	require.Contains(t, got, "reason: nope")
}

func TestHooksRunExecutesPostToolUse(t *testing.T) {
	workingDir := t.TempDir()
	writeAtlasConfig(t, workingDir, `{"hooks":{"PostToolUse":[
		{"name":"context-adder","command":"echo '{\"context\":\"seen\"}'"}
	]}}`)

	c := newHooksRunTestCmd(t, workingDir)
	require.NoError(t, c.Flags().Set("tool", "bash"))
	require.NoError(t, c.Flags().Set("response", "output text"))
	var out bytes.Buffer
	c.SetOut(&out)

	require.NoError(t, c.RunE(c, []string{"post_tool_use"}))
	got := out.String()
	require.Contains(t, got, "context-adder")
	require.Contains(t, got, "context: seen")
}

func TestHooksRunExecutesUserPromptSubmit(t *testing.T) {
	workingDir := t.TempDir()
	writeAtlasConfig(t, workingDir, `{"hooks":{"UserPromptSubmit":[
		{"name":"prompt-check","command":"case \"$ATLAS_AGENT_PROMPT\" in *secret*) echo '{\"decision\":\"deny\",\"reason\":\"blocked\"}' ;; esac"}
	]}}`)

	c := newHooksRunTestCmd(t, workingDir)
	require.NoError(t, c.Flags().Set("prompt", "the secret plan"))
	var out bytes.Buffer
	c.SetOut(&out)

	require.NoError(t, c.RunE(c, []string{"UserPromptSubmit"}))
	got := out.String()
	require.Contains(t, got, "prompt-check")
	require.Contains(t, got, "result: deny")
	require.Contains(t, got, "reason: blocked")
}

func TestHooksRunExecutesSessionStart(t *testing.T) {
	workingDir := t.TempDir()
	writeAtlasConfig(t, workingDir, `{"hooks":{"SessionStart":[
		{"name":"greet","command":"echo '{\"context\":\"welcome back\"}'"}
	]}}`)

	c := newHooksRunTestCmd(t, workingDir)
	var out bytes.Buffer
	c.SetOut(&out)

	require.NoError(t, c.RunE(c, []string{"session_start"}))
	got := out.String()
	require.Contains(t, got, "greet")
	require.Contains(t, got, "context: welcome back")
}

func TestHooksRunExecutesSessionEnd(t *testing.T) {
	workingDir := t.TempDir()
	writeAtlasConfig(t, workingDir, `{"hooks":{"SessionEnd":[
		{"name":"archive","command":"echo ok"}
	]}}`)

	c := newHooksRunTestCmd(t, workingDir)
	var out bytes.Buffer
	c.SetOut(&out)

	require.NoError(t, c.RunE(c, []string{"SessionEnd"}))
	require.Contains(t, out.String(), "archive")
}

func TestHooksRunExecutesPreCompact(t *testing.T) {
	workingDir := t.TempDir()
	writeAtlasConfig(t, workingDir, `{"hooks":{"PreCompact":[
		{"name":"keep-history","command":"echo '{\"decision\":\"deny\",\"reason\":\"keep it all\"}'"}
	]}}`)

	c := newHooksRunTestCmd(t, workingDir)
	var out bytes.Buffer
	c.SetOut(&out)

	require.NoError(t, c.RunE(c, []string{"PreCompact"}))
	got := out.String()
	require.Contains(t, got, "result: deny")
	require.Contains(t, got, "reason: keep it all")
}

func TestPrintHookResultWithNoMatches(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, printHookResult(&out, hooks.AggregateResult{}))
	require.Contains(t, out.String(), "No hooks matched.")
}
