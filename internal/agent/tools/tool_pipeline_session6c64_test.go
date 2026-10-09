package tools

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/stretchr/testify/require"
)

// session6c64BrokenDesktop is the second tool_pipeline call of session
// 6c6446a0 byte for byte: desktop arrived as a string whose encoded sequence
// object is followed by one stray "}".
const session6c64BrokenDesktop = `{"desktop": "{\"mode\":\"sequence\",\"result\":\"checkpoints\",\"steps\":[{\"input\":{\"action\":\"type\",\"text\":\"1234*56=\",\"automation\":{\"window_id\":\"28836010\"}},\"checkpoint\":{\"window_id\":\"28836010\",\"element_id\":\"42:14550362:4:10\",\"condition\":\"text\",\"expected\":\"Ekran değeri 69.104\",\"wait_ms\":3000}},{\"input\":{\"action\":\"type\",\"text\":\"/8=\",\"automation\":{\"window_id\":\"28836010\"}},\"checkpoint\":{\"window_id\":\"28836010\",\"element_id\":\"42:14550362:4:10\",\"condition\":\"text\",\"expected\":\"Ekran değeri 8.638\",\"wait_ms\":3000}},{\"input\":{\"action\":\"type\",\"text\":\"+362=\",\"automation\":{\"window_id\":\"28836010\"}},\"checkpoint\":{\"window_id\":\"28836010\",\"element_id\":\"42:14550362:4:10\",\"condition\":\"text\",\"expected\":\"Ekran değeri 9.000\",\"wait_ms\":3000}}]}}"}`

// session6c64Corrected is the same plan sent the way the schema asks: the
// desktop value is the object itself.
const session6c64Corrected = `{"desktop":{"mode":"sequence","result":"checkpoints","steps":[{"input":{"action":"type","text":"1234*56=","automation":{"window_id":"28836010"}},"checkpoint":{"window_id":"28836010","element_id":"42:14550362:4:10","condition":"text","expected":"Ekran değeri 69.104","wait_ms":3000}},{"input":{"action":"type","text":"/8=","automation":{"window_id":"28836010"}},"checkpoint":{"window_id":"28836010","element_id":"42:14550362:4:10","condition":"text","expected":"Ekran değeri 8.638","wait_ms":3000}},{"input":{"action":"type","text":"+362=","automation":{"window_id":"28836010"}},"checkpoint":{"window_id":"28836010","element_id":"42:14550362:4:10","condition":"text","expected":"Ekran değeri 9.000","wait_ms":3000}}]}}`

// calculatorReplies answers the sequence children like the session's
// calculator: typing succeeds and every checkpoint reads the expected value.
func calculatorReplies(call fantasy.ToolCall) fantasy.ToolResponse {
	var p ComputerParams
	_ = json.Unmarshal([]byte(call.Input), &p)
	switch p.Action {
	case "assert":
		data, _ := json.Marshal(map[string]any{"passed": true, "actual": p.Automation.Expected, "condition": p.Automation.Condition, "window_id": p.Automation.WindowID})
		return fantasy.NewTextResponse(string(data))
	case "windows":
		return fantasy.NewTextResponse(`{"result":[{"window_id":"28836010","name":"Hesap Makinesi","process_name":"ApplicationFrameHost.exe","process_id":15292,"foreground":true}]}`)
	}
	return fantasy.NewTextResponse(`{"result":{"ok":true},"window_id":"28836010"}`)
}

func session6c64Ctx(t *testing.T) *recordingInvoke {
	t.Helper()
	return &recordingInvoke{reply: calculatorReplies}
}

// The broken call is refused before any child call, with a repair hint
// about the actual mistake, a mode-specific example and the structured
// contract in metadata. Its identical repeat is refused as an argument
// error: no focus advice and no fresh observation. The corrected call runs.
func TestSession6c64BrokenDesktopRepairPath(t *testing.T) {
	t.Parallel()
	rec := session6c64Ctx(t)
	guarded := WithDesktopGuard(NewToolPipeline(rec.invoke))
	ctx := guardCtx(t)
	call := fantasy.ToolCall{ID: "toolu_broken", Name: ToolPipelineName, Input: session6c64BrokenDesktop}

	first, err := guarded.Run(ctx, call)
	require.NoError(t, err)
	require.True(t, first.IsError)
	require.True(t, strings.HasPrefix(first.Content, "invalid_argument_type:"), first.Content)
	contract := pipelineContractOf(t, first)
	require.Equal(t, "desktop", contract.Field)
	require.False(t, contract.InputSent)
	require.Equal(t, "none", contract.Effect)
	require.False(t, contract.FreshObservation)
	require.Contains(t, contract.Message, `mode "sequence"`, "the hint names what the string held")
	require.Contains(t, contract.NextStep, "extra")
	require.NotContains(t, strings.ToLower(contract.NextStep), "focus")
	var example struct {
		Desktop map[string]any `json:"desktop"`
	}
	require.NoError(t, json.Unmarshal([]byte(contract.Example), &example), contract.Example)
	require.Equal(t, "sequence", example.Desktop["mode"], "the example matches the mode the model used")
	require.Contains(t, first.Metadata, `"tool_contract"`, "the structured contract survives the decode path")
	require.Contains(t, first.Metadata, `"class":"argument"`)
	require.Zero(t, rec.count())

	second, err := guarded.Run(ctx, call)
	require.NoError(t, err)
	repeat := contractOf(t, second)
	require.Equal(t, "repeat_blocked", repeat.Code)
	require.False(t, repeat.FreshObservation, "an argument error is not fixed by observing")
	require.False(t, repeat.InputSent)
	require.NotContains(t, strings.ToLower(repeat.NextStep), "focus")
	require.Contains(t, repeat.Message, "invalid_argument_type")
	require.Equal(t, "desktop", repeat.Field)
	require.Equal(t, contract.Example, repeat.Example, "the repeat keeps the original repair example")
	require.Zero(t, rec.count())

	fixed, err := guarded.Run(ctx, fantasy.ToolCall{ID: "toolu_fixed", Name: ToolPipelineName, Input: session6c64Corrected})
	require.NoError(t, err)
	require.False(t, fixed.IsError, fixed.Content)
	require.Contains(t, fixed.Content, `"completed_steps":3`, "all three results were read back")
	require.NotZero(t, rec.count(), "the corrected call reached the desktop children")
	require.NotContains(t, fixed.Content, "argument_normalization")
}

// placeholderValue replaces example placeholders with values the runtime
// accepts, so the example can be run through the real decoder and preflight.
var placeholderPattern = regexp.MustCompile(`<[^<>"]+>`)

func fillPlaceholders(example string) string {
	return placeholderPattern.ReplaceAllStringFunc(example, func(p string) string {
		switch {
		case strings.Contains(p, "element_id"):
			return "42:1"
		case strings.Contains(p, "window_id"):
			return "11"
		default:
			return "x"
		}
	})
}

// Every repair example the type contract can return passes the real
// decoder and the recipe preflight: run with a scripted desktop, it reaches
// at least one child call instead of an argument refusal.
func TestPipelineTypeErrorExamplesRunThroughDecoder(t *testing.T) {
	t.Parallel()
	for _, mode := range desktopExampleModes() {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			example := desktopModeExample(mode)
			require.NotContains(t, example, `"desktop":"`, "the example sends an object")
			// Placeholders are deliberately not window handles; filled in,
			// the example passes the same decoder a model call meets.
			var decoded PipelineParams
			require.NoError(t, json.Unmarshal([]byte(fillPlaceholders(example)), &decoded), example)
			require.NotNil(t, decoded.Desktop)
			require.Equal(t, mode, decoded.Desktop.Mode)
			require.Empty(t, decoded.normalized)

			rec := &recordingInvoke{reply: calculatorReplies}
			resp := runPipelineInput(t, fillPlaceholders(example), rec)
			require.NotZero(t, rec.count(), "%s example was refused before dispatch: %s", mode, resp.Content)
		})
	}
}

// A refused generic step is named by index and id, so the model fixes that
// step; nothing of the plan runs.
func TestPipelinePlanErrorNamesTheStep(t *testing.T) {
	t.Parallel()
	rec := &recordingInvoke{reply: calculatorReplies}
	resp := runPipelineInput(t, `{"steps":[{"id":"a","tool":"computer","arguments":{"action":"windows"}},{"id":"b","tool":"computer","arguments":{"action":"windows"}},{"id":"c","tool":"computer","arguments":{"action":"windows"},"if_success":"later"}]}`, rec)
	require.True(t, resp.IsError)
	require.Contains(t, resp.Content, `steps[2] (id "c"): if_success must reference an earlier step. No step of this pipeline was run`)
	require.Zero(t, rec.count())
}
