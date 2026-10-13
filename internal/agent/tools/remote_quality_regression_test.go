package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	fantasy "github.com/Omerfaruk-aydn/Atlas-Agent/internal/deps/atlas-llm"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/nodes"
	"github.com/Omerfaruk-aydn/Atlas-Agent/internal/support"
	"github.com/stretchr/testify/require"
)

func TestRemoteQualityRemoteGuardNeverTreatsRetryAsUserConsent(t *testing.T) {
	b := &countingTool{name: BrowserToolName}
	g := WithRemoteLocalGuard(b, func() (string, string) { return "Remote PC", "ss-review" }, NewRemoteGuardAcks())
	call := fantasy.ToolCall{Name: BrowserToolName, Input: `{"action":"navigate","url":"https://example.com"}`}
	for range 2 {
		_, err := g.Run(t.Context(), call)
		require.NoError(t, err)
	}
	require.Zero(t, b.runs, "a model retry is not an explicit request to act locally")
}

type reviewWaiter struct {
	raw      json.RawMessage
	deadline bool
}

func (w *reviewWaiter) Observe(context.Context, bool) (nodes.DesktopObservation, error) {
	return nodes.DesktopObservation{}, nil
}

func (w *reviewWaiter) Automation(ctx context.Context, _ nodes.AutomationRequest) (json.RawMessage, error) {
	_, w.deadline = ctx.Deadline()
	return w.raw, nil
}

func TestRemoteQualityTruncatedTreeDoesNotProveTextGone(t *testing.T) {
	w := &reviewWaiter{raw: json.RawMessage(`{"count":0,"matches":[],"truncated":true}`)}
	r, _, err := remoteWait(t.Context(), w, "text_gone", "Generating", "wnd", time.Millisecond, time.Millisecond)
	require.False(t, r.Met, "an incomplete UIA scan is not absence evidence")
	_ = err
}

func TestRemoteQualityInvalidTreeDoesNotProveTextGone(t *testing.T) {
	w := &reviewWaiter{raw: json.RawMessage(`{"error":"accessibility_unavailable"}`)}
	r, _, err := remoteWait(t.Context(), w, "text_gone", "Generating", "wnd", time.Millisecond, time.Millisecond)
	require.False(t, r.Met, "provider failure must not be read as absent text")
	_ = err
}

func TestRemoteQualityWaitPropagatesOwnTimeoutToBackend(t *testing.T) {
	w := &reviewWaiter{raw: json.RawMessage(`{"count":1,"matches":[{}]}`)}
	_, _, err := remoteWait(t.Context(), w, "text", "Generating", "wnd", time.Millisecond, time.Millisecond)
	require.NoError(t, err)
	require.True(t, w.deadline, "timeout must also bound the awaited remote request")
}

func TestRemoteQualityRevisedCandidateDoesNotReuseOldPackage(t *testing.T) {
	dir := t.TempDir()
	s := mobileSystem()
	_, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &s}, time.Now)
	require.NoError(t, err)
	writePNG(t, filepath.Join(dir, "screen.png"), 393, 852)
	p := DesignSystemParams{Action: "package", Name: "draft", Screens: []string{"screen.png"}}
	_, err = runDesignSystem(t.Context(), dir, p, time.Now)
	require.NoError(t, err)
	s.Direction.Name = "Completely different visual direction"
	s.Colors["accent"] = "#FF0000"
	_, err = runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &s}, time.Now)
	require.NoError(t, err)
	_, err = runDesignSystem(t.Context(), dir, p, time.Now)
	require.Error(t, err, "changed candidate tokens must not be reported as the old unchanged delivery")
}

func TestRemoteQualityChangedOpenQuestionsDoesNotReuseOldPackage(t *testing.T) {
	dir := t.TempDir()
	s := mobileSystem()
	_, err := runDesignSystem(t.Context(), dir, DesignSystemParams{Action: "set", System: &s}, time.Now)
	require.NoError(t, err)
	writePNG(t, filepath.Join(dir, "screen.png"), 393, 852)
	p := DesignSystemParams{Action: "package", Name: "draft", Screens: []string{"screen.png"}}
	_, err = runDesignSystem(t.Context(), dir, p, time.Now)
	require.NoError(t, err)
	p.OpenQuestions = []string{"Contrast and overflow were not verified"}
	_, err = runDesignSystem(t.Context(), dir, p, time.Now)
	require.Error(t, err, "new limitations must not silently disappear in an unchanged package")
}

func TestRemoteQualityUnknownPowerEffectStaysUnknown(t *testing.T) {
	tool := &remoteSupportTool{}
	res := supportResult{OK: false, Device: supportDevice{RemoteDeviceID: "dev-review", SessionID: "ss-review"}, Effect: support.EffectUnknown}
	err := &support.Error{Code: support.CodeOutcomeUnknown, InputSent: true, Effect: support.EffectUnknown}
	got, _ := tool.powerOutcome(powerShutdown, res, err, "op-review")
	require.Equal(t, support.EffectUnknown, got.Effect, "a dropped socket is not evidence that the OS executed shutdown")
}

func TestRemoteQualityPowerDetectionIgnoresQuotedOutput(t *testing.T) {
	_, ok := classifyPowerCommand([]string{"powershell", "-Command", "Write-Output 'Stop-Computer'"})
	require.False(t, ok, "printing the name of a power command is not executing it")
}

func TestRemoteQualitySecondImageWrapperKeepsOriginalPreviewCoordinates(t *testing.T) {
	data := paddedPNG(t, 1920, 1080, MaxImageDeliveryBytes+1)
	d, err := PrepareImageForDelivery(data, "image/png")
	require.NoError(t, err)
	require.True(t, d.Preview)
	resp := fantasy.WithResponseMetadata(fantasy.NewImageResponse(d.Data, d.MediaType), map[string]any{"image_delivery": d})
	got := EnforceImageDelivery(resp)
	var meta struct {
		Image ImageDelivery `json:"image_delivery"`
	}
	require.NoError(t, json.Unmarshal([]byte(got.Metadata), &meta))
	require.Equal(t, d.CoordinateScale, meta.Image.CoordinateScale, "outer wrapper must not reset preview scaling")
	require.True(t, meta.Image.Preview)
	require.Equal(t, 1920, meta.Image.OriginalWidth)
}

func TestRemoteQualityDesignWritesParticipateInMutationGuard(t *testing.T) {
	for _, action := range []string{"set", "approve", "reset", "package"} {
		require.True(t, CallMutates(DesignSystemToolName, `{"action":"`+action+`"}`), action+" writes project state")
	}
}
